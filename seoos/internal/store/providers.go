package store

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

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
	requests_per_minute, requests_per_day, max_output_tokens, enabled, version, created_at, updated_at, kind, daily_cost_micros`

func scanProvider(row rowScanner, tail ...any) (domain.Provider, error) {
	var provider domain.Provider
	dest := []any{&provider.ID, &provider.OrganizationID, &provider.Name, &provider.Model, &provider.TimeoutSeconds,
		&provider.RequestsPerMinute, &provider.RequestsPerDay, &provider.MaxOutputTokens, &provider.Enabled,
		&provider.Version, &provider.CreatedAt, &provider.UpdatedAt, &provider.Kind, &provider.DailyCostMicros}
	err := row.Scan(append(dest, tail...)...)
	provider.CreatedAt = provider.CreatedAt.UTC()
	provider.UpdatedAt = provider.UpdatedAt.UTC()
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "seo_providers_organization_id_name_key" {
		return domain.Provider{}, domain.ErrProviderNameTaken
	}
	return provider, classifyError(err)
}

func (s *Store) ListProviders(ctx context.Context, orgID string, limit, offset int) ([]domain.Provider, error) {
	if err := validateOrganizationID(orgID); err != nil {
		return nil, err
	}
	if err := domain.ValidatePage(limit, offset); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+providerColumns+` FROM public.seo_providers
		WHERE organization_id = $1 ORDER BY created_at DESC, id DESC LIMIT $2 OFFSET $3`, orgID, limit, offset)
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
	settings := []any{orgID, id, input.Name, input.Model, sealed, input.TimeoutSeconds,
		input.RequestsPerMinute, input.RequestsPerDay, input.MaxOutputTokens, input.Enabled, input.Kind, input.DailyCostMicros}
	var provider domain.Provider
	var details any
	action := "provider.create"
	if creating {
		provider, err = scanProvider(tx.QueryRowContext(ctx, `INSERT INTO public.seo_providers
			(organization_id, id, name, model, credential, timeout_seconds, requests_per_minute, requests_per_day, max_output_tokens, enabled, kind, daily_cost_micros)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) RETURNING `+providerColumns, settings...))
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
		updated, err := scanProvider(tx.QueryRowContext(ctx, `UPDATE public.seo_providers SET name=$3, model=$4,
			credential=COALESCE($5::bytea, credential), timeout_seconds=$6, requests_per_minute=$7, requests_per_day=$8,
			max_output_tokens=$9, enabled=$10, kind=$11, daily_cost_micros=$12, version=version+1, updated_at=clock_timestamp()
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

// ReserveProviderCall serializes spend per provider. Failed and interrupted
// attempts still consume their reservation because the provider may have billed them.
func (s *Store) ReserveProviderCall(ctx context.Context, lease domain.Lease) (domain.ProviderCall, error) {
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
	var now time.Time
	if err := tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return domain.ProviderCall{}, err
	}
	now = now.UTC()
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	minuteStart := now.Add(-time.Minute)
	var daily, minute int
	var oldest sql.NullTime
	err = tx.QueryRowContext(ctx, `SELECT count(*) FILTER (WHERE reserved_at >= $3),
		count(*) FILTER (WHERE reserved_at > $4), min(reserved_at) FILTER (WHERE reserved_at > $4)
		FROM public.seo_provider_calls
		WHERE organization_id=$1 AND provider_id=$2 AND reserved_at >= least($3::timestamptz, $4::timestamptz)`,
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
	_, err = tx.ExecContext(ctx, `INSERT INTO public.seo_provider_calls
		(organization_id, provider_id, lease_token, job_id, reserved_at, output_token_limit)
		VALUES ($1,$2,$3,$4,$5,$6)`, job.OrganizationID, provider.ID, lease.Token,
		job.ID, now, provider.MaxOutputTokens)
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
	return domain.ProviderCall{Provider: provider, APIKey: string(key)}, nil
}
