package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/Autumn-27/artex/seoos/internal/domain"
)

func checkIntegrationQuota(ctx context.Context, tx *sql.Tx, org string, a domain.IntegrationAccount) error {
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "integration-account:"+org+":"+a.ID); err != nil {
		return err
	}
	var countMinute, countDay int
	var now, day time.Time
	var oldest sql.NullTime
	err := tx.QueryRowContext(ctx, `WITH bounds AS (
		SELECT statement_timestamp() AS ts,date_trunc('day',statement_timestamp(),'UTC') AS day), requests AS (
		SELECT requested_at FROM public.seo_integration_account_calls WHERE organization_id=$1 AND account_id=$2
		AND requested_at >= (SELECT least(day,ts-interval '1 minute') FROM bounds)
		UNION ALL SELECT requested_at FROM public.seo_execution_request_calls WHERE organization_id=$1 AND integration_account_id=$2
		AND requested_at >= (SELECT least(day,ts-interval '1 minute') FROM bounds))
		SELECT count(requested_at) FILTER (WHERE requested_at>ts-interval '1 minute'),count(requested_at) FILTER (WHERE requested_at>=day),
		min(requested_at) FILTER (WHERE requested_at>ts-interval '1 minute'),ts,day FROM bounds LEFT JOIN requests ON true GROUP BY ts,day`,
		org, a.ID).Scan(&countMinute, &countDay, &oldest, &now, &day)
	if err != nil {
		return err
	}
	return requestBudget(countMinute, countDay, a.RequestsPerMinute, a.RequestsPerDay, oldest, now, day)
}

func requestBudget(countMinute, countDay, rpm, rpd int, oldest sql.NullTime, now, day time.Time) error {
	var wait time.Duration
	if countDay >= rpd {
		wait = day.Add(24 * time.Hour).Sub(now)
	} else if countMinute >= rpm {
		wait = oldest.Time.Add(time.Minute).Sub(now)
	}
	if wait <= 0 {
		return nil
	}
	return &domain.DataError{Code: "execution_budget", Retry: true, RetryAfter: wait, KnownUnbilled: true}
}

func (s *Store) ReserveIntegrationRequest(ctx context.Context, org, account string, version int64, operation string, allowInactive bool) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	// Refresh may already hold an account lock on a different connection.
	// The quota uses its own lock and the insert only takes a foreign-key lock.
	current, _, err := integrationAccount(ctx, tx, org, account, "")
	if err != nil {
		return 0, err
	}
	if current.Version != version || !allowInactive && current.Status != domain.IntegrationConnected {
		return 0, domain.ErrIntegrationRevoked
	}
	if err = checkIntegrationQuota(ctx, tx, org, current); err != nil {
		return 0, err
	}
	var id int64
	err = tx.QueryRowContext(ctx, `INSERT INTO public.seo_integration_account_calls (organization_id,account_id,operation)
		VALUES ($1,$2,$3) RETURNING id`, org, account, operation).Scan(&id)
	if err != nil {
		return 0, err
	}
	return id, tx.Commit()
}

func (s *Store) SettleIntegrationRequest(ctx context.Context, org, account string, id int64, receipt domain.ProviderReceipt) error {
	raw, err := json.Marshal(receipt)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE public.seo_integration_account_calls SET diagnostic=$4::jsonb
		WHERE organization_id=$1 AND account_id=$2 AND id=$3 AND diagnostic IS NULL`, org, account, id, string(raw))
	return err
}

func (s *Store) fenceExecutionIntegration(ctx context.Context, tx *sql.Tx, job string, c *domain.ExecutionConnection) error {
	_, managed, err := s.resolveIntegration(ctx, tx, c, false)
	if err != nil || !managed || c.IntegrationAccountID == "" {
		return err
	}
	if job == "" {
		return domain.ErrExecutionAuthority
	}
	var account string
	var version int64
	if err = tx.QueryRowContext(ctx, `SELECT coalesce(integration_account_id::text,''),coalesce(integration_account_version,0)
		FROM public.seo_execution_jobs WHERE organization_id=$1 AND site_id=$2 AND job_id=$3 AND connection_id=$4`,
		c.OrganizationID, c.SiteID, job, c.ID).Scan(&account, &version); err != nil {
		return classifyError(err)
	}
	if !c.MatchesIntegration(account, version) {
		return domain.ErrIntegrationRevoked
	}
	current, _, err := integrationAccount(ctx, tx, c.OrganizationID, c.IntegrationAccountID, " FOR SHARE OF a")
	if err != nil {
		return err
	}
	if current.Version != c.IntegrationVersion || !current.Allows(c.Kind, c.AllowWrites) {
		return domain.ErrIntegrationRevoked
	}
	return nil
}
