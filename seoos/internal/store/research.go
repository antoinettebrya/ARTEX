package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"github.com/Autumn-27/artex/seoos/internal/domain"
	"github.com/google/uuid"
)

func (s *Store) EnqueueResearch(ctx context.Context, orgID string, input domain.ResearchInput) (domain.Job, bool, error) {
	in, err := input.Normalize()
	if err != nil {
		return domain.Job{}, false, err
	}
	provider := in.ProviderID
	if in.IntentProviderID != "" {
		provider = in.IntentProviderID
	}
	return s.enqueueJob(ctx, orgID, domain.JobInput{
		SiteID: in.SiteID, ProviderID: provider, Kind: in.Kind, IdempotencyKey: in.IdempotencyKey,
	}, &in)
}

func createResearchJob(ctx context.Context, tx *sql.Tx, job domain.Job, in domain.ResearchInput, request []byte) error {
	marketID, err := ensureMarket(ctx, tx, job.OrganizationID, in.Market)
	if err != nil {
		return err
	}
	keywordID, err := ensureKeyword(ctx, tx, job.OrganizationID, in.Keyword)
	if err != nil {
		return err
	}
	if err := associateKeyword(ctx, tx, job, marketID, keywordID); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO public.seo_research_jobs (organization_id, job_id, market_id, keyword_id, request)
		VALUES ($1,$2,$3,$4,$5::jsonb)`, job.OrganizationID, job.ID, marketID, keywordID, string(request))
	if err != nil {
		return err
	}
	if in.Kind == domain.KeywordDiscovery || in.Kind == domain.KeywordSuggestions {
		_, err = tx.ExecContext(ctx, `INSERT INTO public.seo_keyword_seeds (organization_id, site_id, market_id, keyword_id)
			VALUES ($1,$2,$3,$4) ON CONFLICT (organization_id, site_id, market_id, keyword_id)
			DO UPDATE SET updated_at = clock_timestamp()`, job.OrganizationID, job.SiteID, marketID, keywordID)
	}
	return err
}

const marketIdentity = `organization_id = $1 AND country_code = $2 AND location_code = $3
	AND language_code = $4 AND search_engine = $5 AND device = $6 AND provider = 'dataforseo'`

func ensureMarket(ctx context.Context, tx *sql.Tx, orgID string, m domain.Market) (string, error) {
	var id string
	err := tx.QueryRowContext(ctx, `INSERT INTO public.seo_research_markets
		(organization_id, country_code, location_code, language_code, search_engine, device, id)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
		ON CONFLICT (organization_id, country_code, location_code, language_code, search_engine, device, provider)
		DO NOTHING RETURNING id::text`, orgID, m.CountryCode, m.LocationCode, m.LanguageCode, m.SearchEngine, m.Device, uuid.NewString()).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		err = tx.QueryRowContext(ctx, `SELECT id::text FROM public.seo_research_markets WHERE `+marketIdentity,
			orgID, m.CountryCode, m.LocationCode, m.LanguageCode, m.SearchEngine, m.Device).Scan(&id)
	}
	return id, err
}

func ensureKeyword(ctx context.Context, tx *sql.Tx, orgID, term string) (string, error) {
	var id string
	key := domain.KeywordKey(term)
	err := tx.QueryRowContext(ctx, `INSERT INTO public.seo_keywords (organization_id, normalized, keyword, id)
		VALUES ($1,$2,$3,$4) ON CONFLICT (organization_id, normalized) DO NOTHING RETURNING id::text`,
		orgID, key, term, uuid.NewString()).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		err = tx.QueryRowContext(ctx, `SELECT id::text FROM public.seo_keywords WHERE organization_id=$1 AND normalized=$2`, orgID, key).Scan(&id)
	}
	return id, err
}

func associateKeyword(ctx context.Context, tx *sql.Tx, job domain.Job, marketID, keywordID string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO public.seo_research_site_keywords (organization_id, site_id, market_id, keyword_id)
		VALUES ($1,$2,$3,$4) ON CONFLICT DO NOTHING`, job.OrganizationID, job.SiteID, marketID, keywordID)
	return err
}

func (s *Store) ResearchContext(ctx context.Context, lease domain.Lease) (domain.ResearchContext, error) {
	var result domain.ResearchContext
	var request []byte
	err := s.db.QueryRowContext(ctx, `SELECT r.market_id::text, r.keyword_id::text, r.request
		FROM public.seo_research_jobs r JOIN public.seo_jobs j ON j.organization_id=r.organization_id AND j.id=r.job_id
		WHERE j.organization_id=$1 AND j.id=$2 AND j.lease_token=$3 AND j.state='running' AND j.lease_until > clock_timestamp()`,
		lease.Job.OrganizationID, lease.Job.ID, lease.Token).Scan(&result.MarketID, &result.KeywordID, &request)
	if errors.Is(err, sql.ErrNoRows) {
		return result, domain.ErrLeaseLost
	}
	if err != nil {
		return result, err
	}
	err = json.Unmarshal(request, &result.Request)
	return result, err
}

