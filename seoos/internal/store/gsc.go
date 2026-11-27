package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/Autumn-27/artex/seoos/internal/domain"
)

// StartGSCDays freezes the date window at the first attempt. Reclaims and
// rate-limit deferrals always read the same set of days.
func (s *Store) StartGSCDays(ctx context.Context, lease domain.Lease, c domain.ExecutionConnection, from, to string) error {
	if _, _, err := domain.ExecutionDateRange(from, to); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	job, err := lockLease(ctx, tx, lease)
	if err != nil {
		return err
	}
	if job.Kind != domain.ExecGSCSync || job.SiteID != c.SiteID || job.OrganizationID != c.OrganizationID {
		return domain.ErrExecutionAuthority
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO public.seo_gsc_daily_snapshots
		(organization_id,site_id,connection_id,connection_version,job_id,day,started_at,search_type,data_state)
		SELECT e.organization_id,e.site_id,e.connection_id,e.connection_version,e.job_id,day::date,$5,
			coalesce(nullif(e.request->>'search_type',''),'web'),coalesce(nullif(e.request->>'data_state',''),'final')
		FROM public.seo_execution_jobs e
		CROSS JOIN generate_series($3::date::timestamp,$4::date::timestamp,interval '1 day') day
		WHERE e.organization_id=$1 AND e.job_id=$2
		ON CONFLICT (organization_id,job_id,day) DO NOTHING`, job.OrganizationID, job.ID, from, to, job.CreatedAt); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) NextGSCDay(ctx context.Context, org, job string) (*domain.GSCCursor, error) {
	var out domain.GSCCursor
	err := s.db.QueryRowContext(ctx, `SELECT day::text,totals_checked,next_row,search_type,data_state FROM public.seo_gsc_daily_snapshots
		WHERE organization_id=$1 AND job_id=$2 AND state='pending' ORDER BY day LIMIT 1`, org, job).
		Scan(&out.Day, &out.TotalsChecked, &out.NextRow, &out.SearchType, &out.DataState)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &out, err
}

func (s *Store) GSCProgress(ctx context.Context, org, job string) (domain.GSCProgress, error) {
	var out domain.GSCProgress
	err := s.db.QueryRowContext(ctx, `SELECT coalesce(min(day)::text,''),coalesce(max(day)::text,''),
		count(*) FILTER (WHERE state='pending'),count(*) FILTER (WHERE state='complete'),
		count(*) FILTER (WHERE state='truncated'),count(*) FILTER (WHERE state='failed'),
		coalesce(sum(rows_collected),0)
		FROM public.seo_gsc_daily_snapshots WHERE organization_id=$1 AND job_id=$2`, org, job).
		Scan(&out.From, &out.To, &out.Pending, &out.Complete, &out.Truncated, &out.Failed, &out.Rows)
	return out, err
}

func (s *Store) SaveGSCPage(ctx context.Context, lease domain.Lease, work domain.ExecutionWork, cursor domain.GSCCursor, page domain.GSCReportPage) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	job, err := lockLease(ctx, tx, lease)
	if err != nil {
		return err
	}
	// Serialize the day replacement with connection edits and concurrent windows.
	c, _, active, err := execConnection(ctx, tx, job.OrganizationID, job.SiteID, work.Input.ConnectionID, "FOR UPDATE OF c")
	if err != nil {
		return err
	}
	if !active || !c.Enabled || c.Version != work.ConnectionVersion || c.Kind != domain.ExecutionGSC {
		return domain.ErrExecutionAuthority
	}
	if err = s.fenceExecutionIntegration(ctx, tx, job.ID, &c); err != nil {
		return err
	}
	var offset int
	var checked bool
	var state, searchType, dataState string
	err = tx.QueryRowContext(ctx, `SELECT next_row,totals_checked,state,search_type,data_state FROM public.seo_gsc_daily_snapshots
		WHERE organization_id=$1 AND site_id=$2 AND connection_id=$3 AND job_id=$4 AND day=$5 FOR UPDATE`,
		job.OrganizationID, job.SiteID, c.ID, job.ID, cursor.Day).Scan(&offset, &checked, &state, &searchType, &dataState)
	if err != nil {
		return classifyError(err)
	}
	if state != "pending" || offset != cursor.NextRow || checked != cursor.TotalsChecked ||
		searchType != cursor.SearchType || dataState != cursor.DataState {
		return domain.ErrExecutionState
	}
	metrics := page.Metrics
	if !checked {
		if len(metrics) > 1 || len(metrics) == 1 && metrics[0].Date != cursor.Day {
			return domain.ErrExecutionState
		}
		var totals any
		if len(metrics) == 1 {
			raw, err := json.Marshal(metrics[0].Values)
			if err != nil {
				return err
			}
			totals = string(raw)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE public.seo_gsc_daily_snapshots
			SET totals_checked=true,totals=$4::jsonb,observed_at=clock_timestamp(),first_incomplete_date=NULLIF($5,'')::date
			WHERE organization_id=$1 AND job_id=$2 AND day=$3`, job.OrganizationID, job.ID, cursor.Day, totals, page.FirstIncompleteDate); err != nil {
			return err
		}
		return tx.Commit()
	}
	if len(metrics) > domain.GSCPageSize || offset+len(metrics) > domain.GSCDetailLimit {
		return domain.ErrExecutionState
	}

	added, err := stageGSCMetrics(ctx, tx, job, c.BaseURL, cursor.Day, metrics)
	if err != nil {
		return err
	}
	next := offset + len(metrics)
	switch {
	case added != int64(len(metrics)) || next == domain.GSCDetailLimit:
		state = "truncated"
	case len(metrics) < domain.GSCPageSize:
		state = "complete"
	}
	if _, err := tx.ExecContext(ctx, `UPDATE public.seo_gsc_daily_snapshots
		SET next_row=$4,rows_collected=rows_collected+$5,state=$6,observed_at=clock_timestamp(),
			first_incomplete_date=least(first_incomplete_date,NULLIF($7,'')::date)
		WHERE organization_id=$1 AND job_id=$2 AND day=$3`,
		job.OrganizationID, job.ID, cursor.Day, next, added, state, page.FirstIncompleteDate); err != nil {
		return err
	}
	if state != "pending" {
		if err := publishGSCDay(ctx, tx, job, c, cursor); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func stageGSCMetrics(ctx context.Context, tx *sql.Tx, job domain.Job, baseURL, day string, metrics []domain.ExecutionMetric) (int64, error) {
	type detail struct {
		Hash string                 `json:"hash"`
		Data domain.ExecutionMetric `json:"data"`
	}
	rows := make([]detail, 0, len(metrics))
	for _, metric := range metrics {
		u, err := resolveMetricPage(baseURL, metric.Page)
		if err != nil || metric.Page == "" || metric.Date != day || !domain.BelongsToSite(baseURL, u) {
			return 0, domain.ErrExecutionState
		}
		metric.Page = u.String()
		key, err := json.Marshal([]string{metric.Page, metric.Query, metric.Country, metric.Device})
		if err != nil {
			return 0, err
		}
		hash := sha256.Sum256(key)
		rows = append(rows, detail{hex.EncodeToString(hash[:]), metric})
	}
	raw, err := json.Marshal(rows)
	if err != nil {
		return 0, err
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO public.seo_gsc_snapshot_rows (organization_id,job_id,day,dimension_hash,data)
		SELECT $1,$2,$3,decode(r.hash,'hex'),r.data
		FROM jsonb_to_recordset($4::jsonb) AS r(hash text,data jsonb) ON CONFLICT DO NOTHING`,
		job.OrganizationID, job.ID, day, string(raw))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func publishGSCDay(ctx context.Context, tx *sql.Tx, job domain.Job, c domain.ExecutionConnection, cursor domain.GSCCursor) error {
	day := cursor.Day
	searchType, dataState, err := domain.GSCSeries(cursor.SearchType, cursor.DataState)
	if err != nil {
		return err
	}
	var newest string
	err = tx.QueryRowContext(ctx, `SELECT job_id::text FROM public.seo_gsc_daily_snapshots
		WHERE organization_id=$1 AND site_id=$2 AND connection_id=$3 AND connection_version=$4 AND day=$5
		AND search_type=$6 AND data_state=$7 AND state IN ('complete','truncated') ORDER BY started_at DESC,job_id DESC LIMIT 1`,
		job.OrganizationID, job.SiteID, c.ID, c.Version, day, searchType, dataState).Scan(&newest)
	if err != nil {
		return err
	}
	if newest == job.ID {
		// Empty finished snapshots also replace all prior rows for the day.
		if _, err := tx.ExecContext(ctx, `DELETE FROM public.seo_gsc_daily_metrics
			WHERE organization_id=$1 AND site_id=$2 AND connection_id=$3 AND day=$4 AND search_type=$5 AND data_state=$6`,
			job.OrganizationID, job.SiteID, c.ID, day, searchType, dataState); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO public.seo_gsc_daily_metrics
			(organization_id,site_id,connection_id,day,dimension_hash,data,search_type,data_state)
			SELECT r.organization_id,$4,$5,r.day,r.dimension_hash,r.data,$6,$7
			FROM public.seo_gsc_snapshot_rows r WHERE r.organization_id=$1 AND r.job_id=$2 AND r.day=$3`,
			job.OrganizationID, job.ID, day, job.SiteID, c.ID, searchType, dataState); err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, `DELETE FROM public.seo_gsc_snapshot_rows WHERE organization_id=$1 AND job_id=$2 AND day=$3`,
		job.OrganizationID, job.ID, day)
	return err
}

func abandonGSCDays(ctx context.Context, tx *sql.Tx, job domain.Job, reason string) error {
	if job.Kind != domain.ExecGSCSync {
		return nil
	}
	if _, err := tx.ExecContext(ctx, `UPDATE public.seo_gsc_daily_snapshots
		SET state='failed',error_code=$3,observed_at=clock_timestamp()
		WHERE organization_id=$1 AND job_id=$2 AND state='pending'`, job.OrganizationID, job.ID, reason); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `DELETE FROM public.seo_gsc_snapshot_rows WHERE organization_id=$1 AND job_id=$2`, job.OrganizationID, job.ID)
	return err
}
