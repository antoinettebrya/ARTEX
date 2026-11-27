package store

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"path"
	"slices"
	"strings"

	"github.com/Autumn-27/artex/seoos/internal/domain"
	"github.com/google/uuid"
)

const (
	legacyGoogleMatch = "GOOGLE_CLIENT_ID_SECRET_AND_REFRESH"
	legacyBingMatch   = "BING_API_KEY_HMAC"
)

type LegacyIntegrationResult struct {
	ConnectionID          string `json:"connection_id"`
	SiteID                string `json:"site_id"`
	Provider              string `json:"provider"`
	Status                string `json:"status"`
	BindingResult         string `json:"binding_result"`
	TargetProperty        string `json:"target_property,omitempty"`
	TargetAccountID       string `json:"target_account_id,omitempty"`
	PreviousAccountID     string `json:"previous_account_id,omitempty"`
	MatchBasis            string `json:"match_basis,omitempty"`
	ReauthorizationReason string `json:"reauthorization_reason,omitempty"`
	ReceiptID             string `json:"receipt_id,omitempty"`
	previousSubject       string
	previousTargetScopes  []string
	previousAppScopes     []string
	previousAppVersion    int64
}

type LegacyIntegrationApproval struct {
	ConnectionID    string `json:"connection_id"`
	TargetAccountID string `json:"target_account_id"`
}

type LegacyIntegrationOptions struct {
	DryRun         bool
	DuplicatesOnly bool
	Approvals      []LegacyIntegrationApproval
}

type legacyConnection struct {
	id, site, kind, account, resource string
	bound, removed, inherited         bool
	enabled                           sql.NullBool
}

// Fingerprints are organization-scoped and stay in memory for the migration.
type legacyFingerprint struct {
	service, clientID   string
	clientSecret, grant [32]byte
}

func integrationMigrationHMAC(key, value []byte) [32]byte {
	mac := hmac.New(sha256.New, key)
	mac.Write(value)
	var result [32]byte
	copy(result[:], mac.Sum(nil))
	return result
}

func (s *Store) legacyFingerprint(org, provider string, secret domain.ExecutionCredential) (legacyFingerprint, bool) {
	value := legacyFingerprint{service: provider}
	digest := func(field, credential string) [32]byte {
		return integrationMigrationHMAC(s.integrationMatchKey[:], []byte(org+"\x00"+provider+"\x00"+field+"\x00"+credential))
	}
	switch provider {
	case domain.IntegrationGoogle:
		if !domain.ValidIntegrationSecret(secret.ClientID) || !domain.ValidIntegrationSecret(secret.ClientSecret) ||
			!domain.ValidIntegrationSecret(secret.RefreshToken) {
			return value, false
		}
		value.clientID = secret.ClientID
		value.clientSecret = digest("client-secret", secret.ClientSecret)
		value.grant = digest("refresh", secret.RefreshToken)
	case domain.IntegrationBing:
		if !domain.ValidIntegrationSecret(secret.Secret) {
			return value, false
		}
		value.grant = digest("bing-api-key", secret.Secret)
	default:
		return value, false
	}
	return value, true
}

func legacyProvider(kind string) string {
	switch kind {
	case domain.ExecutionGSC, domain.ExecutionGA4:
		return domain.IntegrationGoogle
	case domain.ExecutionBing:
		return domain.IntegrationBing
	}
	return domain.ExecutionIndexNow
}

type legacyMigration struct {
	repo      *Store
	tx        *sql.Tx
	org       string
	preview   bool
	targets   map[legacyFingerprint]string
	clients   map[string]bool
	approvals map[string]string
}

func (s *Store) MigrateLegacyIntegrations(ctx context.Context, org string, dryRun bool) ([]LegacyIntegrationResult, error) {
	return s.ReviewLegacyIntegrations(ctx, org, LegacyIntegrationOptions{DryRun: dryRun})
}

