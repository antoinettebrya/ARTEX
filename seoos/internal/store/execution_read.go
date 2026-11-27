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
		query = `SELECT (CASE WHEN c.kind='indexnow' THEN c.settings-'key_location' ELSE c.settings END) || jsonb_build_object('id',c.id,'organization_id',c.organization_id,
			'version',c.version,'updated_at',c.updated_at,
			'managed_integration',b.id IS NOT NULL,
			'enabled',coalesce(b.enabled,(c.settings->>'enabled')::boolean,false),
			'configured',c.configured_site_url=s.base_url AND CASE WHEN b.id IS NULL THEN c.credential IS NOT NULL
				WHEN b.provider='indexnow' THEN k2.encrypted_key IS NOT NULL
				ELSE a.authorization_status='CONNECTED' AND a.encrypted_credentials IS NOT NULL
					AND (a.auth_mode='api_key' OR p.enabled) END,
			'capabilities',COALESCE(k.data->'capabilities','{}'::jsonb),'site_info',k.data-'capabilities') FROM public.seo_cms_connections c
			JOIN public.seo_sites s ON (s.organization_id,s.id)=(c.organization_id,c.site_id)
			LEFT JOIN public.seo_cms_capabilities k ON (k.organization_id,k.site_id,k.connection_id)=(c.organization_id,c.site_id,c.id)
			LEFT JOIN public.seo_site_integration_bindings b ON (b.organization_id,b.site_id,b.id)=(c.organization_id,c.site_id,c.id)
			LEFT JOIN public.seo_indexnow_site_keys k2 ON (k2.organization_id,k2.site_id,k2.id)=(b.organization_id,b.site_id,b.id)
			LEFT JOIN public.seo_integration_accounts a ON (a.organization_id,a.id)=(b.organization_id,b.integration_account_id)
			LEFT JOIN public.seo_integration_applications p ON (p.organization_id,p.id)=(a.organization_id,a.application_id)
			WHERE c.organization_id=$1 AND c.site_id=$2 AND b.deleted_at IS NULL
			AND (NULLIF($5,'') IS NULL OR c.kind=$5 OR $5='cms' AND c.kind IN ('wordpress','generic')
			OR $5='legacy_search' AND c.kind IN ('gsc','ga4','bing','indexnow') AND b.id IS NULL)
			ORDER BY c.updated_at DESC,c.id`
		args = append(args, f.Kind)
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
	case "report-quality":
		query = `SELECT jsonb_build_object('job_id',j.id,'observed_at',j.updated_at,'from',e.request->>'from','to',e.request->>'to',
			'quality',j.result->'report_quality')
			FROM public.seo_execution_jobs e
			JOIN public.seo_jobs j ON (j.organization_id,j.site_id,j.id)=(e.organization_id,e.site_id,e.job_id)
			JOIN public.seo_cms_connections c ON (c.organization_id,c.site_id,c.id,c.version)=(e.organization_id,e.site_id,e.connection_id,e.connection_version)
			WHERE e.organization_id=$1 AND e.site_id=$2 AND (NULLIF($5,'') IS NULL OR e.connection_id=NULLIF($5,'')::uuid)
			AND j.kind='ga4_sync' AND j.state='succeeded' AND j.result ? 'report_quality'
			ORDER BY j.updated_at DESC,j.id`
		args = append(args, f.ConnectionID)
	case "pages":
		query = `SELECT data || jsonb_build_object('connection_id',connection_id) FROM public.seo_cms_pages ` + filter + `ORDER BY observed_at DESC,remote_id`
		args = append(args, f.ConnectionID)
	case "media":
		query = `SELECT data || jsonb_build_object('connection_id',connection_id) FROM public.seo_cms_media ` + filter + `ORDER BY observed_at DESC,remote_id`
		args = append(args, f.ConnectionID)
	case "properties", "metrics":
		var sources []string
		usesSeries := false
		searchType, dataState, err := domain.GSCSeries(f.SearchType, f.DataState)
		if err != nil {
			return nil, err
		}
		for _, kind := range []string{domain.ExecutionGSC, domain.ExecutionGA4, domain.ExecutionBing} {
			if f.Kind != "" && f.Kind != kind {
				continue
			}
			if section == "properties" {
				sources = append(sources, `SELECT data || jsonb_build_object('connection_id',connection_id,'kind','`+kind+`') AS value,
					observed_at AS sort_time FROM public.seo_`+kind+`_properties `+filter)
			} else {
				table := `public.seo_` + kind + `_daily_metrics`
				extra := ""
				if kind == domain.ExecutionGSC {
					usesSeries = true
					// Rows are swapped only on a complete or explicitly truncated day.
					// A newer failed attempt makes old detail unavailable.
					extra = `AND m.search_type=$10 AND m.data_state=$11 AND coalesce((SELECT snap.state IN ('complete','truncated') AND snap.connection_version=c.version
						FROM public.seo_gsc_daily_snapshots snap JOIN public.seo_cms_connections c ON c.id=snap.connection_id
						WHERE (snap.organization_id,snap.site_id,snap.connection_id,snap.day)=(m.organization_id,m.site_id,m.connection_id,m.day)
						AND snap.search_type=m.search_type AND snap.data_state=m.data_state
						AND snap.state<>'pending' ORDER BY snap.started_at DESC,snap.job_id DESC LIMIT 1),true) `
				}
				sources = append(sources, `SELECT data || jsonb_build_object('connection_id',connection_id,'kind','`+kind+`','observed_at',observed_at) AS value,
					day::timestamp AS sort_time FROM `+table+` m `+filter+
					`AND (NULLIF($6,'') IS NULL OR day>=NULLIF($6,'')::date)
					AND (NULLIF($7,'') IS NULL OR day<=NULLIF($7,'')::date)
					AND (NULLIF($8,'') IS NULL OR data->>'page'=$8)
					AND (NULLIF($9,'') IS NULL OR lower(data->>'country')=lower($9)) `+extra)
			}
		}
		if len(sources) == 0 {
			return []json.RawMessage{}, nil
		}
		query = "SELECT value FROM (" + joinExecutionSQL(sources) + ") d ORDER BY sort_time DESC,value::text"
		args = append(args, f.ConnectionID)
		if section == "metrics" {
			args = append(args, f.From, f.To, f.Page, f.Country)
			if usesSeries {
				args = append(args, searchType, dataState)
			}
		}
	case "snapshots":
		searchType, dataState, err := domain.GSCSeries(f.SearchType, f.DataState)
		if err != nil {
			return nil, err
		}
		query = `SELECT jsonb_build_object('provider','gsc','date',snap.day,'site_id',snap.site_id,
			'connection_id',snap.connection_id,'job_id',snap.job_id,'state',snap.state,'search_type',snap.search_type,
			'data_state',snap.data_state,'first_incomplete_date',snap.first_incomplete_date,'aggregation','byPage',
			'freshness',CASE WHEN snap.day>=snap.first_incomplete_date THEN 'preliminary'
				WHEN snap.totals_checked AND snap.totals IS NOT NULL THEN 'final' ELSE 'unknown' END,
			'rows',snap.rows_collected,'error_code',snap.error_code,'observed_at',snap.observed_at,
			'totals_state',CASE WHEN snap.totals IS NOT NULL THEN 'observed' ELSE 'unknown' END,
			'totals',snap.totals) FROM (
				SELECT DISTINCT ON (d.day,d.connection_id) d.* FROM public.seo_gsc_daily_snapshots d
				JOIN public.seo_cms_connections c ON (c.organization_id,c.site_id,c.id,c.version)=
					(d.organization_id,d.site_id,d.connection_id,d.connection_version)
				WHERE d.organization_id=$1 AND d.site_id=$2 AND ($5='' OR d.connection_id=NULLIF($5,'')::uuid)
				AND ($6='' OR d.day>=NULLIF($6,'')::date) AND ($7='' OR d.day<=NULLIF($7,'')::date)
				AND d.search_type=$8 AND d.data_state=$9
				ORDER BY d.day,d.connection_id,d.started_at DESC,d.job_id DESC
			) snap ORDER BY snap.day DESC,snap.connection_id`
		args = append(args, f.ConnectionID, f.From, f.To, searchType, dataState)
	case "index":
		query = `SELECT data FROM public.seo_indexing_records ` + core + `AND ($5='' OR provider=$5) ORDER BY observed_at DESC,url_hash,provider`
		args = append(args, f.Kind)
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
