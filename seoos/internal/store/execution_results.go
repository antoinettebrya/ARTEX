package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"time"

	"github.com/Autumn-27/artex/seoos/internal/domain"
)

type ExecutionResult struct {
	Info       *domain.CMSSiteInfo
	Pages      []domain.CMSPage
	Media      []domain.CMSMedia
	Sync       *domain.ExecutionSync
	Indexes    []domain.ExecutionIndexRecord
	Sitemap    *domain.SitemapDocument
	Extraction *domain.ExecutionExtraction
	Submission map[string]any
}

func (s *Store) SaveExecutionResult(ctx context.Context, lease domain.Lease, work domain.ExecutionWork, result ExecutionResult) (json.RawMessage, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	job, err := lockLease(ctx, tx, lease)
	if err != nil {
		return nil, err
	}
	in := work.Input
	var c domain.ExecutionConnection
	if in.ConnectionID != "" {
		var active bool
		c, _, active, err = execConnection(ctx, tx, job.OrganizationID, job.SiteID, in.ConnectionID, "FOR SHARE OF c")
		if err != nil {
			return nil, err
		}
		if !active || !c.Enabled || c.Version != work.ConnectionVersion {
			return nil, domain.ErrExecutionAuthority
		}
	}
	if result.Info != nil {
		err = recordExecutionJSON(ctx, tx, `INSERT INTO public.seo_cms_capabilities (organization_id,site_id,connection_id,data)
			VALUES ($1,$2,$3,$4::jsonb) ON CONFLICT (organization_id,site_id,connection_id)
			DO UPDATE SET data=EXCLUDED.data,observed_at=clock_timestamp()`, result.Info, job.OrganizationID, job.SiteID, c.ID)
		if err != nil {
			return nil, err
		}
	}
	for _, page := range result.Pages {
		if err = recordExecutionJSON(ctx, tx, `INSERT INTO public.seo_cms_pages (organization_id,site_id,connection_id,resource,remote_id,data)
			VALUES ($1,$2,$3,$4,$5,$6::jsonb) ON CONFLICT (organization_id,site_id,connection_id,resource,remote_id)
			DO UPDATE SET data=EXCLUDED.data,observed_at=clock_timestamp()`, page, job.OrganizationID, job.SiteID, c.ID, page.Resource, page.ID); err != nil {
			return nil, err
		}
	}
	for _, media := range result.Media {
		if err = recordExecutionJSON(ctx, tx, `INSERT INTO public.seo_cms_media (organization_id,site_id,connection_id,remote_id,data)
			VALUES ($1,$2,$3,$4,$5::jsonb) ON CONFLICT (organization_id,site_id,connection_id,remote_id)
			DO UPDATE SET data=EXCLUDED.data,observed_at=clock_timestamp()`, media, job.OrganizationID, job.SiteID, c.ID, media.ID); err != nil {
			return nil, err
		}
	}
	if result.Sync != nil {
		if err = writeExecutionSync(ctx, tx, c, *result.Sync); err != nil {
			return nil, err
		}
		result.Indexes = append(result.Indexes, result.Sync.Indexes...)
	}
	for _, record := range result.Indexes {
		if err = writeIndex(ctx, tx, job, record); err != nil {
			return nil, err
		}
	}
	if result.Submission != nil {
		if err = recordExecutionJSON(ctx, tx, `INSERT INTO public.seo_indexing_submissions (organization_id,site_id,job_id,provider,data)
			VALUES ($1,$2,$3,$4,$5::jsonb) ON CONFLICT (organization_id,job_id) DO UPDATE SET data=EXCLUDED.data`,
			result.Submission, job.OrganizationID, job.SiteID, job.ID, c.Kind); err != nil {
			return nil, err
		}
	}
	if result.Sitemap != nil {
		doc := result.Sitemap
		if err = recordExecutionJSON(ctx, tx, `INSERT INTO public.seo_sitemap_jobs (organization_id,site_id,job_id,data)
			VALUES ($1,$2,$3,$4::jsonb) ON CONFLICT DO NOTHING`, doc, job.OrganizationID, job.SiteID, job.ID); err != nil {
			return nil, err
		}
		status := "observed"
		if doc.Truncated {
			status = "truncated"
		}
		if err = writeSitemap(ctx, tx, job.OrganizationID, job.SiteID, domain.ExecutionSitemap{
			URL: doc.URL, JobID: job.ID, Provider: "site", Status: status, Entries: len(doc.Entries), Parts: len(doc.Files),
		}); err != nil {
			return nil, err
		}
		for _, row := range doc.Entries {
			if err = writeIndex(ctx, tx, job, domain.ExecutionIndexRecord{URL: row.URL, Provider: "sitemap", Status: domain.IndexDiscovered,
				Evidence: doc.URL, ObservedAt: time.Now().UTC()}); err != nil {
				return nil, err
			}
		}
	}
	if result.Extraction != nil {
		if err = recordExecutionJSON(ctx, tx, `INSERT INTO public.seo_page_extraction_jobs (organization_id,site_id,job_id,data)
			VALUES ($1,$2,$3,$4::jsonb) ON CONFLICT DO NOTHING`, result.Extraction, job.OrganizationID, job.SiteID, job.ID); err != nil {
			return nil, err
		}
	}
	summary := map[string]any{"kind": in.Kind, "site_id": job.SiteID, "pages": len(result.Pages), "media": len(result.Media), "indexes": len(result.Indexes)}
	if result.Sync != nil {
		summary["rows"] = len(result.Sync.Metrics)
		summary["truncated"] = result.Sync.Truncated
	}
	if result.Sitemap != nil {
		summary["sitemap_urls"] = len(result.Sitemap.Entries)
		summary["truncated"] = result.Sitemap.Truncated
	}
	raw, err := saveExecutionReceipt(ctx, tx, job, summary)
	if err != nil {
		return nil, err
	}
	return raw, tx.Commit()
}

