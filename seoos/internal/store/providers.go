package store

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/Autumn-27/artex/seoos/internal/contentmodel"
	"github.com/Autumn-27/artex/seoos/internal/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

// SetProviderKey is called once at startup, before serving requests.
func (s *Store) SetProviderKey(key []byte) error {
	if len(key) == 0 {
		return nil
	}
	if len(key) != 32 {
		return errors.New("provider key must contain 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return err
	}
	s.providerCipher, err = cipher.NewGCMWithRandomNonce(block)
	return err
}

const providerColumns = `id::text, organization_id::text, name, model, timeout_seconds,
	requests_per_minute, requests_per_day, max_output_tokens, enabled, version, created_at, updated_at, kind, daily_cost_micros, model_options`

func scanProvider(row rowScanner, tail ...any) (domain.Provider, error) {
	var provider domain.Provider
	var options []byte
	dest := []any{&provider.ID, &provider.OrganizationID, &provider.Name, &provider.Model, &provider.TimeoutSeconds,
		&provider.RequestsPerMinute, &provider.RequestsPerDay, &provider.MaxOutputTokens, &provider.Enabled,
		&provider.Version, &provider.CreatedAt, &provider.UpdatedAt, &provider.Kind, &provider.DailyCostMicros, &options}
	err := row.Scan(append(dest, tail...)...)
	provider.CreatedAt = provider.CreatedAt.UTC()
	provider.UpdatedAt = provider.UpdatedAt.UTC()
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "seo_providers_organization_id_name_key" {
		return domain.Provider{}, domain.ErrProviderNameTaken
	}
	if err == nil && len(options) > 0 {
		err = json.Unmarshal(options, &provider.ModelOptions)
	}
	return provider, classifyError(err)
}

func (s *Store) ListProviders(ctx context.Context, orgID, category string, limit, offset int) ([]domain.Provider, error) {
	if err := validateOrganizationID(orgID); err != nil {
		return nil, err
	}
	if err := domain.ValidatePage(limit, offset); err != nil {
		return nil, err
	}
	var filter string
	switch category {
	case "":
	case domain.ProviderCategoryModel:
		filter = " AND kind <> 'dataforseo'"
	case domain.ProviderCategoryData:
		filter = " AND kind = 'dataforseo'"
	default:
		return nil, &domain.ValidationError{Field: "category", Message: "choose model or data"}
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+providerColumns+` FROM public.seo_providers
		WHERE organization_id = $1`+filter+` ORDER BY created_at DESC, id DESC LIMIT $2 OFFSET $3`, orgID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	providers := []domain.Provider{}
	for rows.Next() {
		p, err := scanProvider(rows)
		if err != nil {
			return nil, err
		}
		providers = append(providers, p)
	}
	return providers, rows.Err()
}

func (s *Store) SaveProvider(ctx context.Context, orgID, id string, input domain.ProviderInput) (domain.Provider, error) {
	if err := validateOrganizationID(orgID); err != nil {
		return domain.Provider{}, err
	}
	creating := id == ""
	if creating {
		id = uuid.NewString()
	} else if !domain.ValidID(id) {
		return domain.Provider{}, domain.ErrNotFound
	}
	input, err := input.Normalize(creating)
	if err != nil {
		return domain.Provider{}, err
	}
	secret := []byte(input.APIKey)
	if input.Kind == domain.ProviderDataForSEO && input.Login != "" {
		secret, err = json.Marshal(dataForSEOCredentials{Login: input.Login, Password: input.Password})
		if err != nil {
			return domain.Provider{}, err
		}
	}
	var sealed []byte
	if len(secret) > 0 {
		if s.providerCipher == nil {
			return domain.Provider{}, domain.ErrProviderNotConfigured
		}
		sealed = s.providerCipher.Seal(nil, nil, secret, providerBinding(input.Kind, orgID, id))
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Provider{}, err
	}
	defer tx.Rollback()
	var options any
	if input.ModelOptions != nil {
		raw, err := json.Marshal(input.ModelOptions)
		if err != nil {
			return domain.Provider{}, err
		}
		options = string(raw)
	}
	settings := []any{orgID, id, input.Name, input.Model, sealed, input.TimeoutSeconds,
		input.RequestsPerMinute, input.RequestsPerDay, input.MaxOutputTokens, input.Enabled, input.Kind, input.DailyCostMicros, options}
	var provider domain.Provider
	var details any
	action := "provider.create"
	if creating {
		provider, err = scanProvider(tx.QueryRowContext(ctx, `INSERT INTO public.seo_providers
			(organization_id, id, name, model, credential, timeout_seconds, requests_per_minute, requests_per_day, max_output_tokens, enabled, kind, daily_cost_micros, model_options)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13::jsonb) RETURNING `+providerColumns, settings...))
		details = provider
	} else {
		before, err := scanProvider(tx.QueryRowContext(ctx, `SELECT `+providerColumns+` FROM public.seo_providers
			WHERE organization_id = $1 AND id = $2 FOR NO KEY UPDATE`, orgID, id))
		if err != nil {
			return domain.Provider{}, err
		}
		if before.Kind != input.Kind {
			return domain.Provider{}, &domain.ValidationError{Field: "kind", Message: "cannot change a provider's type"}
		}
		if before.Version != input.Version {
			return domain.Provider{}, domain.ErrVersionConflict
		}
		if input.Kind == domain.ProviderCompatible && input.ModelOptions != nil && sealed == nil {
			old, now := before.Options(), *input.ModelOptions
			if old.BaseURL != now.BaseURL || old.AuthHeader != now.AuthHeader || old.AuthMode != now.AuthMode {
				return domain.Provider{}, &domain.ValidationError{Field: "api_key", Message: "re-enter the credential for changed routing or authentication"}
			}
		}
		updated, err := scanProvider(tx.QueryRowContext(ctx, `UPDATE public.seo_providers SET name=$3, model=$4,
			credential=COALESCE($5::bytea, credential), timeout_seconds=$6, requests_per_minute=$7, requests_per_day=$8,
			max_output_tokens=$9, enabled=$10, kind=$11, daily_cost_micros=$12, model_options=COALESCE($13::jsonb,model_options),
			version=version+1, updated_at=clock_timestamp()
			WHERE organization_id=$1 AND id=$2 RETURNING `+providerColumns, settings...))
		if err != nil {
			return domain.Provider{}, err
		}
		provider, action = updated, "provider.update"
		details = map[string]any{"before": before, "after": provider, "credential_rotated": sealed != nil}
	}
	if err != nil {
		return domain.Provider{}, err
	}
	if err := audit(ctx, tx, orgID, action, id, details); err != nil {
		return domain.Provider{}, err
	}
	return provider, tx.Commit()
}

type dataForSEOCredentials struct {
	Login    string `json:"login"`
	Password string `json:"password"`
}

func providerBinding(kind, orgID, id string) []byte {
	return []byte("seoos/" + kind + "/" + orgID + "/" + id)
}

// Connection tests are free diagnostic reads with separate request counters.
// Lock the provider while recording the attempt, then release before network I/O.
func (s *Store) ReserveProviderTest(ctx context.Context, orgID, id string) (domain.ProviderTestAccess, error) {
	var access domain.ProviderTestAccess
	if err := validateOrganizationID(orgID); err != nil {
		return access, err
	}
	if !domain.ValidID(id) {
		return access, domain.ErrNotFound
	}
	if s.providerCipher == nil {
		return access, domain.ErrProviderNotConfigured
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return access, err
	}
	defer tx.Rollback()
	var sealed []byte
	p, err := scanProvider(tx.QueryRowContext(ctx, `SELECT `+providerColumns+`, credential FROM public.seo_providers
		WHERE organization_id=$1 AND id=$2 FOR NO KEY UPDATE`, orgID, id), &sealed)
	if err != nil {
		return access, err
	}
	var daily, minute int
	err = tx.QueryRowContext(ctx, `WITH limits AS (
			SELECT date_trunc('day', statement_timestamp(), 'UTC') AS day_start,
				statement_timestamp() - interval '1 minute' AS minute_start
		)
		SELECT count(*) FILTER (WHERE created_at >= day_start), count(*) FILTER (WHERE created_at > minute_start)
		FROM public.seo_audit_events CROSS JOIN limits
		WHERE organization_id=$1 AND resource_id=$2 AND action='provider.test'
			AND created_at >= least(day_start, minute_start)`, orgID, id).Scan(&daily, &minute)
	if err != nil {
		return access, err
	}
	if daily >= p.RequestsPerDay || minute >= p.RequestsPerMinute {
		return access, domain.ErrProviderTestLimited
	}
	secret, err := s.providerCipher.Open(nil, nil, sealed, providerBinding(p.Kind, orgID, id))
	if err != nil || len(secret) == 0 {
		return access, domain.ErrProviderNotConfigured
	}
	access.Provider = p
	if p.Kind == domain.ProviderDataForSEO {
		var credentials dataForSEOCredentials
		if json.Unmarshal(secret, &credentials) != nil || credentials.Login == "" || credentials.Password == "" {
			return domain.ProviderTestAccess{}, domain.ErrProviderNotConfigured
		}
		access.Login, access.Password = credentials.Login, credentials.Password
	} else {
		access.APIKey = string(secret)
	}
	if err := audit(ctx, tx, orgID, "provider.test", id, map[string]int64{"provider_version": p.Version}); err != nil {
		return domain.ProviderTestAccess{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.ProviderTestAccess{}, err
	}
	return access, nil
}

func (s *Store) RecordProviderTest(ctx context.Context, orgID, id string, result domain.ProviderTestResult) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := audit(ctx, tx, orgID, "provider.test_result", id, result); err != nil {
		return err
	}
	return tx.Commit()
}

// ReserveProviderCall serializes spend per provider. Failed and interrupted
// attempts still consume their reservation because the provider may have billed them.
func (s *Store) ReserveProviderCall(ctx context.Context, lease domain.Lease, requests ...domain.ContentModelRequest) (domain.ProviderCall, error) {
	if s.providerCipher == nil {
		return domain.ProviderCall{}, domain.ErrProviderNotConfigured
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.ProviderCall{}, err
	}
	defer tx.Rollback()
	job, err := lockLease(ctx, tx, lease)
	if err != nil {
		return domain.ProviderCall{}, err
	}
	var sealed []byte
	provider, err := scanProvider(tx.QueryRowContext(ctx, `SELECT `+providerColumns+`, credential FROM public.seo_providers
		WHERE organization_id=$1 AND id=$2 FOR NO KEY UPDATE`, job.OrganizationID, job.ProviderID), &sealed)
	if err != nil {
		return domain.ProviderCall{}, err
	}
	if !provider.Enabled || provider.Kind != domain.ProviderOpenAI {
		return domain.ProviderCall{}, domain.ErrProviderDisabled
	}
	var priorState string
	var prior []byte
	err = tx.QueryRowContext(ctx, `SELECT state,outcome FROM public.seo_provider_calls
		WHERE organization_id=$1 AND job_id=$2 ORDER BY reserved_at DESC LIMIT 1`,
		job.OrganizationID, job.ID).Scan(&priorState, &prior)
	switch {
	case err == nil:
		if priorState == "reserved" || priorState == "uncertain" {
			return domain.ProviderCall{}, &domain.DataError{Code: "provider_receipt_unknown"}
		}
		var cached domain.JobOutcome
		if err = json.Unmarshal(prior, &cached); err != nil {
			return domain.ProviderCall{}, err
		}
		if priorState == "succeeded" || !cached.Retry {
			return domain.ProviderCall{Provider: provider, Cached: &cached}, nil
		}
	case !errors.Is(err, sql.ErrNoRows):
		return domain.ProviderCall{}, err
	}
	var now time.Time
	if err := tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return domain.ProviderCall{}, err
	}
	now = now.UTC()
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	minuteStart := now.Add(-time.Minute)
	var daily, minute int
	var oldest sql.NullTime
	err = tx.QueryRowContext(ctx, modelRequests,
		job.OrganizationID, provider.ID, dayStart, minuteStart).Scan(&daily, &minute, &oldest)
	if err != nil {
		return domain.ProviderCall{}, err
	}
	if daily >= provider.RequestsPerDay {
		return domain.ProviderCall{DeferUntil: dayStart.AddDate(0, 0, 1)}, nil
	}
	if minute >= provider.RequestsPerMinute {
		return domain.ProviderCall{DeferUntil: oldest.Time.Add(time.Minute)}, nil
	}
	key, err := s.providerCipher.Open(nil, nil, sealed, providerBinding(provider.Kind, job.OrganizationID, provider.ID))
	if err != nil {
		return domain.ProviderCall{}, domain.ErrProviderNotConfigured
	}
	call := domain.ProviderCall{Provider: provider, APIKey: string(key)}
	var pricing []byte
	err = tx.QueryRowContext(ctx, `SELECT data FROM public.seo_content_model_profiles WHERE organization_id=$1 AND provider_id=$2`,
		job.OrganizationID, provider.ID).Scan(&pricing)
	if err == nil {
		if err = json.Unmarshal(pricing, &call.Pricing); err != nil {
			return domain.ProviderCall{}, err
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return domain.ProviderCall{}, err
	}
	if call.Pricing != nil {
		forecast := int64(512)
		if len(requests) != 0 {
			forecast, err = contentmodel.EstimateInputTokens(requests[0], provider, *call.Pricing)
			if err != nil {
				return domain.ProviderCall{}, &domain.DataError{Code: "content_model_invalid_request", KnownUnbilled: true}
			}
		}
		call.ReservedTokens = forecast + int64(provider.MaxOutputTokens)
		call.ReservedMicros = modelCharge(*call.Pricing, forecast, int64(provider.MaxOutputTokens))
		if forecast > int64(call.Pricing.MaxInputTokens) || call.ReservedTokens > call.Pricing.DailyTokens ||
			call.ReservedMicros > call.Pricing.DailyCostMicros {
			return domain.ProviderCall{}, &domain.DataError{Code: "content_model_budget_too_low", KnownUnbilled: true}
		}
		var usedTokens, usedMicros int64
		if err = tx.QueryRowContext(ctx, modelDailySpend, job.OrganizationID, provider.ID, dayStart).Scan(&usedTokens, &usedMicros); err != nil {
			return domain.ProviderCall{}, err
		}
		if usedTokens > call.Pricing.DailyTokens-call.ReservedTokens || usedMicros > call.Pricing.DailyCostMicros-call.ReservedMicros {
			return domain.ProviderCall{DeferUntil: dayStart.AddDate(0, 0, 1)}, nil
		}
	}
	reservation, err := json.Marshal(domain.ProviderReceipt{Provider: provider.Kind, Operation: provider.Model,
		Category: "reserved", MayBeCharged: true, CostSource: "unknown",
		ReservedTokens: call.ReservedTokens, ReservedMicros: call.ReservedMicros})
	if err != nil {
		return domain.ProviderCall{}, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO public.seo_provider_calls
		(organization_id, provider_id, lease_token, job_id, reserved_at, output_token_limit,state,diagnostic)
		VALUES ($1,$2,$3,$4,$5,$6,'reserved',$7::jsonb)`, job.OrganizationID, provider.ID, lease.Token,
		job.ID, now, provider.MaxOutputTokens, string(reservation))
	if err != nil {
		return domain.ProviderCall{}, err
	}
	if err := audit(ctx, tx, job.OrganizationID, "provider.reserve", provider.ID,
		map[string]any{"job_id": job.ID, "attempt": job.Attempts, "max_output_tokens": provider.MaxOutputTokens}); err != nil {
		return domain.ProviderCall{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.ProviderCall{}, err
	}
	return call, nil
}

func (s *Store) SettleProviderCall(ctx context.Context, lease domain.Lease, call domain.ProviderCall, result domain.JobOutcome) error {
	state := "succeeded"
	receipt := domain.ProviderReceipt{Provider: call.Provider.Kind, Operation: call.Provider.Model, CostSource: "unknown"}
	if result.Diagnostic != nil {
		receipt = *result.Diagnostic
	}
	receipt.ReservedTokens, receipt.ReservedMicros = call.ReservedTokens, call.ReservedMicros
	if result.Code != "" {
		state = "failed"
		if receipt.MayBeCharged && receipt.InputTokens == nil {
			state = "uncertain"
		}
		receipt.Code = result.Code
	}
	if receipt.Category == "" {
		receipt.Category = state
	}
	if result.Diagnostic != nil && !receipt.MayBeCharged {
		zero := int64(0)
		receipt.CostMicros, receipt.CostSource = &zero, "unbilled"
	} else if call.Pricing != nil && receipt.InputTokens != nil && receipt.OutputTokens != nil {
		cost := measuredModelCharge(*call.Pricing, *receipt.InputTokens, *receipt.OutputTokens, receipt.CachedTokens)
		receipt.CostMicros, receipt.CostSource = &cost, "estimated"
	}
	outcome, err := json.Marshal(result)
	if err != nil {
		return err
	}
	metadata, err := json.Marshal(receipt)
	if err != nil {
		return err
	}
	update, err := s.db.ExecContext(ctx, `UPDATE public.seo_provider_calls SET state=$4,outcome=$5::jsonb,diagnostic=$6::jsonb
		WHERE organization_id=$1 AND job_id=$2 AND lease_token=$3 AND state='reserved'`,
		lease.Job.OrganizationID, lease.Job.ID, lease.Token, state, string(outcome), string(metadata))
	if err != nil {
		return err
	}
	if n, err := update.RowsAffected(); err != nil || n != 1 {
		return domain.ErrLeaseLost
	}
	return nil
}
