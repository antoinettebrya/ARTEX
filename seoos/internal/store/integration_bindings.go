package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/Autumn-27/artex/seoos/internal/domain"
	"github.com/google/uuid"
)

// resolveIntegration distinguishes old records from disabled or revoked managed
// records. A managed record must never fall back to its pre-migration secret.
func (s *Store) resolveIntegration(ctx context.Context, db rowQueryer, c *domain.ExecutionConnection, decrypt bool) (domain.ExecutionCredential, bool, error) {
	var secret domain.ExecutionCredential
	var raw []byte
	var kind, resource, accountID string
	var enabled, inherited, deleted bool
	err := db.QueryRowContext(ctx, `SELECT provider,external_resource_id,coalesce(integration_account_id::text,''),enabled,inherited,
		deleted_at IS NOT NULL,resource FROM public.seo_site_integration_bindings WHERE organization_id=$1 AND site_id=$2 AND id=$3`,
		c.OrganizationID, c.SiteID, c.ID).Scan(&kind, &resource, &accountID, &enabled, &inherited, &deleted, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return secret, false, nil
	}
	c.Managed, c.IntegrationInherited = true, inherited
	if err != nil {
		return secret, true, err
	}
	if !enabled || deleted || kind != c.Kind {
		return secret, true, domain.ErrIntegrationRevoked
	}
	c.Property = resource
	if kind == domain.ExecutionIndexNow {
		var directory, host, status string
		var sealed []byte
		err = db.QueryRowContext(ctx, `SELECT host,directory,encrypted_key,legacy_key,verification_status FROM public.seo_indexnow_site_keys
			WHERE organization_id=$1 AND site_id=$2 AND id=$3`, c.OrganizationID, c.SiteID, c.ID).
			Scan(&host, &directory, &sealed, &c.LegacyKey, &status)
		if err != nil {
			return secret, true, classifyError(err)
		}
		base, err := domain.EvidenceURL(c.BaseURL)
		if err != nil || host != base.Hostname() || len(sealed) == 0 {
			return secret, true, domain.ErrIntegrationRevoked
		}
		c.IndexNowDirectory, c.KeyVerified = directory, status == "VERIFIED"
		if !decrypt {
			return secret, true, nil
		}
		if err = s.openIntegration("indexnow", c.OrganizationID, c.ID, sealed, &secret.Secret); err != nil {
			return secret, true, err
		}
		c.IndexNowVerificationURL, err = domain.IndexNowLocation(c.BaseURL, directory, secret.Secret)
		if directory != "/" {
			c.KeyLocation = c.IndexNowVerificationURL
		}
		return secret, true, err
	}
	if kind == domain.ExecutionGA4 {
		var meta domain.IntegrationResource
		if err = json.Unmarshal(raw, &meta); err != nil {
			return secret, true, err
		}
		c.PropertyTimeZone = meta.TimeZone
	}
	if !decrypt {
		account, _, err := integrationAccount(ctx, db, c.OrganizationID, accountID, "")
		if err == nil && (!account.Allows(kind, false) || c.AllowWrites && !account.Allows(kind, true)) {
			err = domain.ErrIntegrationRevoked
		}
		c.IntegrationAccountID, c.IntegrationVersion = accountID, account.Version
		return secret, true, err
	}
	access, err := s.integrationAccess(ctx, db, c.OrganizationID, accountID, "")
	if err == nil && (!access.Account.Allows(kind, false) || c.AllowWrites && !access.Account.Allows(kind, true)) {
		err = domain.ErrIntegrationRevoked
	}
	c.IntegrationAccountID, c.IntegrationVersion = accountID, access.Account.Version
	return access.Credential, true, err
}

func integrationSiteLock(ctx context.Context, tx *sql.Tx, site domain.Site) error {
	var base, status string
	var version int64
	err := tx.QueryRowContext(ctx, `SELECT base_url,version,status FROM public.seo_sites
		WHERE organization_id=$1 AND id=$2 FOR SHARE`, site.OrganizationID, site.ID).Scan(&base, &version, &status)
	if err != nil {
		return classifyError(err)
	}
	if site.BaseURL != base || site.Version != version {
		return domain.ErrVersionConflict
	}
	if status != "active" {
		return domain.ErrSiteInactive
	}
	return nil
}