func recordExecutionJSON(ctx context.Context, tx *sql.Tx, sql string, value any, args ...any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, sql, append(args, string(raw))...)
	return err
}

func writeSitemap(ctx context.Context, tx *sql.Tx, org, site string, info domain.ExecutionSitemap) error {
	hash := sha256.Sum256([]byte(info.URL))
	return recordExecutionJSON(ctx, tx, `INSERT INTO public.seo_execution_sitemaps (organization_id,site_id,provider,url_hash,data)
		VALUES ($1,$2,$3,$4,$5::jsonb) ON CONFLICT (organization_id,site_id,provider,url_hash)
		DO UPDATE SET data=EXCLUDED.data,observed_at=clock_timestamp()`, info, org, site, info.Provider, hash[:])
}

func writeIndex(ctx context.Context, tx *sql.Tx, job domain.Job, out domain.ExecutionIndexRecord) error {
	switch out.Status {
	case domain.IndexDiscovered, domain.IndexSubmitted, domain.IndexCrawled, domain.IndexIndexed, domain.IndexNotIndexed, domain.IndexUnknown:
	default:
		return domain.ErrExecutionState
	}
	if _, err := domain.EvidenceURL(out.URL); err != nil {
		return err
	}
	if out.ObservedAt.IsZero() {
		out.ObservedAt = time.Now().UTC()
	}
	hash := sha256.Sum256([]byte(out.URL))
	return recordExecutionJSON(ctx, tx, `INSERT INTO public.seo_indexing_records
		(organization_id,site_id,url_hash,provider,observed_at,data) VALUES ($1,$2,$3,$4,$5,$6::jsonb)
		ON CONFLICT (organization_id,site_id,url_hash,provider)
		DO UPDATE SET data=EXCLUDED.data,observed_at=EXCLUDED.observed_at
		WHERE public.seo_indexing_records.observed_at<=EXCLUDED.observed_at`,
		out, job.OrganizationID, job.SiteID, hash[:], out.Provider, out.ObservedAt)
}

