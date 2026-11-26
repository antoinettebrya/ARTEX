package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"

	"github.com/Autumn-27/artex/seoos/internal/domain"
	"github.com/google/uuid"
)

func executionOwner(ctx context.Context, org string) bool {
	p := domain.PrincipalFromContext(ctx)
	return p.Role == domain.RoleOwner && p.OrganizationID == org
}

const executionConnectionSelect = `SELECT c.settings || jsonb_build_object('id',c.id,'organization_id',c.organization_id,
	'version',c.version,'updated_at',c.updated_at,'configured',c.credential IS NOT NULL AND c.configured_site_url=s.base_url,
	'capabilities',COALESCE(k.data->'capabilities','{}'::jsonb)),c.credential,c.configured_site_url,s.base_url,s.status
	FROM public.seo_cms_connections c JOIN public.seo_sites s ON (s.organization_id,s.id)=(c.organization_id,c.site_id)
	LEFT JOIN public.seo_cms_capabilities k ON (k.organization_id,k.site_id,k.connection_id)=(c.organization_id,c.site_id,c.id)
	WHERE c.organization_id=$1 AND c.site_id=$2 AND c.id=$3 `

func execConnection(ctx context.Context, db rowQueryer, org, site, id, lock string) (domain.ExecutionConnection, []byte, bool, error) {
	var c domain.ExecutionConnection
	var raw, secret []byte
	var bound, base, status string
	if !domain.ValidID(site) || !domain.ValidID(id) {
		return c, nil, false, domain.ErrNotFound
	}
	err := db.QueryRowContext(ctx, executionConnectionSelect+lock, org, site, id).Scan(&raw, &secret, &bound, &base, &status)
	if err != nil {
		return c, nil, false, classifyError(err)
	}
	err = json.Unmarshal(raw, &c)
	return c, secret, bound == base && status == "active", err
}

func execBinding(org, site, id string) []byte {
	return []byte("seoos/execution/" + org + "/" + site + "/" + id)
}

func (s *Store) sealExecution(org, site, id string, secret domain.ExecutionCredential) ([]byte, error) {
	if s.providerCipher == nil {
		return nil, domain.ErrProviderNotConfigured
	}
	bytes, err := json.Marshal(secret)
	if err != nil {
		return nil, err
	}
	return s.providerCipher.Seal(nil, nil, bytes, execBinding(org, site, id)), nil
}

func (s *Store) ExecutionConnectionAccess(ctx context.Context, org, site, id string) (domain.ExecutionConnection, domain.ExecutionCredential, error) {
	c, encrypted, allowed, err := execConnection(ctx, s.db, org, site, id, "")
	var secret domain.ExecutionCredential
	if err != nil {
		return c, secret, err
	}
	if !allowed || !c.Enabled {
		return c, secret, domain.ErrExecutionAuthority
	}
	if s.providerCipher == nil || len(encrypted) == 0 {
		return c, secret, domain.ErrProviderNotConfigured
	}
	clear, err := s.providerCipher.Open(nil, nil, encrypted, execBinding(org, site, id))
	if err != nil || json.Unmarshal(clear, &secret) != nil {
		return c, secret, domain.ErrProviderNotConfigured
	}
	return c, secret, nil
}

