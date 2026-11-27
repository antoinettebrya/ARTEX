// Package store persists the SEO service's data independently of the legacy app.
package store

import (
	"context"
	"crypto/cipher"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Autumn-27/artex/seoos/internal/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/stdlib"
)

// Store owns a PostgreSQL connection pool. Every site operation requires an
// organization ID supplied by the authenticated service, never by a request body.
type Store struct {
	db                  *sql.DB
	providerCipher      cipher.AEAD
	integrationMatchKey [32]byte
}

// Open validates the connection configuration and verifies connectivity. It does
// not modify the schema; callers must explicitly invoke Migrate before serving.
func Open(ctx context.Context, dsn string) (*Store, error) {
	if strings.TrimSpace(dsn) == "" {
		return nil, errors.New("PostgreSQL connection configuration is required")
	}
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		// A parser error can quote the input DSN, which may contain credentials.
		return nil, errors.New("invalid PostgreSQL connection configuration")
	}
	db := stdlib.OpenDB(*cfg)
	db.SetMaxOpenConns(16)
	db.SetMaxIdleConns(4)
	db.SetConnMaxLifetime(30 * time.Minute)
	db.SetConnMaxIdleTime(5 * time.Minute)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("connect to SEO database: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error                   { return s.db.Close() }
func (s *Store) Ping(ctx context.Context) error { return s.db.PingContext(ctx) }

// EnsureOrganization creates a bootstrapped organization if it does not exist.
// Restarting with a different display name does not overwrite existing data.
func (s *Store) EnsureOrganization(ctx context.Context, id, name string) error {
	if err := validateOrganizationID(id); err != nil {
		return err
	}
	name, err := domain.NormalizeOrganizationName(name)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `
		INSERT INTO public.seo_organizations (id, name) VALUES ($1, $2)
		ON CONFLICT (id) DO NOTHING`, id, name)
	if err != nil {
		return err
	}
	if changed, err := result.RowsAffected(); err != nil {
		return err
	} else if changed == 1 {
		if err := audit(ctx, tx, id, "organization.create", id, map[string]string{"name": name}); err != nil {
			return err
		}
	}
	return tx.Commit()
}

const siteColumns = `id::text, organization_id::text, name, base_url, cms,
	country_code, language_tag, status, version, created_at, updated_at`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanSite(row rowScanner) (domain.Site, error) {
	var site domain.Site
	err := row.Scan(&site.ID, &site.OrganizationID, &site.Name, &site.BaseURL,
		&site.CMS, &site.CountryCode, &site.LanguageTag, &site.Status,
		&site.Version, &site.CreatedAt, &site.UpdatedAt)
	if err != nil {
		return domain.Site{}, classifyError(err)
	}
	site.CreatedAt = site.CreatedAt.UTC()
	site.UpdatedAt = site.UpdatedAt.UTC()
	return site, nil
}

func validateOrganizationID(id string) error {
	if !domain.ValidID(id) {
		return &domain.ValidationError{Field: "organization_id", Message: "must be a canonical UUID"}
	}
	return nil
}

func classifyError(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		if pgErr.Code == "23505" && (pgErr.ConstraintName == "seo_sites_organization_url_unique" ||
			pgErr.ConstraintName == "seo_integration_resource_once" || pgErr.ConstraintName == "seo_integration_site_provider_once" ||
			pgErr.ConstraintName == "seo_indexnow_one_active_host") {
			return domain.ErrConflict
		}
		if pgErr.Code == "23503" && pgErr.ConstraintName == "seo_sites_organization_fk" {
			return domain.ErrNotFound
		}
	}
	return err
}

func (s *Store) CreateSite(ctx context.Context, orgID string, input domain.SiteInput) (domain.Site, error) {
	if err := validateOrganizationID(orgID); err != nil {
		return domain.Site{}, err
	}
	input, err := domain.NormalizeSiteInput(input)
	if err != nil {
		return domain.Site{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Site{}, err
	}
	defer tx.Rollback()
	site, err := scanSite(tx.QueryRowContext(ctx, `
		INSERT INTO public.seo_sites
			(id, organization_id, name, base_url, cms, country_code, language_tag, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING `+siteColumns, uuid.NewString(), orgID, input.Name, input.BaseURL,
		input.CMS, input.CountryCode, input.LanguageTag, input.Status))
	if err != nil {
		return domain.Site{}, err
	}
	if err := audit(ctx, tx, orgID, "site.create", site.ID, site); err != nil {
		return domain.Site{}, err
	}
	return site, tx.Commit()
}

func (s *Store) GetSite(ctx context.Context, orgID, siteID string) (domain.Site, error) {
	if err := validateOrganizationID(orgID); err != nil {
		return domain.Site{}, err
	}
	if !domain.ValidID(siteID) {
		return domain.Site{}, domain.ErrNotFound
	}
	return scanSite(s.db.QueryRowContext(ctx, `SELECT `+siteColumns+`
		FROM public.seo_sites WHERE organization_id = $1 AND id = $2`, orgID, siteID))
}

func (s *Store) ListSites(ctx context.Context, orgID string, limit, offset int) ([]domain.Site, error) {
	if err := validateOrganizationID(orgID); err != nil {
		return nil, err
	}
	if err := domain.ValidatePage(limit, offset); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+siteColumns+`
		FROM public.seo_sites WHERE organization_id = $1
		ORDER BY created_at DESC, id DESC LIMIT $2 OFFSET $3`, orgID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	sites := make([]domain.Site, 0)
	for rows.Next() {
		site, err := scanSite(rows)
		if err != nil {
			return nil, err
		}
		sites = append(sites, site)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return sites, nil
}

// UpdateSite locks the tenant-scoped record to capture an accurate audit snapshot
// and checks the expected version before replacing editable fields.
func (s *Store) UpdateSite(ctx context.Context, orgID, siteID string, expectedVersion int64, input domain.SiteInput) (domain.Site, error) {
	if err := validateOrganizationID(orgID); err != nil {
		return domain.Site{}, err
	}
	if !domain.ValidID(siteID) {
		return domain.Site{}, domain.ErrNotFound
	}
	if expectedVersion < 1 {
		return domain.Site{}, &domain.ValidationError{Field: "version", Message: "must be a positive integer"}
	}
	input, err := domain.NormalizeSiteInput(input)
	if err != nil {
		return domain.Site{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Site{}, err
	}
	defer tx.Rollback()
	before, err := scanSite(tx.QueryRowContext(ctx, `SELECT `+siteColumns+`
		FROM public.seo_sites WHERE organization_id = $1 AND id = $2 FOR NO KEY UPDATE`, orgID, siteID))
	if err != nil {
		return domain.Site{}, err
	}
	if before.Version != expectedVersion {
		return domain.Site{}, domain.ErrVersionConflict
	}
	site, err := scanSite(tx.QueryRowContext(ctx, `
		UPDATE public.seo_sites
		SET name = $3, base_url = $4, cms = $5, country_code = $6,
			language_tag = $7, status = $8, version = version + 1,
			updated_at = clock_timestamp()
		WHERE organization_id = $1 AND id = $2
		RETURNING `+siteColumns, orgID, siteID, input.Name, input.BaseURL,
		input.CMS, input.CountryCode, input.LanguageTag, input.Status))
	if err != nil {
		return domain.Site{}, err
	}
	if err := audit(ctx, tx, orgID, "site.update", siteID, map[string]domain.Site{"before": before, "after": site}); err != nil {
		return domain.Site{}, err
	}
	return site, tx.Commit()
}