func writeExecutionSync(ctx context.Context, tx *sql.Tx, c domain.ExecutionConnection, report domain.ExecutionSync) error {
	if !executionAnalytics(c.Kind) {
		return domain.ErrExecutionUnsupported
	}
	for _, property := range report.Properties {
		if err := recordExecutionJSON(ctx, tx, `INSERT INTO public.seo_`+c.Kind+`_properties (organization_id,site_id,connection_id,property_id,data)
			VALUES ($1,$2,$3,$4,$5::jsonb) ON CONFLICT (organization_id,site_id,connection_id,property_id)
			DO UPDATE SET data=EXCLUDED.data,observed_at=clock_timestamp()`, property, c.OrganizationID, c.SiteID, c.ID, property.ID); err != nil {
			return err
		}
	}
	for _, info := range report.Sitemaps {
		if info.Provider == "" {
			info.Provider = c.Kind
		}
		if err := writeSitemap(ctx, tx, c.OrganizationID, c.SiteID, info); err != nil {
			return err
		}
	}
	type storedMetric struct {
		Day  string                 `json:"day"`
		Hash string                 `json:"hash"`
		Data domain.ExecutionMetric `json:"data"`
	}
	rows := make([]storedMetric, 0, len(report.Metrics))
	for _, entry := range report.Metrics {
		if _, err := time.Parse(time.DateOnly, entry.Date); err != nil {
			continue
		}
		if entry.Page != "" {
			if u, err := resolveMetricPage(c.BaseURL, entry.Page); err != nil || !domain.BelongsToSite(c.BaseURL, u) {
				continue
			} else {
				entry.Page = u.String()
			}
		}
		parts, err := json.Marshal([]string{entry.Date, entry.Page, entry.Query, entry.Country, entry.Device, entry.Language})
		if err != nil {
			return err
		}
		hash := sha256.Sum256(parts)
		rows = append(rows, storedMetric{entry.Date, hex.EncodeToString(hash[:]), entry})
	}
	for start := 0; start < len(rows); start += 500 {
		err := recordExecutionJSON(ctx, tx, `INSERT INTO public.seo_`+c.Kind+`_daily_metrics
			(organization_id,site_id,connection_id,day,dimension_hash,data)
			SELECT DISTINCT ON (m.day,m.hash) $1,$2,$3,m.day,decode(m.hash,'hex'),m.data
			FROM jsonb_to_recordset($4::jsonb) AS m(day date,hash text,data jsonb)
			ORDER BY m.day,m.hash
			ON CONFLICT (organization_id,site_id,connection_id,day,dimension_hash)
			DO UPDATE SET data=EXCLUDED.data,observed_at=clock_timestamp()`,
			rows[start:min(start+500, len(rows))], c.OrganizationID, c.SiteID, c.ID)
		if err != nil {
			return err
		}
	}
	if len(rows) > 0 && !report.Truncated && (c.Kind == domain.ExecutionGSC || c.Kind == domain.ExecutionGA4) {
		return updateExecutionFeedback(ctx, tx, c)
	}
	return nil
}

func resolveMetricPage(base, page string) (*url.URL, error) {
	u, err := url.Parse(page)
	if err != nil {
		return nil, err
	}
	if u.IsAbs() {
		return domain.EvidenceURL(u.String())
	}
	b, err := url.Parse(base)
	if err != nil {
		return nil, err
	}
	return domain.EvidenceURL(b.ResolveReference(u).String())
}