func integrationResourceIdentity(kind, id string) string {
	if kind == domain.ExecutionBing || kind == domain.ExecutionGSC && !strings.HasPrefix(id, "sc-domain:") {
		if parsed, err := domain.EvidenceURL(id); err == nil {
			return strings.TrimSuffix(parsed.String(), "/")
		}
	}
	return id
}

func putIntegrationProjection(ctx context.Context, tx *sql.Tx, org, id string, config domain.ExecutionConnectionInput, creating bool) error {
	raw, err := json.Marshal(config)
	if err != nil {
		return err
	}
	if creating {
		_, err = tx.ExecContext(ctx, `INSERT INTO public.seo_cms_connections
			(id,organization_id,site_id,kind,configured_site_url,name,settings) VALUES ($1,$2,$3,$4,$5,$6,$7::jsonb)`,
			id, org, config.SiteID, config.Kind, config.BaseURL, config.Name, string(raw))
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE public.seo_cms_connections SET configured_site_url=$4,name=$5,settings=$6::jsonb,
			version=version+1,updated_at=clock_timestamp() WHERE organization_id=$1 AND site_id=$2 AND id=$3`,
			org, config.SiteID, id, config.BaseURL, config.Name, string(raw))
	}
	return classifyError(err)
}

func (s *Store) SaveIntegrationBinding(ctx context.Context, org, id string, in domain.IntegrationBindingInput,
	site domain.Site, verified domain.IntegrationResource, expectedAccount int64,
) (string, error) {
	if !executionOwner(ctx, org) || site.OrganizationID != org || site.ID != in.SiteID ||
		verified.Kind != in.Kind || verified.Kind == domain.ExecutionIndexNow || verified.ID != in.ResourceID {
		return "", domain.ErrExecutionAuthority
	}
	if !domain.ValidID(in.AccountID) || id != "" && !domain.ValidID(id) {
		return "", domain.ErrNotFound
	}
	config, err := (domain.ExecutionConnectionInput{SiteID: in.SiteID, Kind: in.Kind, Property: verified.ID,
		Name: strings.ToUpper(in.Kind) + " " + site.Name, Enabled: in.Enabled, AllowWrites: in.AllowWrites,
		RequestsPerMinute: in.RequestsPerMinute, RequestsPerDay: in.RequestsPerDay}).Normalize(site)
	if err != nil {
		return "", err
	}
	meta, err := json.Marshal(verified)
	if err != nil {
		return "", err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	if err = integrationSiteLock(ctx, tx, site); err != nil {
		return "", err
	}
	creating := id == ""
	if creating {
		id = uuid.NewString()
	} else {
		before, _, _, err := execConnection(ctx, tx, org, site.ID, id, "FOR NO KEY UPDATE OF c")
		if err != nil {
			return "", err
		}
		if before.Version != in.Version || before.Kind != in.Kind {
			return "", domain.ErrVersionConflict
		}
	}
	access, err := s.integrationAccess(ctx, tx, org, in.AccountID, " FOR SHARE OF a")
	if err != nil {
		return "", err
	}
	if access.Account.Version != expectedAccount || !access.Account.Allows(in.Kind, in.AllowWrites) {
		return "", domain.ErrIntegrationRevoked
	}
	if err = putIntegrationProjection(ctx, tx, org, id, config, creating); err != nil {
		return "", err
	}
	if creating {
		_, err = tx.ExecContext(ctx, `INSERT INTO public.seo_site_integration_bindings
			(id,organization_id,site_id,integration_account_id,provider,external_resource_id,resource_identity,resource,enabled,last_verified_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8::jsonb,$9,clock_timestamp())`, id, org, site.ID, in.AccountID, in.Kind,
			verified.ID, integrationResourceIdentity(in.Kind, verified.ID), string(meta), in.Enabled)
	} else {
		var updated string
		err = tx.QueryRowContext(ctx, `UPDATE public.seo_site_integration_bindings SET resource=$5::jsonb,enabled=$6,
			last_verified_at=clock_timestamp(),updated_at=clock_timestamp() WHERE organization_id=$1 AND site_id=$2 AND id=$3
			AND integration_account_id=$4 AND external_resource_id=$7 AND deleted_at IS NULL RETURNING id::text`,
			org, site.ID, id, in.AccountID, string(meta), in.Enabled, verified.ID).Scan(&updated)
	}
	if err != nil {
		return "", classifyError(err)
	}
	if err = audit(ctx, tx, org, "integration.binding.save", id, map[string]any{"site_id": site.ID, "provider": in.Kind, "resource_id": verified.ID, "enabled": in.Enabled}); err != nil {
		return "", err
	}
	return id, tx.Commit()
}

func (s *Store) RemoveIntegrationBinding(ctx context.Context, org, id string) error {
	if !executionOwner(ctx, org) {
		return domain.ErrExecutionAuthority
	}
	if !domain.ValidID(id) {
		return domain.ErrNotFound
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var site string
	if err = tx.QueryRowContext(ctx, `UPDATE public.seo_cms_connections SET version=version+1,
		settings=jsonb_set(settings,'{enabled}','false'),updated_at=clock_timestamp()
		WHERE organization_id=$1 AND id=$2 RETURNING site_id::text`, org, id).Scan(&site); err != nil {
		return classifyError(err)
	}
	var kind string
	if err = tx.QueryRowContext(ctx, `UPDATE public.seo_site_integration_bindings SET enabled=false,deleted_at=clock_timestamp(),updated_at=clock_timestamp()
		WHERE organization_id=$1 AND site_id=$2 AND id=$3 AND deleted_at IS NULL RETURNING provider`, org, site, id).Scan(&kind); err != nil {
		return classifyError(err)
	}
	if kind == domain.ExecutionIndexNow {
		if _, err = tx.ExecContext(ctx, `UPDATE public.seo_indexnow_site_keys SET encrypted_key=NULL,updated_at=clock_timestamp()
			WHERE organization_id=$1 AND site_id=$2 AND id=$3`, org, site, id); err != nil {
			return err
		}
	}
	if err = audit(ctx, tx, org, "integration.binding.remove", id, map[string]string{"site_id": site, "provider": kind}); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) SaveIndexNowKey(ctx context.Context, org, id string, in domain.IndexNowKeyInput) (string, error) {
	if !executionOwner(ctx, org) {
		return "", domain.ErrExecutionAuthority
	}
	if id != "" && !domain.ValidID(id) {
		return "", domain.ErrNotFound
	}
	site, err := s.GetSite(ctx, org, in.SiteID)
	if err != nil {
		return "", err
	}
	dir, err := domain.IndexNowDirectory(in.Directory)
	if err != nil {
		return "", err
	}
	key := in.Key
	if key == "" {
		random := make([]byte, 16)
		if _, err := rand.Read(random); err != nil {
			return "", err
		}
		key = hex.EncodeToString(random)
	}
	if !domain.ValidIndexNowKey(key, false) {
		return "", integrationInput("key", "use 8–128 hexadecimal characters and dashes")
	}
	location, err := domain.IndexNowLocation(site.BaseURL, dir, key)
	if err != nil {
		return "", err
	}
	parsed, _ := url.Parse(location)
	base, _ := url.Parse(site.BaseURL)
	if dir != "/" && !strings.HasPrefix(dir, strings.TrimSuffix(base.Path, "/")+"/") &&
		!strings.HasPrefix(strings.TrimSuffix(base.Path, "/")+"/", dir) {
		return "", integrationInput("directory", "choose a directory covering the registered site path or a subdirectory within it")
	}
	config, err := (domain.ExecutionConnectionInput{SiteID: site.ID, Kind: domain.ExecutionIndexNow, Name: "IndexNow",
		KeyLocation: location, Enabled: true, AllowWrites: in.AllowWrites}).Normalize(domain.Site{ID: site.ID, BaseURL: base.Scheme + "://" + base.Host})
	if err != nil {
		return "", err
	}
	config.KeyLocation, config.BaseURL = "", site.BaseURL
	creating := id == ""
	if creating {
		id = uuid.NewString()
	}
	sealed, err := s.sealIntegration("indexnow", org, id, key)
	if err != nil {
		return "", err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	if err = integrationSiteLock(ctx, tx, site); err != nil {
		return "", err
	}
	if !creating {
		before, _, _, err := execConnection(ctx, tx, org, site.ID, id, "FOR NO KEY UPDATE OF c")
		if err != nil || before.Kind != domain.ExecutionIndexNow {
			return "", domain.ErrNotFound
		}
		if before.Version != in.Version {
			return "", domain.ErrVersionConflict
		}
	}
	if err = putIntegrationProjection(ctx, tx, org, id, config, creating); err != nil {
		return "", err
	}
	if creating {
		_, err = tx.ExecContext(ctx, `INSERT INTO public.seo_site_integration_bindings
			(id,organization_id,site_id,provider,external_resource_id,resource_identity) VALUES ($1,$2,$3,'indexnow',$4,$4)`, id, org, site.ID, parsed.Hostname())
		if err == nil {
			_, err = tx.ExecContext(ctx, `INSERT INTO public.seo_indexnow_site_keys (id,organization_id,site_id,host,encrypted_key,directory)
				VALUES ($1,$2,$3,$4,$5,$6)`, id, org, site.ID, parsed.Hostname(), sealed, dir)
		}
	} else {
		var updated string
		err = tx.QueryRowContext(ctx, `UPDATE public.seo_indexnow_site_keys SET host=$4,encrypted_key=$5,directory=$6,key_version=key_version+1,
			verification_status='PENDING',verification_error=NULL,verified_at=NULL,legacy_key=false,updated_at=clock_timestamp()
			WHERE organization_id=$1 AND site_id=$2 AND id=$3 AND encrypted_key IS NOT NULL RETURNING id::text`,
			org, site.ID, id, parsed.Hostname(), sealed, dir).Scan(&updated)
		if err == nil {
			_, err = tx.ExecContext(ctx, `UPDATE public.seo_site_integration_bindings SET external_resource_id=$3,resource_identity=$3,enabled=true,
				last_verified_at=NULL,updated_at=clock_timestamp() WHERE organization_id=$1 AND id=$2 AND deleted_at IS NULL`, org, id, parsed.Hostname())
		}
	}
	if err != nil {
		return "", classifyError(err)
	}
	if err = audit(ctx, tx, org, "integration.indexnow.key.save", id, map[string]any{"site_id": site.ID, "host": parsed.Hostname(), "generated": in.Key == "", "rotated": !creating}); err != nil {
		return "", err
	}
	return id, tx.Commit()
}

func (s *Store) RevealIndexNowKey(ctx context.Context, org, id string) (string, string, error) {
	if !executionOwner(ctx, org) {
		return "", "", domain.ErrExecutionAuthority
	}
	if !domain.ValidID(id) {
		return "", "", domain.ErrNotFound
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", "", err
	}
	defer tx.Rollback()
	var host, dir string
	var sealed []byte
	err = tx.QueryRowContext(ctx, `SELECT host,directory,encrypted_key FROM public.seo_indexnow_site_keys
		WHERE organization_id=$1 AND id=$2 AND encrypted_key IS NOT NULL FOR SHARE`, org, id).Scan(&host, &dir, &sealed)
	if err != nil {
		return "", "", classifyError(err)
	}
	var key string
	if err = s.openIntegration("indexnow", org, id, sealed, &key); err != nil {
		return "", "", err
	}
	if err = audit(ctx, tx, org, "integration.indexnow.key.reveal", id, map[string]string{"host": host}); err != nil {
		return "", "", err
	}
	location, err := domain.IndexNowLocation("https://"+host, dir, key)
	if err != nil {
		return "", "", err
	}
	return key, location, tx.Commit()
}

func (s *Store) finishIndexNowVerification(ctx context.Context, tx *sql.Tx, c domain.ExecutionConnection, code string) error {
	if !c.Managed || c.Kind != domain.ExecutionIndexNow {
		return nil
	}
	status := "VERIFIED"
	var when *time.Time
	if code != "" {
		status = "FAILED"
	} else {
		now := time.Now().UTC()
		when = &now
	}
	_, err := tx.ExecContext(ctx, `UPDATE public.seo_indexnow_site_keys SET verification_status=$4,verification_error=nullif($5,''),
		verified_at=$6,updated_at=clock_timestamp() WHERE organization_id=$1 AND site_id=$2 AND id=$3`, c.OrganizationID, c.SiteID, c.ID, status, code, when)
	return err
}

func legacyIndexNowDirectory(raw string) (string, error) {
	u, err := domain.EvidenceURL(raw)
	if err != nil {
		return "", err
	}
	return domain.IndexNowDirectory(path.Dir(u.Path))
}

func resolveScheduledBinding(ctx context.Context, tx *sql.Tx, org string, in domain.ExecutionJobInput) (string, error) {
	var providers []string
	switch in.Kind {
	case domain.ExecGSCSync:
		providers = []string{domain.ExecutionGSC}
	case domain.ExecGA4Sync:
		providers = []string{domain.ExecutionGA4}
	case domain.ExecBingSync:
		providers = []string{domain.ExecutionBing}
	case domain.ExecInspection:
		providers = []string{domain.ExecutionGSC, domain.ExecutionBing}
	case domain.ExecIndexNow:
		providers = []string{domain.ExecutionIndexNow, domain.ExecutionBing}
	default:
		return "", nil
	}
	if in.Provider != "" {
		for _, provider := range providers {
			if provider == in.Provider {
				providers = []string{provider}
				break
			}
		}
	}
	var id string
	var count int
	err := tx.QueryRowContext(ctx, `SELECT coalesce(min(b.id::text),''),count(*)
		FROM public.seo_site_integration_bindings b
		JOIN public.seo_cms_connections c ON (c.organization_id,c.site_id,c.id)=(b.organization_id,b.site_id,b.id)
		JOIN public.seo_sites s ON (s.organization_id,s.id)=(b.organization_id,b.site_id)
		LEFT JOIN public.seo_integration_accounts a ON (a.organization_id,a.id)=(b.organization_id,b.integration_account_id)
		WHERE b.organization_id=$1 AND b.site_id=$2 AND b.provider=ANY($3::text[])
		AND b.enabled AND b.deleted_at IS NULL AND s.status='active' AND c.configured_site_url=s.base_url
		AND (b.provider='indexnow' OR a.authorization_status='CONNECTED')
		AND (NOT $4 OR c.settings->>'allow_writes'='true')`,
		org, in.SiteID, providers, in.Kind == domain.ExecIndexNow).Scan(&id, &count)
	if err != nil {
		return "", err
	}
	if count != 1 {
		return "", domain.ErrIntegrationRequired
	}
	return id, nil
}

func (s *Store) RecordIndexNowVerification(ctx context.Context, lease domain.Lease, c domain.ExecutionConnection, code string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	job, err := lockLease(ctx, tx, lease)
	if err != nil {
		return err
	}
	if job.OrganizationID != c.OrganizationID || job.SiteID != c.SiteID {
		return domain.ErrExecutionAuthority
	}
	current, _, active, err := execConnection(ctx, tx, c.OrganizationID, c.SiteID, c.ID, "FOR SHARE OF c")
	if err != nil || !active || current.Version != c.Version {
		return domain.ErrIntegrationRevoked
	}
	if err = s.fenceExecutionIntegration(ctx, tx, job.ID, &current); err != nil {
		return err
	}
	if err = s.finishIndexNowVerification(ctx, tx, current, code); err != nil {
		return err
	}
	if err = audit(ctx, tx, c.OrganizationID, "integration.indexnow.verify", c.ID, map[string]string{"site_id": c.SiteID, "error": code}); err != nil {
		return err
	}
	return tx.Commit()
}
