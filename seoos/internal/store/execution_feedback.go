package store

import (
	"context"
	"database/sql"

	"github.com/Autumn-27/artex/seoos/internal/domain"
)

// Comparison windows end on the connection's latest observed date. Only
// mapped URLs with observations in both windows contribute editorial signals.
func updateExecutionFeedback(ctx context.Context, tx *sql.Tx, c domain.ExecutionConnection, useVerifiedGSCDays ...bool) error {
	value, filters, snapshot := "", "", ""
	if c.Kind == domain.ExecutionGA4 {
		value = `jsonb_build_object('traffic_change',100*(new_sessions-old_sessions)/NULLIF(old_sessions,0),
			'evidence_url','https://analytics.google.com/')`
		filters = `AND old_sessions>0`
	} else if c.Kind == domain.ExecutionGSC {
		value = `jsonb_build_object('ctr_change',100*(new_clicks/NULLIF(new_impressions,0)-old_clicks/NULLIF(old_impressions,0)),
			'rank_change',new_weighted/NULLIF(new_rank_impressions,0)-old_weighted/NULLIF(old_rank_impressions,0),
			'evidence_url','https://search.google.com/search-console')`
		filters = `AND old_impressions>0 AND new_impressions>0`
		if len(useVerifiedGSCDays) != 0 && useVerifiedGSCDays[0] {
			snapshot = `AND (SELECT d.state FROM public.seo_gsc_daily_snapshots d
				WHERE (d.organization_id,d.site_id,d.connection_id,d.day)=(m.organization_id,m.site_id,m.connection_id,m.day)
				  AND d.connection_version=$5 AND d.state<>'pending'
				ORDER BY d.started_at DESC,d.job_id DESC LIMIT 1)='complete'`
		}
	} else {
		return domain.ErrExecutionUnsupported
	}
	statement := `WITH daily AS (
		SELECT day,data->>'page' page, (data->'values'->>'sessions')::float8 sessions,
			(data->'values'->>'impressions')::float8 impressions,(data->'values'->>'clicks')::float8 clicks,
			(data->'values'->>'position')::float8 position
		FROM public.seo_` + c.Kind + `_daily_metrics m WHERE organization_id=$1 AND site_id=$2 AND connection_id=$3 ` + snapshot + `
	), cutoff AS (SELECT max(day) latest FROM daily), station AS (
		SELECT page,
			COUNT(DISTINCT day) FILTER(WHERE day>latest-7) new_days,
			COUNT(DISTINCT day) FILTER(WHERE day<=latest-7) old_days,
			sum(sessions) FILTER(WHERE day>latest-7) new_sessions,
			sum(sessions) FILTER(WHERE day<=latest-7) old_sessions,
			sum(clicks) FILTER(WHERE day>latest-7) new_clicks,
			sum(clicks) FILTER(WHERE day<=latest-7) old_clicks,
			sum(impressions) FILTER(WHERE day>latest-7) new_impressions,
			sum(impressions) FILTER(WHERE day<=latest-7) old_impressions,
			sum(impressions) FILTER(WHERE day>latest-7 AND position IS NOT NULL) new_rank_impressions,
			sum(impressions) FILTER(WHERE day<=latest-7 AND position IS NOT NULL) old_rank_impressions,
			sum(position*impressions) FILTER(WHERE day>latest-7) new_weighted,
			sum(position*impressions) FILTER(WHERE day<=latest-7) old_weighted
		FROM daily CROSS JOIN cutoff WHERE page IS NOT NULL AND page<>'' AND day>latest-14 GROUP BY page
	), signals AS (
		SELECT page,jsonb_strip_nulls(` + value + `) value FROM station
		WHERE new_days>=4 AND old_days>=4 ` + filters + `
	)
	UPDATE public.seo_content_projects p SET data=p.data || jsonb_build_object(
		'signals',COALESCE(p.data->'signals','{}'::jsonb)||s.value,'execution_feedback',
		jsonb_build_object('kind',$4::text,'connection_id',$3::uuid,'through',cutoff.latest)),
		revision=revision+1,updated_at=clock_timestamp()
	FROM signals s CROSS JOIN cutoff WHERE p.organization_id=$1 AND p.site_id=$2 AND p.data->'strategy'->>'target_url'=s.page
	AND COALESCE(p.data->'signals','{}'::jsonb)<>(COALESCE(p.data->'signals','{}'::jsonb)||s.value)`
	args := []any{c.OrganizationID, c.SiteID, c.ID, c.Kind}
	if snapshot != "" {
		args = append(args, c.Version)
	}
	res, err := tx.ExecContext(ctx, statement, args...)
	if err != nil {
		return err
	}
	count, err := res.RowsAffected()
	if err != nil || count == 0 {
		return err
	}
	return audit(ctx, tx, c.OrganizationID, "execution.analytics.feedback", c.ID, map[string]any{"site_id": c.SiteID, "projects": count, "kind": c.Kind})
}