func (s *Store) ExecutionSyncDates(ctx context.Context, c domain.ExecutionConnection, in domain.ExecutionJobInput) (string, string, error) {
	if !executionAnalytics(c.Kind) {
		return "", "", domain.ErrExecutionUnsupported
	}
	from, to, err := domain.ExecutionDateRange(in.From, in.To)
	if err != nil {
		return "", "", err
	}
	if to.IsZero() {
		to = time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, -2)
	}
	if from.IsZero() {
		var latest sql.NullTime
		err = s.db.QueryRowContext(ctx, `SELECT max(day) FROM public.seo_`+c.Kind+`_daily_metrics
			WHERE organization_id=$1 AND site_id=$2 AND connection_id=$3`, c.OrganizationID, c.SiteID, c.ID).Scan(&latest)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return "", "", err
		}
		from = to.AddDate(0, 0, -13)
		if latest.Valid && latest.Time.Before(to) && to.Sub(latest.Time) < 28*24*time.Hour {
			from = latest.Time.AddDate(0, 0, -2)
		}
	}
	if to.Sub(from) >= 31*24*time.Hour && in.To == "" {
		to = from.AddDate(0, 0, 30)
	}
	_, _, err = domain.ExecutionDateRange(from.Format(time.DateOnly), to.Format(time.DateOnly))
	return from.Format(time.DateOnly), to.Format(time.DateOnly), err
}

func (s *Store) BeginExecutionSubmission(ctx context.Context, lease domain.Lease, provider, url string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	job, err := lockLease(ctx, tx, lease)
	if err != nil {
		return err
	}
	if job.Kind != domain.ExecIndexNow || provider != domain.ExecutionIndexNow && provider != domain.ExecutionBing {
		return domain.ErrExecutionUnsupported
	}
	lock := "indexing:" + job.OrganizationID + ":" + job.SiteID + ":" + provider + ":" + url
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, lock); err != nil {
		return err
	}
	var prior string
	err = tx.QueryRowContext(ctx, `SELECT data->>'status' FROM public.seo_indexing_submissions
		WHERE organization_id=$1 AND site_id=$2 AND provider=$3 AND data->>'url'=$4 AND
			((job_id=$5 AND data->>'status' IN ('dispatching','unknown')) OR
			(created_at >= clock_timestamp()-interval '24 hours' AND data->>'status' IN
				('dispatching','unknown','accepted','pending_key_validation')))
		ORDER BY created_at DESC LIMIT 1`, job.OrganizationID, job.SiteID, provider, url, job.ID).Scan(&prior)
	if err == nil {
		code := "execution_indexing_recently_submitted"
		if prior == "dispatching" || prior == "unknown" {
			code = "execution_indexing_receipt_unknown"
		}
		return &domain.DataError{Code: code}
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err = recordExecutionJSON(ctx, tx, `INSERT INTO public.seo_indexing_submissions (organization_id,site_id,job_id,provider,data)
		VALUES ($1,$2,$3,$4,$5::jsonb) ON CONFLICT (organization_id,job_id) DO UPDATE SET data=EXCLUDED.data`,
		map[string]any{"url": url, "status": "dispatching"}, job.OrganizationID, job.SiteID, job.ID, provider); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) ExecutionSubmissionFailure(ctx context.Context, lease domain.Lease, provider, url string, status int, code string, unknown bool) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	job, err := lockLease(ctx, tx, lease)
	if err != nil {
		return err
	}
	state := "failed"
	if unknown {
		state = "unknown"
	}
	if err = recordExecutionJSON(ctx, tx, `INSERT INTO public.seo_indexing_submissions (organization_id,site_id,job_id,provider,data)
		VALUES ($1,$2,$3,$4,$5::jsonb) ON CONFLICT (organization_id,job_id) DO UPDATE SET data=EXCLUDED.data`,
		map[string]any{"url": url, "status": state, "http_status": status, "error": code}, job.OrganizationID, job.SiteID, job.ID, provider); err != nil {
		return err
	}
	if err = audit(ctx, tx, job.OrganizationID, "execution.indexing.submit_fail", job.ID, map[string]string{"provider": provider, "error": code}); err != nil {
		return err
	}
	return tx.Commit()
}
