package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/Autumn-27/artex/seoos/internal/domain"
	"github.com/google/uuid"
)

func integrationAAD(kind, org, id string) []byte {
	return []byte("seoos/integrations/" + kind + "/" + org + "/" + id)
}

func (s *Store) sealIntegration(kind, org, id string, value any) ([]byte, error) {
	if s.providerCipher == nil {
		return nil, domain.ErrProviderNotConfigured
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return s.providerCipher.Seal(nil, nil, raw, integrationAAD(kind, org, id)), nil
}

func (s *Store) openIntegration(kind, org, id string, ciphertext []byte, value any) error {
	if s.providerCipher == nil || len(ciphertext) == 0 {
		return domain.ErrProviderNotConfigured
	}
	raw, err := s.providerCipher.Open(nil, nil, ciphertext, integrationAAD(kind, org, id))
	if err != nil || json.Unmarshal(raw, value) != nil {
		return domain.ErrProviderNotConfigured
	}
	return nil
}

func integrationInput(field, message string) error {
	return &domain.ValidationError{Field: field, Message: message}
}

const integrationAppData = `to_jsonb(p)-'encrypted_client_secret'-'organization_id' ||
	jsonb_build_object('configured',p.encrypted_client_secret IS NOT NULL)`

func integrationApplication(ctx context.Context, db rowQueryer, org, id, lock string) (domain.IntegrationApplication, []byte, error) {
	var value domain.IntegrationApplication
	var data, cipher []byte
	if !domain.ValidID(id) {
		return value, nil, domain.ErrNotFound
	}
	err := db.QueryRowContext(ctx, `SELECT `+integrationAppData+`,p.encrypted_client_secret
		FROM public.seo_integration_applications p WHERE p.organization_id=$1 AND p.id=$2 AND p.deleted_at IS NULL`+lock,
		org, id).Scan(&data, &cipher)
	if err != nil {
		return value, nil, classifyError(err)
	}
	err = json.Unmarshal(data, &value)
	return value, cipher, err
}

func (s *Store) SaveIntegrationApplication(ctx context.Context, org, id string, in domain.IntegrationApplicationInput) (domain.IntegrationApplication, error) {
	var value domain.IntegrationApplication
	if !executionOwner(ctx, org) {
		return value, domain.ErrExecutionAuthority
	}
	in.Label = strings.TrimSpace(in.Label)
	if !domain.ValidIntegrationText(in.Label, 120) || !domain.ValidIntegrationSecret(in.ClientID) || len(in.ClientID) > 512 ||
		!domain.ValidIntegrationCallback(in.RedirectURI, in.Provider) {
		return value, integrationInput("application", "provide a label, client ID, and exact HTTPS integration callback")
	}
	scopes, err := domain.IntegrationScopes(in.Provider, in.AllowedScopes)
	if err != nil {
		return value, err
	}
	create := id == ""
	if create {
		id = uuid.NewString()
	}
	if !domain.ValidID(id) {
		return value, domain.ErrNotFound
	}
	var sealed []byte
	if create || in.ClientSecret != "" {
		if !domain.ValidIntegrationSecret(in.ClientSecret) || len(in.ClientSecret) > 512 {
			return value, integrationInput("client_secret", "enter the application's client secret")
		}
		sealed, err = s.sealIntegration("application", org, id, in.ClientSecret)
		if err != nil {
			return value, err
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return value, err
	}
	defer tx.Rollback()
	if create {
		_, err = tx.ExecContext(ctx, `INSERT INTO public.seo_integration_applications
			(id,organization_id,provider,label,client_id,encrypted_client_secret,redirect_uri,allowed_scopes,enabled)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`, id, org, in.Provider, in.Label, in.ClientID, sealed, in.RedirectURI, scopes, in.Enabled)
	} else {
		before, _, err := integrationApplication(ctx, tx, org, id, " FOR NO KEY UPDATE OF p")
		if err != nil {
			return value, err
		}
		if before.Version != in.Version {
			return value, domain.ErrVersionConflict
		}
		if before.Provider != in.Provider {
			return value, integrationInput("provider", "an application's provider is permanent")
		}
		if before.ClientID != in.ClientID || before.RedirectURI != in.RedirectURI {
			if err := ensureNoIntegrationDependency(ctx, tx, org, id, true); err != nil {
				return value, err
			}
			if sealed == nil {
				return value, integrationInput("client_secret", "re-enter the secret for changed OAuth application coordinates")
			}
		}
		if _, err = tx.ExecContext(ctx, `UPDATE public.seo_integration_applications SET label=$3,client_id=$4,redirect_uri=$5,allowed_scopes=$6,
			enabled=$7,encrypted_client_secret=coalesce($8,encrypted_client_secret),config_version=config_version+1,updated_at=clock_timestamp()
			WHERE organization_id=$1 AND id=$2`, org, id, in.Label, in.ClientID, in.RedirectURI, scopes, in.Enabled, sealed); err != nil {
			return value, classifyError(err)
		}
		if before.Enabled != in.Enabled || sealed != nil || !slices.Equal(before.AllowedScopes, scopes) {
			if _, err = tx.ExecContext(ctx, `UPDATE public.seo_integration_accounts SET credential_version=credential_version+1,
				updated_at=clock_timestamp() WHERE organization_id=$1 AND application_id=$2 AND deleted_at IS NULL`, org, id); err != nil {
				return value, err
			}
		}
	}
	if err != nil {
		return value, classifyError(err)
	}
	value, _, err = integrationApplication(ctx, tx, org, id, "")
	if err != nil {
		return value, err
	}
	if err = audit(ctx, tx, org, "integration.application.save", id, map[string]any{"provider": in.Provider, "rotated": sealed != nil, "version": value.Version}); err != nil {
		return value, err
	}
	return value, tx.Commit()
}

const integrationAccountData = `to_jsonb(a)-'encrypted_credentials'-'organization_id'-'verified_subject' ||
	jsonb_build_object('identity_status',CASE WHEN a.provider='bing' THEN 'UNAVAILABLE'
	WHEN a.verified_subject IS NOT NULL THEN 'VERIFIED' ELSE 'UNVERIFIED' END)`

func integrationAccount(ctx context.Context, db rowQueryer, org, id, lock string) (domain.IntegrationAccount, []byte, error) {
	var value domain.IntegrationAccount
	if !domain.ValidID(id) {
		return value, nil, domain.ErrNotFound
	}
	var data, cipher []byte
	err := db.QueryRowContext(ctx, `SELECT `+integrationAccountData+`,a.encrypted_credentials,coalesce(a.verified_subject,'')
		FROM public.seo_integration_accounts a WHERE a.organization_id=$1 AND a.id=$2 AND a.deleted_at IS NULL`+lock,
		org, id).Scan(&data, &cipher, &value.VerifiedSubject)
	if err != nil {
		return value, nil, classifyError(err)
	}
	err = json.Unmarshal(data, &value)
	return value, cipher, err
}

func (s *Store) IntegrationAccount(ctx context.Context, org, id string) (domain.IntegrationAccount, error) {
	value, _, err := integrationAccount(ctx, s.db, org, id, "")
	return value, err
}

func (s *Store) CreateIntegrationAccount(ctx context.Context, org string, in domain.IntegrationAccountInput) (domain.IntegrationAccount, error) {
	var value domain.IntegrationAccount
	if !executionOwner(ctx, org) {
		return value, domain.ErrExecutionAuthority
	}
	in.Label = strings.TrimSpace(in.Label)
	if !domain.ValidIntegrationText(in.Label, 120) ||
		in.Provider != domain.IntegrationGoogle && in.Provider != domain.IntegrationBing {
		return value, integrationInput("account", "provide a valid provider and account label")
	}
	if in.RequestsPerMinute == 0 {
		in.RequestsPerMinute = 60
	}
	if in.RequestsPerDay == 0 {
		in.RequestsPerDay = 1000
	}
	if in.RequestsPerMinute < 1 || in.RequestsPerMinute > 120 || in.RequestsPerDay < 1 || in.RequestsPerDay > 25000 {
		return value, integrationInput("limits", "provide bounded account request limits")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return value, err
	}
	defer tx.Rollback()
	id, status := uuid.NewString(), domain.IntegrationReauth
	var cipher []byte
	switch in.Mode {
	case domain.IntegrationOAuth:
		app, _, err := integrationApplication(ctx, tx, org, in.ApplicationID, " FOR KEY SHARE OF p")
		if err != nil {
			return value, err
		}
		if !app.Enabled || app.Provider != in.Provider || in.APIKey != "" {
			return value, domain.ErrExecutionAuthority
		}
	case domain.IntegrationAPIKey:
		if in.Provider != domain.IntegrationBing || in.ApplicationID != "" || !domain.ValidIntegrationSecret(in.APIKey) || len(in.APIKey) > 512 {
			return value, integrationInput("api_key", "only Bing accepts a Webmaster account API key")
		}
		cipher, err = s.sealIntegration("account", org, id, domain.ExecutionCredential{Secret: in.APIKey})
		if err != nil {
			return value, err
		}
		status = domain.IntegrationConnected
	default:
		return value, integrationInput("auth_mode", "choose OAuth or Bing API key")
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO public.seo_integration_accounts
		(id,organization_id,provider,application_id,label,auth_mode,encrypted_credentials,authorization_status,requests_per_minute,requests_per_day)
		VALUES ($1,$2,$3,NULLIF($4,'')::uuid,$5,$6,$7,$8,$9,$10)`,
		id, org, in.Provider, in.ApplicationID, in.Label, in.Mode, cipher, status, in.RequestsPerMinute, in.RequestsPerDay); err != nil {
		return value, classifyError(err)
	}
	value, _, err = integrationAccount(ctx, tx, org, id, "")
	if err != nil {
		return value, err
	}
	if err = audit(ctx, tx, org, "integration.account.create", id, map[string]string{"provider": in.Provider, "mode": in.Mode}); err != nil {
		return value, err
	}
	return value, tx.Commit()
}

func (s *Store) integrationAccess(ctx context.Context, db rowQueryer, org, id, lock string) (domain.IntegrationAccess, error) {
	var out domain.IntegrationAccess
	account, cipher, err := integrationAccount(ctx, db, org, id, lock)
	if err != nil {
		return out, err
	}
	out.Account = account
	if len(cipher) > 0 {
		if err = s.openIntegration("account", org, id, cipher, &out.Credential); err != nil {
			return out, err
		}
	}
	if account.Mode == domain.IntegrationOAuth {
		var appCipher []byte
		out.Application, appCipher, err = integrationApplication(ctx, db, org, account.ApplicationID, "")
		if err != nil {
			return out, err
		}
		if !out.Application.Enabled {
			return out, domain.ErrIntegrationRevoked
		}
		if err = s.openIntegration("application", org, out.Application.ID, appCipher, &out.Credential.ClientSecret); err != nil {
			return out, err
		}
		out.Credential.ClientID = out.Application.ClientID
	}
	return out, nil
}

func (s *Store) IntegrationAccess(ctx context.Context, org, id string) (domain.IntegrationAccess, error) {
	return s.integrationAccess(ctx, s.db, org, id, "")
}

type IntegrationState struct {
	Access   domain.IntegrationAccess
	Scopes   []string
	Verifier string
}

func integrationActor(ctx context.Context) string {
	p := domain.PrincipalFromContext(ctx)
	return p.Kind + ":" + p.UserID
}

func (s *Store) StartIntegrationOAuth(ctx context.Context, org, id string, requested []string) (IntegrationState, string, error) {
	var state IntegrationState
	if !executionOwner(ctx, org) {
		return state, "", domain.ErrExecutionAuthority
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return state, "", err
	}
	defer tx.Rollback()
	state.Access, err = s.integrationAccess(ctx, tx, org, id, " FOR NO KEY UPDATE OF a")
	if err != nil {
		return state, "", err
	}
	a, app := state.Access.Account, state.Access.Application
	if a.Mode != domain.IntegrationOAuth {
		return state, "", domain.ErrExecutionUnsupported
	}
	state.Scopes, err = domain.IntegrationScopes(a.Provider, requested)
	if err != nil {
		return state, "", err
	}
	for _, scope := range state.Scopes {
		if !slices.Contains(app.AllowedScopes, scope) {
			return state, "", domain.ErrExecutionAuthority
		}
	}
	rawKey, rawProof := make([]byte, 32), make([]byte, 32)
	if _, err = rand.Read(rawKey); err != nil {
		return state, "", err
	}
	if _, err = rand.Read(rawProof); err != nil {
		return state, "", err
	}
	key := base64.RawURLEncoding.EncodeToString(rawKey)
	state.Verifier = base64.RawURLEncoding.EncodeToString(rawProof)
	sealed, err := s.sealIntegration("oauth_state", org, id, state.Verifier)
	if err != nil {
		return state, "", err
	}
	digest := sha256.Sum256([]byte(key))
	if _, err = tx.ExecContext(ctx, `DELETE FROM public.seo_integration_oauth_states WHERE expires_at<=clock_timestamp()
		OR organization_id=$1 AND account_id=$2`, org, id); err != nil {
		return state, "", err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO public.seo_integration_oauth_states
		(state_hash,organization_id,account_id,account_version,config_version,provider,redirect_uri,actor_key,requested_scopes,verifier,expires_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,clock_timestamp()+interval '10 minutes')`,
		digest[:], org, id, a.Version, app.Version, a.Provider, app.RedirectURI, integrationActor(ctx), state.Scopes, sealed); err != nil {
		return state, "", err
	}
	if err = audit(ctx, tx, org, "integration.oauth.start", id, map[string]any{"provider": a.Provider, "scopes": state.Scopes}); err != nil {
		return state, "", err
	}
	return state, key, tx.Commit()
}

func (s *Store) ConsumeIntegrationOAuth(ctx context.Context, org, provider, key, callback string) (IntegrationState, error) {
	var state IntegrationState
	if !executionOwner(ctx, org) || len(key) != 43 || !domain.ValidIntegrationCallback(callback, provider) {
		return state, domain.ErrExecutionAuthority
	}
	digest := sha256.Sum256([]byte(key))
	var id, redirect string
	var aVersion, pVersion int64
	var scopes, verifier []byte
	err := s.db.QueryRowContext(ctx, `DELETE FROM public.seo_integration_oauth_states
		WHERE state_hash=$1 AND organization_id=$2 AND provider=$3 AND redirect_uri=$4 AND actor_key=$5
		AND expires_at>clock_timestamp() RETURNING account_id,account_version,config_version,redirect_uri,to_jsonb(requested_scopes),verifier`,
		digest[:], org, provider, callback, integrationActor(ctx)).Scan(&id, &aVersion, &pVersion, &redirect, &scopes, &verifier)
	if errors.Is(err, sql.ErrNoRows) {
		return state, domain.ErrExecutionAuthority
	}
	if err != nil {
		return state, err
	}
	state.Access, err = s.IntegrationAccess(ctx, org, id)
	if err != nil {
		return state, err
	}
	if state.Access.Account.Version != aVersion || state.Access.Application.Version != pVersion || state.Access.Application.RedirectURI != redirect {
		return state, domain.ErrVersionConflict
	}
	if err = json.Unmarshal(scopes, &state.Scopes); err != nil {
		return state, err
	}
	err = s.openIntegration("oauth_state", org, id, verifier, &state.Verifier)
	return state, err
}

func (s *Store) SaveIntegrationGrant(ctx context.Context, org string, prior domain.IntegrationAccess, secret domain.ExecutionCredential, scopes []string) (domain.IntegrationAccount, error) {
	var result domain.IntegrationAccount
	if !executionOwner(ctx, org) {
		return result, domain.ErrExecutionAuthority
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	id := prior.Account.ID
	current, err := s.integrationAccess(ctx, tx, org, id, " FOR NO KEY UPDATE OF a")
	if err != nil {
		return result, err
	}
	if current.Account.Version != prior.Account.Version || current.Application.Version != prior.Application.Version {
		return result, domain.ErrVersionConflict
	}
	if err = checkIntegrationSubject(current.Account, secret.ExternalSubject, true); err != nil {
		return result, err
	}
	if err = s.writeIntegrationGrant(ctx, tx, org, id, secret, scopes, true); err != nil {
		return result, err
	}
	if err = audit(ctx, tx, org, "integration.oauth.connected", id, map[string]any{"provider": current.Account.Provider, "scopes": scopes}); err != nil {
		return result, err
	}
	result, _, err = integrationAccount(ctx, tx, org, id, "")
	if err != nil {
		return result, err
	}
	return result, tx.Commit()
}

func (s *Store) writeIntegrationGrant(ctx context.Context, tx *sql.Tx, org, id string, secret domain.ExecutionCredential, scopes []string, bump bool) error {
	subject := secret.ExternalSubject
	secret.ClientID, secret.ClientSecret, secret.ExternalSubject = "", "", ""
	sealed, err := s.sealIntegration("account", org, id, secret)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE public.seo_integration_accounts SET encrypted_credentials=$3,granted_scopes=$4,
		expires_at=$5,last_refresh_at=clock_timestamp(),authorization_status='CONNECTED',
		verified_subject=coalesce(nullif($7,''),verified_subject),
		identity_verified_at=CASE WHEN $7<>'' AND ($6 OR verified_subject IS NULL) THEN clock_timestamp() ELSE identity_verified_at END,
		credential_version=credential_version+CASE WHEN $6 THEN 1 ELSE 0 END,updated_at=clock_timestamp()
		WHERE organization_id=$1 AND id=$2`, org, id, sealed, scopes, secret.ExpiresAt, bump, subject)
	return err
}

func checkIntegrationSubject(account domain.IntegrationAccount, subject string, required bool) error {
	if account.Provider != domain.IntegrationGoogle {
		if subject != "" {
			return &domain.DataError{Code: "integration_oauth_invalid_identity"}
		}
		return nil
	}
	if (required || subject != "") && !domain.ValidIntegrationSubject(subject) {
		return &domain.DataError{Code: "integration_oauth_invalid_identity"}
	}
	if account.VerifiedSubject != "" && subject != "" && subject != account.VerifiedSubject {
		return &domain.DataError{Code: "integration_oauth_identity_mismatch"}
	}
	return nil
}

func (s *Store) RefreshIntegration(ctx context.Context, org, id string, expected int64, force bool,
	refresh func(context.Context, domain.IntegrationAccess) (domain.ExecutionCredential, []string, error),
) (domain.IntegrationAccess, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.IntegrationAccess{}, err
	}
	defer tx.Rollback()
	access, err := s.integrationAccess(ctx, tx, org, id, " FOR NO KEY UPDATE OF a")
	if err != nil {
		return access, err
	}
	a := access.Account
	if a.Version != expected || a.Status != domain.IntegrationConnected {
		return access, domain.ErrIntegrationRevoked
	}
	if a.Mode != domain.IntegrationOAuth || !force && access.Credential.AccessToken != "" && a.ExpiresAt != nil && time.Until(*a.ExpiresAt) >= time.Minute {
		return access, tx.Commit()
	}
	secret, scopes, err := refresh(ctx, access)
	if err == nil {
		err = checkIntegrationSubject(a, secret.ExternalSubject, false)
	}
	if err != nil {
		var problem *domain.DataError
		if errors.As(err, &problem) && (problem.Code == "integration_invalid_grant" || problem.Code == "integration_oauth_scope_required" ||
			problem.Code == "integration_oauth_identity_mismatch") {
			if _, dbErr := tx.ExecContext(ctx, `UPDATE public.seo_integration_accounts SET authorization_status='REAUTH_REQUIRED',encrypted_credentials=NULL,granted_scopes='{}',expires_at=NULL,
				credential_version=credential_version+1,updated_at=clock_timestamp() WHERE organization_id=$1 AND id=$2`, org, id); dbErr != nil {
				return access, dbErr
			}
			if dbErr := disableIntegrationBindings(ctx, tx, org, id); dbErr != nil {
				return access, dbErr
			}
			if dbErr := audit(ctx, tx, org, "integration.oauth.reauth_required", id, map[string]string{"code": problem.Code}); dbErr != nil {
				return access, dbErr
			}
			if dbErr := tx.Commit(); dbErr != nil {
				return access, dbErr
			}
		}
		return access, err
	}
	if err = s.writeIntegrationGrant(ctx, tx, org, id, secret, scopes, false); err != nil {
		return access, err
	}
	access.Credential = secret
	access.Account.ExpiresAt = secret.ExpiresAt
	access.Account.Scopes = scopes
	if err = audit(ctx, tx, org, "integration.oauth.refresh", id, map[string]string{"provider": a.Provider}); err != nil {
		return access, err
	}
	return access, tx.Commit()
}

func ensureNoIntegrationDependency(ctx context.Context, tx *sql.Tx, org, id string, application bool) error {
	table, column := "seo_site_integration_bindings", "integration_account_id"
	if application {
		table, column = "seo_integration_accounts", "application_id"
	}
	var present bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM public.`+table+` WHERE organization_id=$1 AND `+column+`=$2 AND deleted_at IS NULL)`, org, id).Scan(&present)
	if err == nil && present {
		return domain.ErrIntegrationDependency
	}
	return err
}

func (s *Store) RemoveIntegrationApplication(ctx context.Context, org, id string) error {
	if !executionOwner(ctx, org) {
		return domain.ErrExecutionAuthority
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, _, err = integrationApplication(ctx, tx, org, id, " FOR UPDATE OF p"); err != nil {
		return err
	}
	if err = ensureNoIntegrationDependency(ctx, tx, org, id, true); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE public.seo_integration_applications SET enabled=false,encrypted_client_secret=NULL,
		deleted_at=clock_timestamp(),updated_at=clock_timestamp() WHERE organization_id=$1 AND id=$2`, org, id); err != nil {
		return err
	}
	if err = audit(ctx, tx, org, "integration.application.delete", id, nil); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) RevokeIntegrationAccount(ctx context.Context, org, id string, remove bool) (domain.IntegrationAccess, error) {
	var out domain.IntegrationAccess
	if !executionOwner(ctx, org) {
		return out, domain.ErrExecutionAuthority
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	a, cipher, err := integrationAccount(ctx, tx, org, id, " FOR UPDATE OF a")
	if err != nil {
		return out, err
	}
	out.Account = a
	if len(cipher) > 0 {
		if err = s.openIntegration("account", org, id, cipher, &out.Credential); err != nil {
			return out, err
		}
	}
	if remove {
		if err = ensureNoIntegrationDependency(ctx, tx, org, id, false); err != nil {
			return out, err
		}
	}
	_, err = tx.ExecContext(ctx, `UPDATE public.seo_integration_accounts SET authorization_status='REVOKED',encrypted_credentials=NULL,
		granted_scopes='{}',expires_at=NULL,credential_version=credential_version+1,updated_at=clock_timestamp(),
		deleted_at=CASE WHEN $3 THEN clock_timestamp() ELSE deleted_at END WHERE organization_id=$1 AND id=$2`, org, id, remove)
	if err != nil {
		return out, err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM public.seo_integration_oauth_states WHERE organization_id=$1 AND account_id=$2`, org, id); err != nil {
		return out, err
	}
	if err = disableIntegrationBindings(ctx, tx, org, id); err != nil {
		return out, err
	}
	if err = audit(ctx, tx, org, "integration.account.revoke", id, map[string]bool{"removed": remove}); err != nil {
		return out, err
	}
	return out, tx.Commit()
}

func (s *Store) RotateBingAPIKey(ctx context.Context, org, id, key string, expected int64) error {
	if !executionOwner(ctx, org) {
		return domain.ErrExecutionAuthority
	}
	if !domain.ValidIntegrationSecret(key) || len(key) > 512 {
		return integrationInput("api_key", "provide a valid Bing account key")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	a, _, err := integrationAccount(ctx, tx, org, id, " FOR NO KEY UPDATE OF a")
	if err != nil {
		return err
	}
	if a.Mode != domain.IntegrationAPIKey {
		return domain.ErrExecutionUnsupported
	}
	if a.Version != expected {
		return domain.ErrVersionConflict
	}
	if err = s.writeIntegrationGrant(ctx, tx, org, id, domain.ExecutionCredential{Secret: key}, []string{}, true); err != nil {
		return err
	}
	if err = audit(ctx, tx, org, "integration.account.key_rotate", id, nil); err != nil {
		return err
	}
	return tx.Commit()
}

func disableIntegrationBindings(ctx context.Context, tx *sql.Tx, org, account string) error {
	_, err := tx.ExecContext(ctx, `UPDATE public.seo_site_integration_bindings SET enabled=false,updated_at=clock_timestamp()
		WHERE organization_id=$1 AND integration_account_id=$2 AND deleted_at IS NULL AND enabled`, org, account)
	return err
}
