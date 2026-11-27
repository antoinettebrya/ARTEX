package store

import (
	"context"
	"encoding/json"

	"github.com/Autumn-27/artex/seoos/internal/domain"
)

// Dashboard reads one internally consistent view of the authorized portfolio.
// Provider amounts are visible only to the organization owner.
func (s *Store) Dashboard(ctx context.Context, org string, scope domain.SiteScope, owner bool) (json.RawMessage, error) {
	if err := validateOrganizationID(org); err != nil {
		return nil, err
	}
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	var result []byte
	err := s.db.QueryRowContext(ctx, `
		WITH bounds AS (
			SELECT date_trunc('day',now(),'UTC') today, (now() AT TIME ZONE 'UTC')::date-28 first_day,
				(now() AT TIME ZONE 'UTC')::date-1 last_day
		), sites AS MATERIALIZED (
			SELECT id,status,base_url FROM public.seo_sites WHERE organization_id=$1
			AND ($2='' OR id=NULLIF($2,'')::uuid) AND ($3='' OR country_code=$3) AND ($4='' OR language_tag=$4)
		), current_connections AS (
			SELECT DISTINCT ON (c.site_id,c.kind) c.site_id,c.id,c.kind,c.version
			FROM public.seo_cms_connections c JOIN sites s ON s.id=c.site_id
			LEFT JOIN public.seo_site_integration_bindings b ON (b.organization_id,b.site_id,b.id)=(c.organization_id,c.site_id,c.id)
			LEFT JOIN public.seo_integration_accounts a ON (a.organization_id,a.id)=(b.organization_id,b.integration_account_id)
			LEFT JOIN public.seo_integration_applications p ON (p.organization_id,p.id)=(a.organization_id,a.application_id)
			WHERE c.organization_id=$1 AND c.configured_site_url=s.base_url
			AND (b.id IS NULL AND c.credential IS NOT NULL OR b.enabled AND b.deleted_at IS NULL
				AND a.authorization_status='CONNECTED' AND p.enabled)
			AND c.kind IN ('gsc','ga4') AND c.settings->>'enabled'='true' AND coalesce(c.settings->>'property','')<>''
			ORDER BY c.site_id,c.kind,c.updated_at DESC,c.id
		), daily_jobs AS (
			SELECT count(*) FILTER (WHERE attempts>0) executed,
				count(*) FILTER (WHERE state='succeeded') succeeded,count(*) FILTER (WHERE state='failed') failed
			FROM public.seo_jobs j JOIN sites s ON s.id=j.site_id CROSS JOIN bounds
			WHERE j.organization_id=$1 AND j.created_at>=today
		), latest_jobs AS (
			SELECT s.id,
				(SELECT j.state FROM public.seo_jobs j WHERE j.organization_id=$1 AND j.site_id=s.id
				 AND j.state IN ('succeeded','failed') ORDER BY j.created_at DESC,j.id DESC LIMIT 1) outcome,
				(SELECT j.state FROM public.seo_jobs j WHERE j.organization_id=$1 AND j.site_id=s.id
				 AND j.kind='cms_connection_test' AND j.state IN ('succeeded','failed')
				 ORDER BY j.created_at DESC,j.id DESC LIMIT 1) cms
			FROM sites s
		), projects AS (
			SELECT count(*) total,
				count(*) FILTER (WHERE coalesce((p.data->>'article_version')::int,0)=0) generation,
				count(*) FILTER (WHERE (p.data->>'article_version')::int>0) produced,
				count(*) FILTER (WHERE (p.data->>'article_version')::int>0 AND p.data->'quality'->>'passed'='true'
					AND p.status NOT IN ('approved','prepared')) pending
			FROM public.seo_content_projects p JOIN sites s ON s.id=p.site_id WHERE p.organization_id=$1
		), published AS (
			SELECT count(DISTINCT (p.site_id,p.connection_id,p.request->>'resource',p.target_id)) total
			FROM public.seo_cms_publications p JOIN sites s ON s.id=p.site_id
			WHERE p.organization_id=$1 AND p.target_id<>'' AND p.status IN ('PUBLISHED','VERIFIED')
		), gsc_days AS (
			SELECT DISTINCT ON (d.site_id,d.day) d.site_id,d.day,d.state,d.observed_at,d.totals
			FROM public.seo_gsc_daily_snapshots d CROSS JOIN bounds
			JOIN current_connections c ON c.kind='gsc'
			WHERE d.organization_id=$1 AND (d.site_id,d.connection_id,d.connection_version)=(c.site_id,c.id,c.version)
			AND d.search_type='web' AND d.data_state='final'
			AND d.day BETWEEN first_day AND last_day ORDER BY d.site_id,d.day,d.started_at DESC,d.job_id DESC
		), gsc AS (
			SELECT count(*) FILTER (WHERE totals IS NOT NULL AND state IN ('complete','truncated')) covered_days,
				count(*) FILTER (WHERE state='truncated') truncated_days,count(*) FILTER (WHERE state='failed') failed_days,
				count(*) FILTER (WHERE state='pending') pending_days,max(observed_at) observed_at,
				sum((totals->>'clicks')::float8) FILTER (WHERE state IN ('complete','truncated')) clicks,
				sum((totals->>'impressions')::float8) FILTER (WHERE state IN ('complete','truncated')) impressions,
				sum((totals->>'position')::float8*(totals->>'impressions')::float8)
					FILTER (WHERE state IN ('complete','truncated')) weighted,
				sum((totals->>'impressions')::float8) FILTER (WHERE state IN ('complete','truncated') AND totals?'position') rank_impressions
			FROM gsc_days
		), ga4 AS (
			SELECT sum((m.data->'values'->>'sessions')::float8) sessions,
				count(DISTINCT (m.site_id,m.day)) covered_days,max(m.observed_at) observed_at
			FROM public.seo_ga4_daily_metrics m CROSS JOIN bounds JOIN current_connections c ON c.kind='ga4'
			WHERE m.organization_id=$1 AND (m.site_id,m.connection_id)=(c.site_id,c.id)
			AND m.day BETWEEN first_day AND last_day
		), index_stats AS (
			SELECT i.provider,count(*) observed,max(i.observed_at) observed_at,
				count(*) FILTER (WHERE i.data->>'status'='INDEXED') indexed,
				count(*) FILTER (WHERE i.data->>'status'='NOT_INDEXED') not_indexed,
				count(*) FILTER (WHERE i.data->>'status' NOT IN ('INDEXED','NOT_INDEXED')) pending
			FROM public.seo_indexing_records i JOIN sites s ON s.id=i.site_id WHERE i.organization_id=$1 GROUP BY i.provider
		), receipts AS (
			SELECT diagnostic->>'provider'='dataforseo' research, count(*) FILTER (WHERE state NOT IN ('reserved','pending')) calls,
				count(*) FILTER (WHERE diagnostic?'input_tokens' AND diagnostic?'output_tokens') metered,
				sum((diagnostic->>'input_tokens')::bigint+(diagnostic->>'output_tokens')::bigint) tokens,
				sum((diagnostic->>'cost_micros')::bigint) cost_micros,
				count(*) FILTER (WHERE diagnostic->>'cost_micros' IS NULL AND diagnostic->>'may_be_charged'='true') unknown
			FROM public.seo_provider_receipts CROSS JOIN bounds WHERE $5 AND organization_id=$1 AND occurred_at>=today
			AND call_id NOT LIKE 'test-%' AND call_id NOT LIKE 'execution-%' GROUP BY 1
		), budgets AS (
			SELECT false research,sum((data->>'daily_cost_micros')::bigint) allowance
			FROM public.seo_content_model_profiles WHERE $5 AND organization_id=$1
			UNION ALL SELECT true,sum(daily_cost_micros) FROM public.seo_providers WHERE $5 AND organization_id=$1 AND kind='dataforseo'
		), reservations AS (
			SELECT true research,sum(coalesce(cost_micros,reserved_micros)) spent
			FROM public.seo_research_calls CROSS JOIN bounds WHERE $5 AND organization_id=$1 AND reserved_at>=today
			UNION ALL SELECT false,sum(spent)
			FROM (
				SELECT sum(coalesce(cost_micros,reserved_micros)) spent FROM public.seo_content_model_calls CROSS JOIN bounds
					WHERE $5 AND organization_id=$1 AND reserved_at>=today
				UNION ALL SELECT sum(coalesce((diagnostic->>'cost_micros')::bigint,(diagnostic->>'reserved_micros')::bigint))
					FROM public.seo_provider_calls CROSS JOIN bounds WHERE $5 AND organization_id=$1 AND reserved_at>=today
			) model
		)
		SELECT jsonb_build_object(
			'date_utc',(today AT TIME ZONE 'UTC')::date,'from',first_day,'to',last_day,
			'sites',jsonb_build_object('total',(SELECT count(*) FROM sites),'active',(SELECT count(*) FROM sites WHERE status='active'),
				'observed',(SELECT count(outcome) FROM latest_jobs),'failed',(SELECT count(*) FROM latest_jobs WHERE outcome='failed'),
				'cms_observed',(SELECT count(cms) FROM latest_jobs),'cms_failed',(SELECT count(*) FROM latest_jobs WHERE cms='failed')),
			'jobs',to_jsonb(daily_jobs),
			'content',to_jsonb(projects)||jsonb_build_object('published',published.total),
			'gsc',to_jsonb(gsc)-'weighted'-'rank_impressions'||jsonb_build_object('ctr',clicks/nullif(impressions,0),
				'position',weighted/nullif(rank_impressions,0),'connected_sites',(SELECT count(*) FROM current_connections WHERE kind='gsc')),
			'ga4',to_jsonb(ga4)||jsonb_build_object('connected_sites',(SELECT count(*) FROM current_connections WHERE kind='ga4')),
			'index',coalesce((SELECT jsonb_object_agg(provider,to_jsonb(index_stats)-'provider') FROM index_stats),'{}'::jsonb),
			'ai',CASE WHEN $5 THEN (SELECT jsonb_object_agg(CASE WHEN b.research THEN 'data' ELSE 'model' END,
				jsonb_build_object('calls',r.calls,'metered',r.metered,'tokens',r.tokens,'cost_micros',r.cost_micros,
					'unknown',r.unknown,'budget_ratio',coalesce(q.spent,0)::float8/nullif(b.allowance,0)))
				FROM budgets b JOIN reservations q USING (research) LEFT JOIN receipts r USING (research)) END
		) FROM bounds,daily_jobs,projects,published,gsc,ga4`, org, scope.SiteID, scope.Country, scope.Language, owner).Scan(&result)
	return result, err
}