func (s *Store) ResearchJobs(ctx context.Context, org string, q domain.ResearchQuery) ([]domain.ResearchJob, error) {
	if _, err := s.validateResearchQuery(ctx, org, q); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+jobColumns+`,
		(SELECT r.request FROM public.seo_research_jobs r WHERE r.organization_id=j.organization_id AND r.job_id=j.id)
		FROM public.seo_jobs j WHERE organization_id=$1 AND site_id=$2
		AND EXISTS (SELECT 1 FROM public.seo_research_jobs r WHERE r.organization_id=j.organization_id AND r.job_id=j.id)
		ORDER BY created_at DESC, id DESC LIMIT $3 OFFSET $4`, org, q.SiteID, q.Limit, q.Offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	jobs := []domain.ResearchJob{}
	for rows.Next() {
		var request []byte
		job, err := scanJob(rows, &request)
		if err != nil {
			return nil, err
		}
		var in domain.ResearchInput
		if err := json.Unmarshal(request, &in); err != nil {
			return nil, err
		}
		jobs = append(jobs, domain.ResearchJob{Job: job, Request: in})
	}
	return jobs, rows.Err()
}

func (s *Store) validateResearchQuery(ctx context.Context, org string, q domain.ResearchQuery) (string, error) {
	if err := validateOrganizationID(org); err != nil {
		return "", err
	}
	if err := domain.ValidatePage(q.Limit, q.Offset); err != nil {
		return "", err
	}
	if q.SiteID != "" {
		if _, err := s.GetSite(ctx, org, q.SiteID); err != nil {
			return "", err
		}
	}
	if q.Market == nil {
		return "", nil
	}
	m, err := q.Market.Normalize()
	if err != nil {
		return "", err
	}
	var market string
	err = s.db.QueryRowContext(ctx, `SELECT id::text FROM public.seo_research_markets WHERE `+marketIdentity,
		org, m.CountryCode, m.LocationCode, m.LanguageCode, m.SearchEngine, m.Device).Scan(&market)
	return market, classifyError(err)
}

func (s *Store) SaveSitePage(ctx context.Context, org string, input domain.SitePageInput) (string, error) {
	if err := validateOrganizationID(org); err != nil {
		return "", err
	}
	if !domain.ValidID(input.SiteID) {
		return "", domain.ErrNotFound
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	site, err := scanSite(tx.QueryRowContext(ctx, `SELECT `+siteColumns+`
		FROM public.seo_sites WHERE organization_id=$1 AND id=$2 FOR SHARE`, org, input.SiteID))
	if err != nil {
		return "", err
	}
	url, err := domain.EvidenceURL(input.Page.URL)
	if err != nil || !domain.BelongsToSite(site.BaseURL, url) {
		return "", &domain.ValidationError{Field: "url", Message: "page must belong to this site's registered origin and path"}
	}
	m, err := input.Market.Normalize()
	if err != nil {
		return "", err
	}
	page := input.Page
	page.Source = "site_inventory_import"
	page.LastModified = nil
	page.SourceSnapshots = nil
	page.MicrodataObserved = false
	page.Sections = nil
	if page.CanonicalURL != "" {
		if _, err := domain.EvidenceURL(page.CanonicalURL); err != nil {
			return "", err
		}
	}
	page.Title = strings.TrimSpace(page.Title)
	if len(page.Title) > 500 || len(page.Description) > 2000 || len(page.PageType) > 50 ||
		len(page.Headings) > 100 || len(page.Topics) > 100 ||
		len(page.Entities) > 100 || len(page.FAQ) > 100 || len(page.Schema) > 100 {
		return "", &domain.ValidationError{Field: "page", Message: "page metadata exceeds the allowed size"}
	}
	for _, heading := range page.Headings {
		if heading.Level < 1 || heading.Level > 6 || len(heading.Text) > 500 {
			return "", &domain.ValidationError{Field: "headings", Message: "use H1 to H6 headings of at most 500 bytes"}
		}
	}
	data, err := json.Marshal(page)
	if err != nil {
		return "", err
	}
	if strings.Contains(string(data), `\u0000`) {
		return "", &domain.ValidationError{Field: "page", Message: "page metadata contains an unsupported character"}
	}
	digest := sha256.Sum256([]byte(page.URL))
	market, err := ensureMarket(ctx, tx, org, m)
	if err != nil {
		return "", err
	}
	var id string
	err = tx.QueryRowContext(ctx, `INSERT INTO public.seo_research_site_pages
		(id, organization_id, site_id, market_id, url_digest, page, source, collected_at)
		VALUES ($1,$2,$3,$4,$5,$6::jsonb,$7,clock_timestamp())
		ON CONFLICT (organization_id, site_id, market_id, url_digest)
		DO UPDATE SET page=EXCLUDED.page, collected_at=EXCLUDED.collected_at, updated_at=clock_timestamp()
		RETURNING id::text`, uuid.NewString(), org, site.ID, market, digest[:], string(data), page.Source).Scan(&id)
	if err != nil {
		return "", err
	}
	if err := audit(ctx, tx, org, "research.site_page.save", id, map[string]any{"site_id": site.ID, "market_id": market}); err != nil {
		return "", err
	}
	return id, tx.Commit()
}