func (s *Store) ReviewLegacyIntegrations(ctx context.Context, org string, options LegacyIntegrationOptions) ([]LegacyIntegrationResult, error) {
	if !executionOwner(ctx, org) || s.providerCipher == nil {
		return nil, domain.ErrExecutionAuthority
	}
	approvals := make(map[string]string, len(options.Approvals))
	for _, approval := range options.Approvals {
		if !domain.ValidID(approval.ConnectionID) || !domain.ValidID(approval.TargetAccountID) || approvals[approval.ConnectionID] != "" {
			return nil, integrationInput("approvals", "supply one valid target for each reviewed connection")
		}
		approvals[approval.ConnectionID] = approval.TargetAccountID
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "integration-legacy:"+org); err != nil {
		return nil, err
	}
	connections, err := readLegacyConnections(ctx, tx, org)
	if err != nil {
		return nil, err
	}
	run := legacyMigration{repo: s, tx: tx, org: org, preview: options.DryRun || options.DuplicatesOnly,
		targets: make(map[legacyFingerprint]string), clients: make(map[string]bool), approvals: approvals}
	if err = run.seedTargets(ctx, connections); err != nil {
		return nil, err
	}
	results := make([]LegacyIntegrationResult, 0, len(connections))
	for _, connection := range connections {
		result, err := run.apply(ctx, connection)
		if err != nil {
			return results, err
		}
		if !options.DuplicatesOnly || result.MatchBasis == legacyGoogleMatch || result.MatchBasis == legacyBingMatch {
			results = append(results, result)
		}
	}
	if len(approvals) != 0 {
		return results, integrationInput("approvals", "the reviewed Bing match changed or no longer applies; repeat the duplicate report")
	}
	if run.preview {
		return results, nil
	}
	return results, tx.Commit()
}

func readLegacyConnections(ctx context.Context, tx *sql.Tx, org string) ([]legacyConnection, error) {
	rows, err := tx.QueryContext(ctx, `SELECT c.id::text,c.site_id::text,c.kind,coalesce(b.integration_account_id::text,''),
		coalesce(b.external_resource_id,''),b.id IS NOT NULL,b.deleted_at IS NOT NULL,coalesce(b.inherited,false),b.enabled
		FROM public.seo_cms_connections c
		LEFT JOIN public.seo_site_integration_bindings b ON (b.organization_id,b.site_id,b.id)=(c.organization_id,c.site_id,c.id)
		WHERE c.organization_id=$1 AND c.kind IN ('gsc','ga4','bing','indexnow') AND c.credential IS NOT NULL
		ORDER BY c.created_at,c.id`, org)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []legacyConnection
	for rows.Next() {
		var entry legacyConnection
		if err := rows.Scan(&entry.id, &entry.site, &entry.kind, &entry.account, &entry.resource, &entry.bound, &entry.removed, &entry.inherited, &entry.enabled); err != nil {
			return nil, err
		}
		result = append(result, entry)
	}
	return result, rows.Err()
}

func (run legacyMigration) seedTargets(ctx context.Context, connections []legacyConnection) error {
	seen := make(map[string]bool)
	for _, item := range connections {
		if item.removed || !item.inherited || item.account == "" || seen[item.account] {
			continue
		}
		seen[item.account] = true
		access, err := run.activeAccess(ctx, item.account, "")
		if err != nil {
			return err
		}
		if access.Account.ID != "" {
			run.remember(access.Account.ID, access.Account.Provider, access.Credential)
		}
	}
	return nil
}

func (run legacyMigration) activeAccess(ctx context.Context, account, lock string) (domain.IntegrationAccess, error) {
	access, err := run.repo.integrationAccess(ctx, run.tx, run.org, account, lock)
	if errors.Is(err, domain.ErrIntegrationRevoked) || errors.Is(err, domain.ErrNotFound) ||
		errors.Is(err, domain.ErrProviderNotConfigured) || err == nil && access.Account.Status != domain.IntegrationConnected {
		return domain.IntegrationAccess{}, nil
	}
	return access, err
}

