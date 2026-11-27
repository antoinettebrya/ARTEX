package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/Autumn-27/artex/seoos/internal/domain"
	"github.com/google/uuid"
)

type LegacyIntegrationResult struct {
	ConnectionID string `json:"connection_id"`
	Provider     string `json:"provider"`
	Status       string `json:"status"`
}

func (s *Store) MigrateLegacyIntegrations(ctx context.Context, org string, dryRun bool) ([]LegacyIntegrationResult, error) {
	if !executionOwner(ctx, org) || s.providerCipher == nil {
		return nil, domain.ErrExecutionAuthority
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id::text,site_id::text,kind FROM public.seo_cms_connections
		WHERE organization_id=$1 AND kind IN ('gsc','ga4','bing','indexnow') AND credential IS NOT NULL ORDER BY created_at,id`, org)
	if err != nil {
		return nil, err
	}
	type legacy struct{ id, site, kind string }
	var pending []legacy
	for rows.Next() {
		var item legacy
		if err = rows.Scan(&item.id, &item.site, &item.kind); err != nil {
			rows.Close()
			return nil, err
		}
		pending = append(pending, item)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, err
	}
	results := make([]LegacyIntegrationResult, 0, len(pending))
	for _, item := range pending {
		status, err := s.migrateLegacyIntegration(ctx, org, item.site, item.id, dryRun)
		if err != nil {
			return results, fmt.Errorf("migration of %s failed: %w", item.id, err)
		}
		results = append(results, LegacyIntegrationResult{ConnectionID: item.id, Provider: item.kind, Status: status})
	}
	return results, nil
}

func (s *Store) migrateLegacyIntegration(ctx context.Context, org, siteID, id string, dryRun bool) (string, error) {
	site, err := s.GetSite(ctx, org, siteID)
	if err != nil {
		return "", err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	if err = integrationSiteLock(ctx, tx, site); err != nil {
		if errors.Is(err, domain.ErrSiteInactive) || errors.Is(err, domain.ErrVersionConflict) {
			return "SITE_INACTIVE_OR_CHANGED", nil
		}
		return "", err
	}
	c, raw, active, err := execConnection(ctx, tx, org, siteID, id, "FOR NO KEY UPDATE OF c")
	if err != nil {
		return "", err
	}
	var exists bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM public.seo_site_integration_bindings WHERE organization_id=$1 AND id=$2)`, org, id).Scan(&exists); err != nil {
		return "", err
	}
	if exists {
		return "ALREADY_MIGRATED", nil
	}
	if !active {
		return "SITE_INACTIVE_OR_CHANGED", nil
	}
	if _, err = c.ExecutionConnectionInput.Normalize(site); err != nil {
		return "INVALID_LEGACY_SCOPE", nil
	}
	var secret domain.ExecutionCredential
	clear, err := s.providerCipher.Open(nil, nil, raw, execBinding(org, siteID, id))
	if err != nil || json.Unmarshal(clear, &secret) != nil || secret.Validate(c.ExecutionConnectionInput) != nil {
		return "INVALID_LEGACY_CREDENTIAL", nil
	}
	resource := c.Property
	directory := ""
	if c.Kind == domain.ExecutionIndexNow {
		keyURL, err := domain.EvidenceURL(c.KeyLocation)
		if err != nil || path.Base(keyURL.Path) != secret.Secret+".txt" {
			return "INVALID_LEGACY_SCOPE", nil
		}
		resource, directory = keyURL.Hostname(), path.Dir(keyURL.Path)
		dir, err := domain.IndexNowDirectory(directory)
		if err != nil {
			return "INVALID_LEGACY_SCOPE", nil
		}
		directory = dir
		location, err := domain.IndexNowLocation(site.BaseURL, directory, secret.Secret)
		if err != nil || location != c.KeyLocation {
			return "INVALID_LEGACY_SCOPE", nil
		}
	}
	identity := integrationResourceIdentity(c.Kind, resource)
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM public.seo_site_integration_bindings
		WHERE organization_id=$1 AND provider=$2 AND deleted_at IS NULL AND (site_id=$3 OR resource_identity=$4))`,
		org, c.Kind, siteID, identity).Scan(&exists); err != nil {
		return "", err
	}
	if exists {
		return "BINDING_CONFLICT", nil
	}
	var accountID string
	if c.Kind != domain.ExecutionIndexNow {
		accountID, err = s.migrateLegacyAccount(ctx, tx, c, secret)
		if err != nil {
			return "", err
		}
	}
	metadata, err := json.Marshal(domain.IntegrationResource{ID: resource, Kind: c.Kind, Name: c.Property})
	if err != nil {
		return "", err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO public.seo_site_integration_bindings
		(id,organization_id,site_id,integration_account_id,provider,external_resource_id,resource_identity,resource,enabled,inherited)
		VALUES($1,$2,$3,NULLIF($4,'')::uuid,$5,$6,$7,$8::jsonb,$9,true)`, id, org, siteID, accountID, c.Kind,
		resource, identity, string(metadata), c.Enabled); err != nil {
		if errors.Is(classifyError(err), domain.ErrConflict) {
			return "BINDING_CONFLICT", nil
		}
		return "", err
	}
	if c.Kind == domain.ExecutionIndexNow {
		encrypted, err := s.sealIntegration("indexnow", org, id, secret.Secret)
		if err != nil {
			return "", err
		}
		var check string
		if err = s.openIntegration("indexnow", org, id, encrypted, &check); err != nil || check != secret.Secret {
			return "", domain.ErrProviderNotConfigured
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO public.seo_indexnow_site_keys
			(id,organization_id,site_id,host,encrypted_key,directory,legacy_key) VALUES ($1,$2,$3,$4,$5,$6,true)`,
			id, org, siteID, resource, encrypted, directory); err != nil {
			return "", classifyError(err)
		}
	}
	if err = audit(ctx, tx, org, "integration.legacy.migrate", id, map[string]string{"site_id": siteID, "provider": c.Kind}); err != nil {
		return "", err
	}
	if dryRun {
		return "READY", nil
	}
	return "MIGRATED", tx.Commit()
}

func (s *Store) migrateLegacyAccount(ctx context.Context, tx *sql.Tx, c domain.ExecutionConnection, secret domain.ExecutionCredential) (string, error) {
	org := c.OrganizationID
	account := uuid.NewString()
	label := strings.ToUpper(c.Kind) + " " + c.ID[:8]
	provider, mode, app, status := domain.IntegrationBing, domain.IntegrationAPIKey, "", domain.IntegrationConnected
	scopes := []string{}
	if c.Kind == domain.ExecutionGA4 || c.Kind == domain.ExecutionGSC {
		provider, mode, app = domain.IntegrationGoogle, domain.IntegrationOAuth, uuid.NewString()
		allowed := domain.GSCReadScope
		if c.Kind == domain.ExecutionGA4 {
			allowed = domain.GA4ReadScope
		}
		callback := ""
		if c.RedirectURI != "" {
			u, err := domain.EvidenceURL(c.RedirectURI)
			if err != nil {
				return "", err
			}
			u.Path, u.RawPath = domain.GoogleOAuthCallbackPath, ""
			callback = u.String()
		}
		cipher, err := s.sealIntegration("application", org, app, secret.ClientSecret)
		if err != nil {
			return "", err
		}
		var check string
		if err = s.openIntegration("application", org, app, cipher, &check); err != nil || check != secret.ClientSecret {
			return "", domain.ErrProviderNotConfigured
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO public.seo_integration_applications
			(id,organization_id,provider,label,client_id,encrypted_client_secret,redirect_uri,allowed_scopes)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, app, org, provider, label, secret.ClientID, cipher, callback, []string{allowed}); err != nil {
			return "", err
		}
		secret.ClientID, secret.ClientSecret, secret.AccessToken, secret.ExpiresAt = "", "", "", nil
		if domain.ValidIntegrationSecret(secret.RefreshToken) {
			scopes = []string{allowed}
		} else {
			status = domain.IntegrationReauth
		}
	}
	cipher, err := s.sealIntegration("account", org, account, secret)
	if err != nil {
		return "", err
	}
	var check domain.ExecutionCredential
	if err = s.openIntegration("account", org, account, cipher, &check); err != nil || check.Secret != secret.Secret || check.RefreshToken != secret.RefreshToken {
		return "", domain.ErrProviderNotConfigured
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO public.seo_integration_accounts
		(id,organization_id,provider,application_id,auth_mode,label,granted_scopes,encrypted_credentials,authorization_status)
		VALUES($1,$2,$3,NULLIF($4,'')::uuid,$5,$6,$7,$8,$9)`, account, org, provider, app, mode, label, scopes, cipher, status)
	return account, err
}
