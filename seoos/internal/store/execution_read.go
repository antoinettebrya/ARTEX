package store

import (
	"context"
	"encoding/json"

	"github.com/Autumn-27/artex/seoos/internal/domain"
)

func executionAnalytics(kind string) bool {
	return kind == domain.ExecutionGSC || kind == domain.ExecutionGA4 || kind == domain.ExecutionBing
}

func (s *Store) ExecutionRows(ctx context.Context, org, section string, f domain.ExecutionFilter) ([]json.RawMessage, error) {
	if _, err := s.GetSite(ctx, org, f.SiteID); err != nil {
		return nil, err
	}
	if err := domain.ValidatePage(f.Limit, f.Offset); err != nil {
		return nil, err
	}
	if f.ConnectionID != "" && !domain.ValidID(f.ConnectionID) {
		return nil, domain.ErrNotFound
	}
	args := []any{org, f.SiteID, f.Limit, f.Offset}
	core := `WHERE organization_id=$1 AND site_id=$2 `
	filter := core + `AND (NULLIF($5,'') IS NULL OR connection_id=NULLIF($5,'')::uuid) `
	var query string
	switch section {
	case "connections":
		query = `SELECT c.settings || jsonb_build_object('id',c.id,'organization_id',c.organization_id,
			'version',c.version,'updated_at',c.updated_at,'configured',c.credential IS NOT NULL AND c.configured_site_url=s.base_url,
			'capabilities',COALESCE(k.data->'capabilities','{}'::jsonb),'site_info',k.data-'capabilities') FROM public.seo_cms_connections c
			JOIN public.seo_sites s ON (s.organization_id,s.id)=(c.organization_id,c.site_id)
			LEFT JOIN public.seo_cms_capabilities k ON (k.organization_id,k.site_id,k.connection_id)=(c.organization_id,c.site_id,c.id)
			WHERE c.organization_id=$1 AND c.site_id=$2 ORDER BY c.updated_at DESC,c.id`
	case "packages":
		query = `SELECT p.data || jsonb_build_object('project_id',p.project_id)
			FROM public.seo_content_publications p JOIN public.seo_content_projects x
			ON (x.organization_id,x.site_id,x.id)=(p.organization_id,p.site_id,p.project_id)
			WHERE p.organization_id=$1 AND p.site_id=$2 AND x.status='prepared'
			AND (x.data->>'article_version')::int=p.version AND (x.data->'quality'->>'passed')::boolean
			AND (x.data->'fact_check'->>'passed')::boolean AND (x.data->'review'->>'passed')::boolean
			ORDER BY p.created_at DESC,p.id`
	case "publications":
		query = `SELECT ` + releaseJSON + releaseJoin + `WHERE r.organization_id=$1 AND r.site_id=$2 ORDER BY r.created_at DESC,r.id`
	case "jobs":
		query = `SELECT jsonb_build_object('job',to_jsonb(j) - 'lease_token' - 'lease_until' - 'priority','request',e.request-'media_base64')
			FROM public.seo_execution_jobs e JOIN public.seo_jobs j ON (j.organization_id,j.id)=(e.organization_id,e.job_id)
			WHERE e.organization_id=$1 AND e.site_id=$2 ORDER BY j.created_at DESC,j.id`
	case "pages":
		query = `SELECT data || jsonb_build_object('connection_id',connection_id) FROM public.seo_cms_pages ` + filter + `ORDER BY observed_at DESC,remote_id`
		args = append(args, f.ConnectionID)
	case "media":
		query = `SELECT data || jsonb_build_object('connection_id',connection_id) FROM public.seo_cms_media ` + filter + `ORDER BY observed_at DESC,remote_id`
		args = append(args, f.ConnectionID)
	case "properties", "metrics":
		var sources []string
		for _, kind := range []string{domain.ExecutionGSC, domain.ExecutionGA4, domain.ExecutionBing} {
			if f.Kind != "" && f.Kind != kind {
				continue
			}
			if section == "properties" {
				sources = append(sources, `SELECT data || jsonb_build_object('connection_id',connection_id,'kind','`+kind+`') AS value,
					observed_at AS sort_time FROM public.seo_`+kind+`_properties `+filter)
			} else {
				sources = append(sources, `SELECT data || jsonb_build_object('connection_id',connection_id,'kind','`+kind+`') AS value,
					day::timestamp AS sort_time FROM public.seo_`+kind+`_daily_metrics `+filter+
					`AND (NULLIF($6,'') IS NULL OR day>=NULLIF($6,'')::date)
					AND (NULLIF($7,'') IS NULL OR day<=NULLIF($7,'')::date)
					AND (NULLIF($8,'') IS NULL OR data->>'page'=$8)
					AND (NULLIF($9,'') IS NULL OR lower(data->>'country')=lower($9)) `)
			}
		}
		if len(sources) == 0 {
			return []json.RawMessage{}, nil
		}
		query = "SELECT value FROM (" + joinExecutionSQL(sources) + ") d ORDER BY sort_time DESC,value::text"
		args = append(args, f.ConnectionID)
		if section == "metrics" {
			args = append(args, f.From, f.To, f.Page, f.Country)
		}
	case "index":
		query = `SELECT data FROM public.seo_indexing_records ` + core + `ORDER BY observed_at DESC,url_hash,provider`
	case "submissions":
		query = `SELECT data || jsonb_build_object('job_id',job_id,'provider',provider,'created_at',created_at)
			FROM public.seo_indexing_submissions ` + core + `ORDER BY created_at DESC,job_id`
	case "sitemaps":
		query = `SELECT data FROM public.seo_execution_sitemaps ` + core + `ORDER BY observed_at DESC,url_hash`
	case "extractions":
		query = `SELECT data || jsonb_build_object('job_id',job_id,'created_at',created_at)
			FROM public.seo_page_extraction_jobs ` + core + `ORDER BY created_at DESC,job_id`
	case "usage":
		if !executionOwner(ctx, org) {
			return nil, domain.ErrExecutionAuthority
		}
		query = `SELECT value FROM (
			SELECT c.name SORT_KEY,jsonb_build_object('connection_id',c.id,'name',c.name,'requests',COUNT(u.requested_at),
			'requests_per_day',(c.settings->>'requests_per_day')::int,'date_utc',to_char(now() AT TIME ZONE 'UTC','YYYY-MM-DD'),
			'cost_micros',0) value
			FROM public.seo_cms_connections c LEFT JOIN public.seo_execution_request_calls u
			ON (u.organization_id,u.site_id,u.connection_id)=(c.organization_id,c.site_id,c.id)
			AND u.requested_at>=date_trunc('day',clock_timestamp(),'UTC')
			WHERE c.organization_id=$1 AND c.site_id=$2 GROUP BY c.id
			UNION ALL SELECT '站点采集',jsonb_build_object('connection_id','','name','站点采集','requests',COUNT(*),
			'requests_per_day',1000,'date_utc',to_char(now() AT TIME ZONE 'UTC','YYYY-MM-DD'),'cost_micros',0)
			FROM public.seo_execution_request_calls WHERE organization_id=$1 AND site_id=$2 AND connection_id IS NULL
			AND requested_at>=date_trunc('day',clock_timestamp(),'UTC') HAVING COUNT(*)>0
			) usage ORDER BY sort_key,value::text`
	case "feedback":
		query = `SELECT jsonb_build_object('project_id',p.id,'target_url',p.data->'strategy'->>'target_url',
			'keyword',k.keyword,'signals',p.data->'signals','updated_at',p.updated_at)
			FROM public.seo_content_projects p JOIN public.seo_keywords k ON (k.organization_id,k.id)=(p.organization_id,p.keyword_id)
			WHERE p.organization_id=$1 AND p.site_id=$2 AND p.data ? 'execution_feedback' ORDER BY p.updated_at DESC,p.id`
	default:
		return nil, domain.ErrNotFound
	}
	return jsonRows(ctx, s.db, query+` LIMIT $3 OFFSET $4`, args...)
}

func joinExecutionSQL(values []string) string {
	out := values[0]
	for _, value := range values[1:] {
		out += " UNION ALL " + value
	}
	return out
}

func (s *Store) ExecutionSitemapOutput(ctx context.Context, org, site, job string) (json.RawMessage, error) {
	if !domain.ValidID(site) || !domain.ValidID(job) {
		return nil, domain.ErrNotFound
	}
	var document []byte
	err := s.db.QueryRowContext(ctx, `SELECT data FROM public.seo_sitemap_jobs WHERE organization_id=$1 AND site_id=$2 AND job_id=$3`, org, site, job).Scan(&document)
	return document, classifyError(err)
}