func (run legacyMigration) remember(id, provider string, secret domain.ExecutionCredential) {
	if provider == domain.IntegrationGoogle && secret.ClientID != "" {
		run.clients[secret.ClientID] = true
	}
	if key, ok := run.repo.legacyFingerprint(run.org, provider, secret); ok && run.targets[key] == "" {
		run.targets[key] = id
	}
}

func (run legacyMigration) apply(ctx context.Context, item legacyConnection) (LegacyIntegrationResult, error) {
	result := LegacyIntegrationResult{ConnectionID: item.id, SiteID: item.site, Provider: item.kind, PreviousAccountID: item.account,
		Status: "SITE_INACTIVE_OR_CHANGED", BindingResult: "UNCHANGED"}
	if item.removed {
		result.Status = "BINDING_REMOVED"
		return result, nil
	}
	if item.bound && (item.kind == domain.ExecutionIndexNow || !item.inherited) {
		result.Status, result.BindingResult = "ALREADY_MIGRATED", "RETAINED"
		result.TargetProperty, result.TargetAccountID = item.resource, item.account
		return result, nil
	}
	site, err := scanSite(run.tx.QueryRowContext(ctx, `SELECT `+siteColumns+` FROM public.seo_sites
		WHERE organization_id=$1 AND id=$2`, run.org, item.site))
	if err != nil {
		return result, err
	}
	if err = integrationSiteLock(ctx, run.tx, site); err != nil {
		if errors.Is(err, domain.ErrSiteInactive) || errors.Is(err, domain.ErrVersionConflict) {
			return result, nil
		}
		return result, err
	}
	c, raw, active, err := execConnection(ctx, run.tx, run.org, item.site, item.id, "FOR NO KEY UPDATE OF c")
	if err != nil || !active {
		return result, err
	}
	// A concurrent user edit must not be overwritten by a reviewed alias.
	var unchanged bool
	if err = run.tx.QueryRowContext(ctx, `SELECT coalesce(b.integration_account_id::text,'')=$4 AND
		(b.id IS NOT NULL)=$5 AND b.deleted_at IS NULL FROM public.seo_cms_connections c
		LEFT JOIN public.seo_site_integration_bindings b ON (b.organization_id,b.site_id,b.id)=(c.organization_id,c.site_id,c.id)
		WHERE c.organization_id=$1 AND c.site_id=$2 AND c.id=$3`, run.org, item.site, item.id, item.account, item.bound).Scan(&unchanged); err != nil {
		return result, err
	}
	if !unchanged {
		result.Status = "BINDING_CHANGED"
		return result, nil
	}
	result.TargetProperty = c.Property
	if _, err = c.ExecutionConnectionInput.Normalize(site); err != nil {
		result.Status = "INVALID_LEGACY_SCOPE"
		return result, nil
	}
	var secret domain.ExecutionCredential
	clear, err := run.repo.providerCipher.Open(nil, nil, raw, execBinding(run.org, item.site, item.id))
	if err != nil || json.Unmarshal(clear, &secret) != nil || secret.Validate(c.ExecutionConnectionInput) != nil {
		result.Status = "INVALID_LEGACY_CREDENTIAL"
		return result, nil
	}
	var directory string
	if c.Kind == domain.ExecutionIndexNow {
		host, dir, err := legacyIndexNowLocation(c, site, secret.Secret)
		if err != nil {
			result.Status = "INVALID_LEGACY_SCOPE"
			return result, nil
		}
		result.TargetProperty, directory = host, dir
	}
	if !item.bound {
		var conflict bool
		identity := integrationResourceIdentity(c.Kind, result.TargetProperty)
		if err = run.tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM public.seo_site_integration_bindings
			WHERE organization_id=$1 AND provider=$2 AND deleted_at IS NULL
			AND (site_id=$3 OR (provider IN ('bing','indexnow') AND resource_identity=$4)))`,
			run.org, c.Kind, item.site, identity).Scan(&conflict); err != nil {
			return result, err
		}
		if conflict {
			result.Status = "BINDING_CONFLICT"
			return result, nil
		}
	}
	provider := legacyProvider(c.Kind)
	fingerprint, hasFingerprint := run.repo.legacyFingerprint(run.org, provider, secret)
	var target string
	if hasFingerprint {
		target = run.targets[fingerprint]
	}
	if item.bound {
		result.Status, result.BindingResult, result.TargetAccountID = "ALREADY_MIGRATED", "RETAINED", item.account
		if err = run.checkPrevious(ctx, item, fingerprint, target, &result); err != nil || result.Status != "ALREADY_MIGRATED" || target == "" || target == item.account {
			return result, err
		}
	}
	if target != "" {
		eligible, err := run.reuseTarget(ctx, item, fingerprint, target, &result)
		if err != nil || !eligible {
			return result, err
		}
	} else if provider != domain.ExecutionIndexNow {
		result.MatchBasis = "NEW_ACCOUNT"
		if provider == domain.IntegrationGoogle && run.clients[secret.ClientID] {
			result.MatchBasis = "GOOGLE_CLIENT_ID_WITH_DIFFERENT_GRANT"
		}
		result.TargetAccountID = uuid.NewSHA1(uuid.NameSpaceOID, []byte("artex-legacy-account:"+run.org+":"+item.id)).String()
		if err := run.repo.migrateLegacyAccount(ctx, run.tx, c, secret, result.TargetAccountID); err != nil {
			return result, err
		}
		run.remember(result.TargetAccountID, provider, secret)
		if provider == domain.IntegrationGoogle {
			result.ReauthorizationReason = "GOOGLE_IDENTITY_UNVERIFIED"
			if !domain.ValidIntegrationSecret(secret.RefreshToken) {
				result.ReauthorizationReason = "GOOGLE_REFRESH_TOKEN_MISSING"
			}
		}
	}
	if item.bound {
		err = run.reassign(ctx, c, item, result.TargetAccountID)
		result.Status, result.BindingResult = "REASSIGNED", "REASSIGNED"
		if run.preview {
			result.Status, result.BindingResult = "READY_TO_REASSIGN", "WOULD_REASSIGN"
		}
	} else {
		err = run.insertBinding(ctx, c, directory, secret.Secret, result)
		result.Status, result.BindingResult = "MIGRATED", "BOUND"
		if run.preview {
			result.Status, result.BindingResult = "READY", "WOULD_BIND"
		}
	}
	if err != nil {
		return result, err
	}
	if result.MatchBasis == legacyBingMatch {
		delete(run.approvals, item.id)
	}
	if !run.preview {
		err = run.record(ctx, c, item, &result)
	}
	return result, err
}

func (run legacyMigration) checkPrevious(ctx context.Context, item legacyConnection, fingerprint legacyFingerprint, target string, result *LegacyIntegrationResult) error {
	if item.account == "" {
		return nil
	}
	access, err := run.activeAccess(ctx, item.account, " FOR NO KEY UPDATE OF a")
	if err != nil {
		return err
	}
	if access.Account.ID == "" {
		result.ReauthorizationReason = "CURRENT_AUTHORIZATION_INACTIVE"
		result.Status = "CURRENT_ACCOUNT_RETAINED"
		return nil
	}
	if access.Account.Provider == domain.IntegrationGoogle && access.Account.VerifiedSubject == "" {
		result.ReauthorizationReason = "GOOGLE_IDENTITY_UNVERIFIED"
	}
	if current, ok := run.repo.legacyFingerprint(run.org, access.Account.Provider, access.Credential); !ok || current != fingerprint {
		result.Status = "CURRENT_ACCOUNT_RETAINED"
		result.MatchBasis = "CURRENT_GRANT_DIFFERS_FROM_LEGACY"
		return nil
	}
	result.previousSubject = access.Account.VerifiedSubject
	if target == item.account && run.approvals[item.id] == target && fingerprint.service == domain.IntegrationBing {
		result.MatchBasis = legacyBingMatch
		delete(run.approvals, item.id)
	}
	return nil
}

func (run legacyMigration) reuseTarget(ctx context.Context, item legacyConnection, key legacyFingerprint, target string, result *LegacyIntegrationResult) (bool, error) {
	result.TargetAccountID = target
	if key.service == domain.IntegrationBing {
		result.MatchBasis = legacyBingMatch
		if run.approvals[item.id] != target {
			result.Status, result.BindingResult = "MERGE_REVIEW_REQUIRED", "AWAITING_APPROVAL"
			return false, nil
		}
	} else {
		result.MatchBasis = legacyGoogleMatch
	}
	access, err := run.activeAccess(ctx, target, " FOR NO KEY UPDATE OF a")
	if err != nil {
		return false, err
	}
	if current, ok := run.repo.legacyFingerprint(run.org, key.service, access.Credential); !ok || current != key {
		result.Status = "TARGET_ACCOUNT_CHANGED"
		return false, nil
	}
	if result.previousSubject != "" && result.previousSubject != access.Account.VerifiedSubject {
		result.Status, result.BindingResult, result.ReauthorizationReason = "TARGET_IDENTITY_REVIEW_REQUIRED", "RETAINED", "TARGET_IDENTITY_MISMATCH"
		return false, nil
	}
	if key.service == domain.IntegrationGoogle && access.Account.VerifiedSubject == "" {
		result.ReauthorizationReason = "GOOGLE_IDENTITY_UNVERIFIED"
	}
	scope := legacyGoogleScope(item.kind)
	if access.Account.Allows(item.kind, false) &&
		(key.service != domain.IntegrationGoogle || slices.Contains(access.Application.AllowedScopes, scope)) {
		return true, nil
	}
	// Only an inherited account without a verified subject can absorb
	// scope evidence from the same exact legacy refresh grant.
	if key.service != domain.IntegrationGoogle || access.Account.VerifiedSubject != "" {
		result.Status, result.BindingResult = "TARGET_SCOPE_MISSING", "AWAITING_AUTHORIZATION"
		result.ReauthorizationReason = "TARGET_SCOPE_NOT_GRANTED"
		return false, nil
	}
	result.previousTargetScopes = access.Account.Scopes
	result.previousAppScopes = access.Application.AllowedScopes
	result.previousAppVersion = access.Application.Version
	granted := slices.Clone(access.Account.Scopes)
	if !slices.Contains(granted, scope) {
		granted = append(granted, scope)
	}
	allowed := slices.Clone(access.Application.AllowedScopes)
	if !slices.Contains(allowed, scope) {
		allowed = append(allowed, scope)
	}
	if _, err = run.tx.ExecContext(ctx, `UPDATE public.seo_integration_applications SET allowed_scopes=$3,
		config_version=config_version+1,updated_at=clock_timestamp() WHERE organization_id=$1 AND id=$2`,
		run.org, access.Application.ID, allowed); err != nil {
		return false, err
	}
	_, err = run.tx.ExecContext(ctx, `UPDATE public.seo_integration_accounts SET granted_scopes=$3,updated_at=clock_timestamp()
		WHERE organization_id=$1 AND id=$2`, run.org, target, granted)
	return err == nil, err
}

func (run legacyMigration) insertBinding(ctx context.Context, c domain.ExecutionConnection, directory, key string, result LegacyIntegrationResult) error {
	resource := result.TargetProperty
	metadata, err := json.Marshal(domain.IntegrationResource{ID: resource, Kind: c.Kind, Name: c.Property})
	if err != nil {
		return err
	}
	if _, err = run.tx.ExecContext(ctx, `INSERT INTO public.seo_site_integration_bindings
		(id,organization_id,site_id,integration_account_id,provider,external_resource_id,resource_identity,resource,enabled,inherited)
		VALUES($1,$2,$3,NULLIF($4,'')::uuid,$5,$6,$7,$8::jsonb,$9,true)`, c.ID, run.org, c.SiteID, result.TargetAccountID, c.Kind,
		resource, integrationResourceIdentity(c.Kind, resource), string(metadata), c.Enabled); err != nil || c.Kind != domain.ExecutionIndexNow {
		return classifyError(err)
	}
	encrypted, err := run.repo.sealIntegration("indexnow", run.org, c.ID, key)
	if err != nil {
		return err
	}
	var check string
	if err = run.repo.openIntegration("indexnow", run.org, c.ID, encrypted, &check); err != nil || check != key {
		return domain.ErrProviderNotConfigured
	}
	_, err = run.tx.ExecContext(ctx, `INSERT INTO public.seo_indexnow_site_keys
		(id,organization_id,site_id,host,encrypted_key,directory,legacy_key) VALUES ($1,$2,$3,$4,$5,$6,true)`,
		c.ID, run.org, c.SiteID, resource, encrypted, directory)
	return classifyError(err)
}

func (run legacyMigration) reassign(ctx context.Context, c domain.ExecutionConnection, item legacyConnection, target string) error {
	var id string
	err := run.tx.QueryRowContext(ctx, `UPDATE public.seo_site_integration_bindings SET integration_account_id=$4,
		updated_at=clock_timestamp() WHERE organization_id=$1 AND site_id=$2 AND id=$3 AND inherited
		AND deleted_at IS NULL AND integration_account_id=$5 RETURNING id::text`, run.org, c.SiteID, c.ID, target, item.account).Scan(&id)
	if err != nil {
		return classifyError(err)
	}
	_, err = run.tx.ExecContext(ctx, `UPDATE public.seo_cms_connections SET version=version+1,updated_at=clock_timestamp()
		WHERE organization_id=$1 AND site_id=$2 AND id=$3`, run.org, c.SiteID, c.ID)
	if err != nil {
		return err
	}
	// Preserve the previous encrypted account for recovery and remove it
	// from account selection once all its bindings have moved or detached.
	_, err = run.tx.ExecContext(ctx, `UPDATE public.seo_integration_accounts a SET deleted_at=clock_timestamp(),
		credential_version=credential_version+1,updated_at=clock_timestamp()
		WHERE a.organization_id=$1 AND a.id=$2 AND a.deleted_at IS NULL AND NOT EXISTS (
		  SELECT 1 FROM public.seo_site_integration_bindings b WHERE
		  (b.organization_id,b.integration_account_id)=(a.organization_id,a.id) AND b.deleted_at IS NULL)`, run.org, item.account)
	return err
}

func (run legacyMigration) record(ctx context.Context, c domain.ExecutionConnection, item legacyConnection, result *LegacyIntegrationResult) error {
	result.ReceiptID = uuid.NewString()
	_, err := run.tx.ExecContext(ctx, `INSERT INTO public.seo_integration_legacy_migration_receipts
		(id,organization_id,site_id,connection_id,previous_account_id,target_account_id,previous_connection_version,previous_binding_enabled,
		action,match_basis,previous_target_scopes,previous_application_scopes,previous_application_version)
		VALUES($1,$2,$3,$4,NULLIF($5,'')::uuid,NULLIF($6,'')::uuid,$7,$8,$9,$10,$11,$12,NULLIF($13,0))`, result.ReceiptID, run.org, c.SiteID,
		c.ID, item.account, result.TargetAccountID, c.Version, item.enabled, result.Status, result.MatchBasis,
		result.previousTargetScopes, result.previousAppScopes, result.previousAppVersion)
	if err != nil {
		return err
	}
	return audit(ctx, run.tx, run.org, "integration.legacy.migrate", c.ID, map[string]string{
		"site_id": c.SiteID, "provider": c.Kind, "receipt_id": result.ReceiptID, "action": result.Status})
}

func legacyIndexNowLocation(c domain.ExecutionConnection, site domain.Site, secret string) (string, string, error) {
	keyURL, err := domain.EvidenceURL(c.KeyLocation)
	if err != nil || path.Base(keyURL.Path) != secret+".txt" {
		return "", "", domain.ErrExecutionAuthority
	}
	dir, err := domain.IndexNowDirectory(path.Dir(keyURL.Path))
	if err != nil {
		return "", "", err
	}
	location, err := domain.IndexNowLocation(site.BaseURL, dir, secret)
	if err != nil || location != c.KeyLocation {
		return "", "", domain.ErrExecutionAuthority
	}
	return keyURL.Hostname(), dir, nil
}

func legacyGoogleScope(kind string) string {
	if kind == domain.ExecutionGA4 {
		return domain.GA4ReadScope
	}
	return domain.GSCReadScope
}

func (s *Store) migrateLegacyAccount(ctx context.Context, tx *sql.Tx, c domain.ExecutionConnection, secret domain.ExecutionCredential, account string) error {
	org := c.OrganizationID
	label := strings.ToUpper(c.Kind) + " " + c.ID[:8]
	provider, mode, app, status := domain.IntegrationBing, domain.IntegrationAPIKey, "", domain.IntegrationConnected
	scopes := []string{}
	if c.Kind == domain.ExecutionGA4 || c.Kind == domain.ExecutionGSC {
		provider, mode, app = domain.IntegrationGoogle, domain.IntegrationOAuth, uuid.NewString()
		allowed := legacyGoogleScope(c.Kind)
		callback := ""
		if c.RedirectURI != "" {
			u, err := domain.EvidenceURL(c.RedirectURI)
			if err != nil {
				return err
			}
			u.Path, u.RawPath = domain.GoogleOAuthCallbackPath, ""
			callback = u.String()
		}
		cipher, err := s.sealIntegration("application", org, app, secret.ClientSecret)
		if err != nil {
			return err
		}
		var check string
		if err = s.openIntegration("application", org, app, cipher, &check); err != nil || check != secret.ClientSecret {
			return domain.ErrProviderNotConfigured
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO public.seo_integration_applications
			(id,organization_id,provider,label,client_id,encrypted_client_secret,redirect_uri,allowed_scopes)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, app, org, provider, label, secret.ClientID, cipher, callback, []string{allowed}); err != nil {
			return err
		}
		secret.ClientID, secret.ClientSecret, secret.AccessToken, secret.ExpiresAt = "", "", "", nil
		if domain.ValidIntegrationSecret(secret.RefreshToken) {
			scopes = []string{allowed}
		} else {
			secret.RefreshToken = ""
			status = domain.IntegrationReauth
		}
	}
	// Pre-OIDC legacy records cannot supply a verified account identity.
	secret.ExternalSubject = ""
	cipher, err := s.sealIntegration("account", org, account, secret)
	if err != nil {
		return err
	}
	var check domain.ExecutionCredential
	if err = s.openIntegration("account", org, account, cipher, &check); err != nil || check.Secret != secret.Secret || check.RefreshToken != secret.RefreshToken {
		return domain.ErrProviderNotConfigured
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO public.seo_integration_accounts
		(id,organization_id,provider,application_id,auth_mode,label,granted_scopes,encrypted_credentials,authorization_status)
		VALUES($1,$2,$3,NULLIF($4,'')::uuid,$5,$6,$7,$8,$9)`, account, org, provider, app, mode, label, scopes, cipher, status)
	return err
}
