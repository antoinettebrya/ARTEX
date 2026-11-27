package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/Autumn-27/artex/seoos/internal/contentinventory"
	"github.com/Autumn-27/artex/seoos/internal/domain"
	"github.com/google/uuid"
)

func (s *Store) CrawlPolicy(ctx context.Context, org, site string) (*domain.CrawlPolicy, error) {
	if _, err := s.GetSite(ctx, org, site); err != nil {
		return nil, err
	}
	var raw []byte
	err := s.db.QueryRowContext(ctx, `SELECT p.policy FROM public.seo_site_crawl_policies p JOIN public.seo_sites s
		ON s.organization_id=p.organization_id AND s.id=p.site_id AND s.base_url=p.configured_base_url
		WHERE p.organization_id=$1 AND p.site_id=$2`, org, site).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	var policy domain.CrawlPolicy
	if err != nil {
		return nil, err
	}
	return &policy, json.Unmarshal(raw, &policy)
}

func (s *Store) SaveCrawlPolicy(ctx context.Context, site domain.Site, p domain.CrawlPolicy) (domain.CrawlPolicy, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return p, err
	}
	defer tx.Rollback()
	var current string
	if err := tx.QueryRowContext(ctx, `SELECT base_url FROM public.seo_sites WHERE organization_id=$1 AND id=$2 FOR SHARE`,
		site.OrganizationID, site.ID).Scan(&current); err != nil {
		return p, classifyError(err)
	}
	if current != site.BaseURL || p.SiteID != site.ID {
		return p, domain.ErrVersionConflict
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return p, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO public.seo_site_crawl_policies (organization_id,site_id,configured_base_url,policy)
		VALUES ($1,$2,$3,$4::jsonb) ON CONFLICT (organization_id,site_id) DO UPDATE SET configured_base_url=EXCLUDED.configured_base_url,
		policy=EXCLUDED.policy,updated_at=clock_timestamp()`, site.OrganizationID, site.ID, site.BaseURL, string(raw)); err != nil {
		return p, err
	}
	if err = audit(ctx, tx, site.OrganizationID, "content.crawl.policy", site.ID, p); err != nil {
		return p, err
	}
	return p, tx.Commit()
}

func (s *Store) ContentInventory(ctx context.Context, org, site string, limit, offset int) ([]domain.SitePage, error) {
	if _, err := s.GetSite(ctx, org, site); err != nil {
		return nil, err
	}
	if err := domain.ValidatePage(limit, offset); err != nil {
		return nil, err
	}
	return inventoryRows(ctx, s.db, org, site, false, limit, offset)
}

const inventoryMapped = `EXISTS(SELECT 1 FROM public.seo_keyword_url_mappings m
	WHERE m.organization_id=i.organization_id AND m.site_id=i.site_id AND m.mapping->>'target_url'=i.url)`
const inventorySelect = `SELECT i.data || jsonb_build_object('id',i.id,'site_id',i.site_id,'snapshot_id',i.current_snapshot_id,
	'discovered_at',i.discovered_at,'checked_at',i.checked_at,'mapped',` + inventoryMapped + `) `
const inventoryFrom = ` FROM public.seo_site_inventory i WHERE i.organization_id=$1 AND i.site_id=$2 `

func inventoryRows(ctx context.Context, db queryer, org, site string, body bool, limit, offset int) ([]domain.SitePage, error) {
	selectSQL := inventorySelect
	if !body {
		selectSQL = "SELECT (" + inventorySelect[len("SELECT "):] + ") - 'body' "
	}
	values, err := jsonRows(ctx, db, selectSQL+inventoryFrom+`ORDER BY checked_at DESC NULLS LAST,i.id LIMIT $3 OFFSET $4`,
		org, site, limit, offset)
	return contentDecodeList[domain.SitePage](values, err)
}

func (s *Store) InventoryPage(ctx context.Context, org, site, id string) (domain.SitePage, error) {
	if !domain.ValidID(id) || !domain.ValidID(site) {
		return domain.SitePage{}, domain.ErrNotFound
	}
	return inventoryPage(ctx, s.db, org, site, `AND i.id=$3`, id)
}

func (s *Store) InventoryPrior(ctx context.Context, org, site, url string) (*domain.SitePage, error) {
	digest := sha256.Sum256([]byte(url))
	page, err := inventoryPage(ctx, s.db, org, site, `AND i.url_digest=$3`, digest[:])
	if errors.Is(err, domain.ErrNotFound) {
		return nil, nil
	}
	return &page, err
}

type rowQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func inventoryPage(ctx context.Context, db rowQueryer, org, site, predicate string, key any) (domain.SitePage, error) {
	var page domain.SitePage
	var raw []byte
	if err := db.QueryRowContext(ctx, inventorySelect+inventoryFrom+predicate, org, site, key).Scan(&raw); err != nil {
		return page, classifyError(err)
	}
	return page, json.Unmarshal(raw, &page)
}

func activeCrawlPolicy(ctx context.Context, tx *sql.Tx, org, site string, policy domain.CrawlPolicy) error {
	encoded, err := json.Marshal(policy)
	if err != nil {
		return err
	}
	var allowed bool
	err = tx.QueryRowContext(ctx, `SELECT p.policy=$3::jsonb AND s.base_url=p.configured_base_url
		FROM public.seo_site_crawl_policies p JOIN public.seo_sites s ON s.organization_id=p.organization_id AND s.id=p.site_id
		WHERE s.status='active' AND p.organization_id=$1 AND p.site_id=$2 FOR SHARE OF p,s`, org, site, string(encoded)).Scan(&allowed)
	if errors.Is(err, sql.ErrNoRows) || err == nil && (!allowed || !policy.Enabled) {
		return domain.ErrCrawlUnauthorized
	}
	return err
}

func (s *Store) SaveInventoryBatch(ctx context.Context, lease domain.Lease, policy domain.CrawlPolicy, batch domain.InventoryBatch) (int, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	job, err := lockLease(ctx, tx, lease)
	if err != nil {
		return 0, err
	}
	if err := activeCrawlPolicy(ctx, tx, job.OrganizationID, job.SiteID, policy); err != nil {
		return 0, err
	}
	site, _, err := inventoryJobTarget(ctx, tx, job, domain.ContentInventory)
	if err != nil {
		return 0, err
	}
	for _, item := range batch.Entries {
		if _, err := contentinventory.ValidatePageURL(site, policy, item.URL); err != nil {
			return 0, err
		}
		if err := queueDiscoveredPage(ctx, tx, job, item); err != nil {
			return 0, err
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO public.seo_site_inventory_runs
		(organization_id,site_id,job_id,discovered_count,truncated) VALUES ($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`,
		job.OrganizationID, job.SiteID, job.ID, len(batch.Entries), batch.Truncated)
	if err != nil {
		return 0, err
	}
	if err := audit(ctx, tx, job.OrganizationID, "content.inventory.discover", job.SiteID,
		map[string]any{"job_id": job.ID, "discovered": len(batch.Entries), "truncated": batch.Truncated}); err != nil {
		return 0, err
	}
	return len(batch.Entries), tx.Commit()
}