func (s *Store) SaveExecutionConnection(ctx context.Context, org, id string, in domain.ExecutionConnectionSave) (domain.ExecutionConnection, error) {
	var result domain.ExecutionConnection
	if !executionOwner(ctx, org) {
		return result, domain.ErrExecutionAuthority
	}
	site, err := s.GetSite(ctx, org, in.SiteID)
	if err != nil {
		return result, err
	}
	config, err := in.ExecutionConnectionInput.Normalize(site)
	if err != nil {
		return result, err
	}
	creating := id == ""
	if creating {
		id = uuid.NewString()
	}
	if !domain.ValidID(id) {
		return result, domain.ErrNotFound
	}
	if creating && in.Credential == nil {
		return result, &domain.ValidationError{Field: "credential", Message: "configure the site's credential"}
	}
	var encrypted []byte
	if in.Credential != nil {
		if err := in.Credential.Validate(config); err != nil {
			return result, err
		}
		in.Credential.AccessToken, in.Credential.ExpiresAt = "", nil
		encrypted, err = s.sealExecution(org, site.ID, id, *in.Credential)
		if err != nil {
			return result, err
		}
	}
	payload, err := json.Marshal(config)
	if err != nil {
		return result, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	var base string
	if err = tx.QueryRowContext(ctx, `SELECT base_url FROM public.seo_sites WHERE organization_id=$1 AND id=$2 FOR SHARE`, org, site.ID).Scan(&base); err != nil {
		return result, classifyError(err)
	}
	if base != site.BaseURL {
		return result, domain.ErrVersionConflict
	}
	if creating {
		_, err = tx.ExecContext(ctx, `INSERT INTO public.seo_cms_connections (id,organization_id,site_id,kind,configured_site_url,name,settings,credential)
			VALUES ($1,$2,$3,$4,$5,$6,$7::jsonb,$8)`, id, org, site.ID, config.Kind, base, config.Name, string(payload), encrypted)
	} else {
		before, _, same, e := execConnection(ctx, tx, org, site.ID, id, "FOR NO KEY UPDATE OF c")
		if e != nil {
			return result, e
		}
		if before.Version != in.Version {
			return result, domain.ErrVersionConflict
		}
		if before.Kind != config.Kind || !same && encrypted == nil {
			return result, &domain.ValidationError{Field: "credential", Message: "re-authorize a changed site or keep the connection type"}
		}
		if config.Kind == domain.ExecutionIndexNow && before.KeyLocation != config.KeyLocation && encrypted == nil {
			return result, &domain.ValidationError{Field: "credential", Message: "rotate the key when its location changes"}
		}
		_, err = tx.ExecContext(ctx, `UPDATE public.seo_cms_connections SET configured_site_url=$4,name=$5,settings=$6::jsonb,
			credential=COALESCE($7,credential),version=version+1,updated_at=clock_timestamp()
			WHERE organization_id=$1 AND site_id=$2 AND id=$3`, org, site.ID, id, base, config.Name, string(payload), encrypted)
		if err == nil {
			_, err = tx.ExecContext(ctx, `DELETE FROM public.seo_cms_capabilities WHERE organization_id=$1 AND site_id=$2 AND connection_id=$3`, org, site.ID, id)
		}
	}
	if err != nil {
		return result, classifyError(err)
	}
	result, _, _, err = execConnection(ctx, tx, org, site.ID, id, "")
	if err != nil {
		return result, err
	}
	if err = audit(ctx, tx, org, "execution.connection.save", id, map[string]any{
		"site_id": site.ID, "kind": config.Kind, "enabled": config.Enabled, "allow_writes": config.AllowWrites,
		"credential_rotated": encrypted != nil, "requests_per_day": config.RequestsPerDay,
	}); err != nil {
		return result, err
	}
	return result, tx.Commit()
}

func (s *Store) SaveExecutionTokens(ctx context.Context, c domain.ExecutionConnection, secret domain.ExecutionCredential) error {
	raw, err := s.sealExecution(c.OrganizationID, c.SiteID, c.ID, secret)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	r, err := tx.ExecContext(ctx, `UPDATE public.seo_cms_connections c SET credential=$5
		FROM public.seo_sites s WHERE s.organization_id=c.organization_id AND s.id=c.site_id AND s.base_url=c.configured_site_url
		AND c.organization_id=$1 AND c.site_id=$2 AND c.id=$3 AND c.version=$4 AND (c.settings->>'enabled')::boolean`,
		c.OrganizationID, c.SiteID, c.ID, c.Version, raw)
	if err != nil {
		return err
	}
	n, err := r.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return domain.ErrVersionConflict
	}
	if err = audit(ctx, tx, c.OrganizationID, "execution.oauth.tokens", c.ID, map[string]string{"kind": c.Kind, "site_id": c.SiteID}); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) OAuthState(ctx context.Context, c domain.ExecutionConnection) (string, string, error) {
	if !executionOwner(ctx, c.OrganizationID) || s.providerCipher == nil {
		return "", "", domain.ErrExecutionAuthority
	}
	state, verifier := make([]byte, 32), make([]byte, 32)
	if _, err := rand.Read(state); err != nil {
		return "", "", err
	}
	if _, err := rand.Read(verifier); err != nil {
		return "", "", err
	}
	key, proof := base64.RawURLEncoding.EncodeToString(state), base64.RawURLEncoding.EncodeToString(verifier)
	digest := sha256.Sum256([]byte(key))
	sealed := s.providerCipher.Seal(nil, nil, []byte(proof), execBinding(c.OrganizationID, c.SiteID, c.ID))
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", "", err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `DELETE FROM public.seo_execution_oauth_states WHERE organization_id=$1 AND connection_id=$2`, c.OrganizationID, c.ID); err != nil {
		return "", "", err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO public.seo_execution_oauth_states
		(state_hash,organization_id,site_id,connection_id,connection_version,verifier,expires_at)
		VALUES ($1,$2,$3,$4,$5,$6,clock_timestamp()+interval '10 minutes')`, digest[:], c.OrganizationID, c.SiteID, c.ID, c.Version, sealed)
	if err != nil {
		return "", "", err
	}
	if err = audit(ctx, tx, c.OrganizationID, "execution.oauth.start", c.ID, map[string]string{"site_id": c.SiteID}); err != nil {
		return "", "", err
	}
	return key, proof, tx.Commit()
}

func (s *Store) ConsumeOAuthState(ctx context.Context, c domain.ExecutionConnection, state string) (string, error) {
	if !executionOwner(ctx, c.OrganizationID) || s.providerCipher == nil || len(state) != 43 {
		return "", domain.ErrExecutionAuthority
	}
	digest := sha256.Sum256([]byte(state))
	var sealed []byte
	err := s.db.QueryRowContext(ctx, `DELETE FROM public.seo_execution_oauth_states
		WHERE state_hash=$1 AND organization_id=$2 AND site_id=$3 AND connection_id=$4 AND connection_version=$5
		AND expires_at>clock_timestamp() RETURNING verifier`, digest[:], c.OrganizationID, c.SiteID, c.ID, c.Version).Scan(&sealed)
	if errors.Is(err, sql.ErrNoRows) {
		return "", domain.ErrExecutionAuthority
	}
	if err != nil {
		return "", err
	}
	clear, err := s.providerCipher.Open(nil, nil, sealed, execBinding(c.OrganizationID, c.SiteID, c.ID))
	if err != nil {
		return "", domain.ErrExecutionAuthority
	}
	return string(clear), nil
}

func (s *Store) UseExecutionRequest(ctx context.Context, org, site, connection string, version int64, lease *domain.Lease) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var jobID string
	if lease != nil {
		job, err := lockLease(ctx, tx, *lease)
		if err != nil {
			return err
		}
		if job.OrganizationID != org || job.SiteID != site {
			return domain.ErrExecutionAuthority
		}
		jobID = job.ID
	} else if !executionOwner(ctx, org) {
		return domain.ErrExecutionAuthority
	}
	rpm, dayLimit := 60, 1000
	if connection != "" {
		c, _, active, err := execConnection(ctx, tx, org, site, connection, "FOR NO KEY UPDATE OF c")
		if err != nil {
			return err
		}
		if !active || !c.Enabled || c.Version != version {
			return domain.ErrExecutionAuthority
		}
		rpm, dayLimit = c.RequestsPerMinute, c.RequestsPerDay
	} else if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "execution:"+org+":"+site); err != nil {
		return err
	}
	var minuteCount, dayCount int
	var oldest sql.NullTime
	var now time.Time
	if err := tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return err
	}
	day := now.UTC().Truncate(24 * time.Hour)
	minute := now.Add(-time.Minute)
	err = tx.QueryRowContext(ctx, `SELECT count(*) FILTER (WHERE requested_at > $4),
		count(*) FILTER (WHERE requested_at >= $5),min(requested_at) FILTER (WHERE requested_at > $4)
		FROM public.seo_execution_request_calls WHERE organization_id=$1 AND site_id=$2
		AND connection_id IS NOT DISTINCT FROM NULLIF($3,'')::uuid AND requested_at >= least($4::timestamptz,$5::timestamptz)`,
		org, site, connection, minute, day).Scan(&minuteCount, &dayCount, &oldest)
	if err != nil {
		return err
	}
	var wait time.Duration
	if dayCount >= dayLimit {
		wait = day.Add(24 * time.Hour).Sub(now)
	} else if minuteCount >= rpm {
		wait = oldest.Time.Add(time.Minute).Sub(now)
	}
	if wait > 0 {
		return &domain.DataError{Code: "execution_budget", Retry: true, RetryAfter: wait, KnownUnbilled: true}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO public.seo_execution_request_calls (organization_id,site_id,connection_id,job_id)
		VALUES ($1,$2,NULLIF($3,'')::uuid,NULLIF($4,'')::uuid)`, org, site, connection, jobID)
	if err != nil {
		return err
	}
	return tx.Commit()
}