func queueDiscoveredPage(ctx context.Context, tx *sql.Tx, parent domain.Job, item domain.InventoryEntry) error {
	page := domain.SitePage{State: "discovered", IndexStatus: "unverified", Categories: item.Categories, Tags: item.Tags,
		PageEvidence: domain.PageEvidence{URL: item.URL, PageType: item.Kind, LastModified: item.ModifiedAt, Source: "site_inventory"}}
	digest := sha256.Sum256([]byte(item.URL))
	payload, err := json.Marshal(page)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO public.seo_site_inventory (id,organization_id,site_id,url,url_digest,data)
		VALUES ($1,$2,$3,$4,$5,$6::jsonb) ON CONFLICT (organization_id,site_id,url_digest) DO UPDATE SET
		data=public.seo_site_inventory.data || (EXCLUDED.data - 'state' - 'index_status' - 'page_type' - 'last_modified' - 'source')`,
		uuid.NewString(), parent.OrganizationID, parent.SiteID, item.URL, digest[:], string(payload)); err != nil {
		return err
	}
	input := domain.ContentJobInput{SiteID: parent.SiteID, Kind: domain.ContentExtract, URL: item.URL, Priority: 10,
		IdempotencyKey: "extract:" + parent.ID + ":" + hex.EncodeToString(digest[:])}
	raw, err := json.Marshal(input)
	if err != nil {
		return err
	}
	job, err := scanJob(tx.QueryRowContext(ctx, `INSERT INTO public.seo_jobs (id,organization_id,site_id,kind,idempotency_key,priority)
		VALUES ($1,$2,$3,$4,$5,$6) ON CONFLICT (organization_id,idempotency_key) DO NOTHING RETURNING `+jobColumns,
		uuid.NewString(), parent.OrganizationID, parent.SiteID, input.Kind, input.IdempotencyKey, input.Priority))
	if errors.Is(err, domain.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := createContentJob(ctx, tx, job, input, raw); err != nil {
		return err
	}
	return auditJob(ctx, tx, "job.enqueue", job)
}

func (s *Store) SaveInventoryPage(ctx context.Context, lease domain.Lease, policy domain.CrawlPolicy, page domain.SitePage) (string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	job, err := lockLease(ctx, tx, lease)
	if err != nil {
		return "", err
	}
	if err := activeCrawlPolicy(ctx, tx, job.OrganizationID, job.SiteID, policy); err != nil {
		return "", err
	}
	site, requested, err := inventoryJobTarget(ctx, tx, job, domain.ContentExtract)
	if err != nil {
		return "", err
	}
	target, err := contentinventory.ValidatePageURL(site, policy, requested)
	if err != nil || target != page.URL {
		return "", domain.ErrCrawlUnauthorized
	}
	digest := sha256.Sum256([]byte(page.URL))
	if page.Fingerprint == "" {
		observation := sha256.Sum256([]byte(page.URL + "|" + page.State + "|" + strconv.Itoa(page.HTTPStatus)))
		page.Fingerprint = hex.EncodeToString(observation[:])
	}
	if page.CheckedAt == nil {
		now := time.Now().UTC()
		page.CheckedAt = &now
	}
	page.IndexStatus = "unverified"
	raw, err := json.Marshal(page)
	if err != nil {
		return "", err
	}
	var id string
	err = tx.QueryRowContext(ctx, `INSERT INTO public.seo_site_inventory (id,organization_id,site_id,url,url_digest,data,checked_at)
		VALUES ($1,$2,$3,$4,$5,$6::jsonb,$7) ON CONFLICT (organization_id,site_id,url_digest)
		DO UPDATE SET data=EXCLUDED.data,checked_at=EXCLUDED.checked_at RETURNING id::text`,
		uuid.NewString(), job.OrganizationID, job.SiteID, page.URL, digest[:], string(raw), page.CheckedAt).Scan(&id)
	if err != nil {
		return "", err
	}
	var snapshot string
	err = tx.QueryRowContext(ctx, `INSERT INTO public.seo_site_page_snapshots (id,organization_id,site_id,page_id,fingerprint,data)
		VALUES ($1,$2,$3,$4,$5,$6::jsonb) ON CONFLICT (organization_id,site_id,page_id,fingerprint) DO NOTHING RETURNING id::text`,
		uuid.NewString(), job.OrganizationID, job.SiteID, id, page.Fingerprint, string(raw)).Scan(&snapshot)
	if errors.Is(err, sql.ErrNoRows) {
		err = tx.QueryRowContext(ctx, `SELECT id::text FROM public.seo_site_page_snapshots WHERE organization_id=$1 AND site_id=$2
			AND page_id=$3 AND fingerprint=$4`, job.OrganizationID, job.SiteID, id, page.Fingerprint).Scan(&snapshot)
	}
	if err != nil {
		return "", err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE public.seo_site_inventory SET current_snapshot_id=$4
		WHERE organization_id=$1 AND site_id=$2 AND id=$3`, job.OrganizationID, job.SiteID, id, snapshot); err != nil {
		return "", err
	}
	if err := audit(ctx, tx, job.OrganizationID, "content.inventory.observe", id,
		map[string]any{"job_id": job.ID, "snapshot_id": snapshot, "http_status": page.HTTPStatus, "unchanged": page.Unchanged}); err != nil {
		return "", err
	}
	return id, tx.Commit()
}

func inventoryJobTarget(ctx context.Context, tx *sql.Tx, j domain.Job, kind string) (domain.Site, string, error) {
	site := domain.Site{ID: j.SiteID, OrganizationID: j.OrganizationID, Status: "active"}
	if j.Kind != kind {
		return site, "", domain.ErrNotFound
	}
	var target string
	err := tx.QueryRowContext(ctx, `SELECT s.base_url, COALESCE(c.request->>'url','') FROM public.seo_content_jobs c
		JOIN public.seo_sites s ON (c.organization_id,c.site_id)=(s.organization_id,s.id)
		WHERE c.organization_id=$1 AND c.site_id=$2 AND c.job_id=$3 AND c.project_id IS NULL`,
		j.OrganizationID, j.SiteID, j.ID).Scan(&site.BaseURL, &target)
	return site, target, classifyError(err)
}
