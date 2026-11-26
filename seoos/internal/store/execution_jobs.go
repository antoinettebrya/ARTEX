package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/Autumn-27/artex/seoos/internal/domain"
	"github.com/google/uuid"
)

func executionActionOwner(in domain.ExecutionJobInput) bool {
	return in.Kind == domain.ExecPublication || in.Kind == domain.ExecMediaUpload || in.Kind == domain.ExecIndexNow
}

func executionConnectionKind(kind, connection string) bool {
	switch kind {
	case domain.ExecConnectionTest:
		return connection != ""
	case domain.ExecPageSync, domain.ExecMediaUpload, domain.ExecPublication, domain.ExecVerify:
		return domain.IsCMSConnection(connection)
	case domain.ExecGSCSync:
		return connection == domain.ExecutionGSC
	case domain.ExecGA4Sync:
		return connection == domain.ExecutionGA4
	case domain.ExecBingSync:
		return connection == domain.ExecutionBing
	case domain.ExecInspection:
		return connection == domain.ExecutionGSC || connection == domain.ExecutionBing
	case domain.ExecIndexNow:
		return connection == domain.ExecutionIndexNow || connection == domain.ExecutionBing
	case domain.ExecSitemap:
		return connection == "" || domain.IsCMSConnection(connection) || connection == domain.ExecutionGSC || connection == domain.ExecutionBing
	case domain.ExecPDF, domain.ExecBrowser:
		return connection == ""
	}
	return false
}

func (s *Store) EnqueueExecution(ctx context.Context, org string, in domain.ExecutionJobInput) (domain.Job, bool, error) {
	var empty domain.Job
	if err := in.Validate(); err != nil {
		return empty, false, err
	}
	actor := domain.PrincipalFromContext(ctx)
	if actor.OrganizationID != org || !actor.Role.CanWrite() || executionActionOwner(in) && !executionOwner(ctx, org) {
		return empty, false, domain.ErrExecutionAuthority
	}
	raw, err := json.Marshal(in)
	if err != nil {
		return empty, false, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return empty, false, err
	}
	defer tx.Rollback()
	lookup := func() (domain.Job, error) {
		job, err := scanJob(tx.QueryRowContext(ctx, `SELECT `+jobColumns+` FROM public.seo_jobs
			WHERE organization_id=$1 AND idempotency_key=$2`, org, in.IdempotencyKey))
		if err != nil {
			return job, err
		}
		var same bool
		err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM public.seo_execution_jobs
			WHERE organization_id=$1 AND job_id=$2 AND request=$3::jsonb)`, org, job.ID, string(raw)).Scan(&same)
		if err == nil && !same {
			err = domain.ErrIdempotencyConflict
		}
		return job, err
	}
	if old, err := lookup(); !errors.Is(err, domain.ErrNotFound) {
		return old, false, err
	}
	var base, status string
	err = tx.QueryRowContext(ctx, `SELECT base_url,status FROM public.seo_sites WHERE organization_id=$1 AND id=$2 FOR SHARE`, org, in.SiteID).Scan(&base, &status)
	if err != nil {
		return empty, false, classifyError(err)
	}
	if status != "active" {
		return empty, false, domain.ErrSiteInactive
	}
	if in.URL != "" {
		u, err := domain.EvidenceURL(in.URL)
		if err != nil || !domain.BelongsToSite(base, u) {
			return empty, false, domain.ErrExecutionAuthority
		}
	}
	if in.Operation == "rollback" {
		if err := supersedeExecutionVerifications(ctx, tx, org, in); err != nil {
			return empty, false, err
		}
	}
	var connection domain.ExecutionConnection
	if in.ConnectionID != "" {
		var active bool
		connection, _, active, err = execConnection(ctx, tx, org, in.SiteID, in.ConnectionID, "FOR NO KEY UPDATE OF c")
		if err != nil {
			return empty, false, err
		}
		if !active || !connection.Enabled || executionActionOwner(in) && !connection.AllowWrites {
			return empty, false, domain.ErrExecutionAuthority
		}
	}
	if !executionConnectionKind(in.Kind, connection.Kind) {
		return empty, false, domain.ErrExecutionUnsupported
	}
	if executionAnalytics(connection.Kind) && (in.Kind == domain.ExecGSCSync || in.Kind == domain.ExecGA4Sync || in.Kind == domain.ExecBingSync) {
		existing, existingErr := scanJob(tx.QueryRowContext(ctx, `SELECT `+jobColumns+`
			FROM public.seo_jobs WHERE organization_id=$1 AND id=(
				SELECT e.job_id FROM public.seo_execution_jobs e JOIN public.seo_jobs j
				ON (j.organization_id,j.id)=(e.organization_id,e.job_id)
				WHERE e.organization_id=$1 AND e.site_id=$2 AND e.connection_id=$3 AND e.connection_version=$4
				AND (e.request - 'idempotency_key' - 'priority')=($5::jsonb - 'idempotency_key' - 'priority')
				AND (j.state IN ('queued','running') OR (j.state='succeeded' AND j.updated_at>clock_timestamp()-interval '2 minutes'))
				ORDER BY j.created_at DESC LIMIT 1)`, org, in.SiteID, in.ConnectionID, connection.Version, string(raw)))
		if existingErr == nil {
			return existing, false, tx.Commit()
		}
		if !errors.Is(existingErr, domain.ErrNotFound) {
			return empty, false, existingErr
		}
	}
	var release domain.ExecutionRelease
	var available *time.Time
	if in.PublicationID != "" {
		if old, err := lookup(); !errors.Is(err, domain.ErrNotFound) {
			return old, false, err
		}
		release, err = readRelease(ctx, tx, org, in.PublicationID, "FOR NO KEY UPDATE OF r")
		if err != nil {
			return empty, false, err
		}
		if release.SiteID != in.SiteID || release.ConnectionID != in.ConnectionID {
			return empty, false, domain.ErrNotFound
		}
		if !executionPublicationAllowed(release.Status, in) {
			return empty, false, domain.ErrExecutionState
		}
		if in.ScheduledAt != nil && release.RemoteID != "" {
			return empty, false, domain.ErrExecutionUnsupported
		}
		if in.ScheduledAt != nil && time.Until(*in.ScheduledAt) < time.Minute {
			return empty, false, domain.ErrExecutionState
		}
		if in.Kind == domain.ExecVerify && release.Status == domain.PublicationScheduled {
			available = release.ScheduledAt
		}
		var busy bool
		err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM public.seo_execution_jobs e JOIN public.seo_jobs j
			ON (j.organization_id,j.id)=(e.organization_id,e.job_id) JOIN public.seo_cms_publications p
			ON (p.organization_id,p.site_id,p.id)=(e.organization_id,e.site_id,e.publication_id)
			WHERE e.organization_id=$1 AND e.site_id=$2 AND j.state IN ('queued','running')
			AND (e.publication_id=$3 OR ($4<>'' AND e.connection_id=$5 AND p.target_id=$4 AND p.request->>'resource'=$6)))`,
			org, in.SiteID, in.PublicationID, release.TargetID, in.ConnectionID, release.Resource).Scan(&busy)
		if err != nil {
			return empty, false, err
		}
		if busy {
			return empty, false, domain.ErrExecutionState
		}
	}
	job, err := insertExecutionJob(ctx, tx, org, in, connection.Version, release.Status, available, raw)
	if errors.Is(err, domain.ErrNotFound) {
		old, e := lookup()
		return old, false, e
	}
	if err != nil {
		return empty, false, err
	}
	if in.PublicationID != "" {
		action := in.Operation
		if in.Kind == domain.ExecVerify {
			action = "verify"
		}
		if err = executionEvent(ctx, tx, org, release.ExecutionPublication, action+"_requested", in.Note); err != nil {
			return empty, false, err
		}
	}
	return job, true, tx.Commit()
}

func insertExecutionJob(ctx context.Context, tx *sql.Tx, org string, in domain.ExecutionJobInput, version int64,
	prior string, available *time.Time, raw []byte) (domain.Job, error) {
	job, err := scanJob(tx.QueryRowContext(ctx, `INSERT INTO public.seo_jobs (id,organization_id,site_id,kind,idempotency_key,priority,available_at)
		VALUES ($1,$2,$3,$4,$5,$6,greatest(clock_timestamp(),COALESCE($7,clock_timestamp())))
		ON CONFLICT (organization_id,idempotency_key) DO NOTHING RETURNING `+jobColumns,
		uuid.NewString(), org, in.SiteID, in.Kind, in.IdempotencyKey, in.Priority, available))
	if err != nil {
		return job, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO public.seo_execution_jobs (organization_id,site_id,job_id,connection_id,connection_version,
		publication_id,request,prior_status)
		VALUES ($1,$2,$3,NULLIF($4,'')::uuid,NULLIF($5,0),NULLIF($6,'')::uuid,$7::jsonb,NULLIF($8,''))`,
		org, in.SiteID, job.ID, in.ConnectionID, version, in.PublicationID, string(raw), prior)
	if err != nil {
		return job, err
	}
	return job, auditJob(ctx, tx, "job.enqueue", job)
}

func supersedeExecutionVerifications(ctx context.Context, tx *sql.Tx, org string, in domain.ExecutionJobInput) error {
	rows, err := tx.QueryContext(ctx, `SELECT j.id::text FROM public.seo_jobs j JOIN public.seo_execution_jobs e
		ON (j.organization_id,j.id)=(e.organization_id,e.job_id) WHERE e.organization_id=$1 AND e.site_id=$2
		AND e.publication_id=$3 AND e.connection_id=$4 AND j.kind=$5 AND j.state='queued' FOR UPDATE OF j`,
		org, in.SiteID, in.PublicationID, in.ConnectionID, domain.ExecVerify)
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, id := range ids {
		job, err := scanJob(tx.QueryRowContext(ctx, `SELECT `+jobColumns+` FROM public.seo_jobs WHERE organization_id=$1 AND id=$2`, org, id))
		if err != nil {
			return err
		}
		job, err = setJobState(ctx, tx, job, "cancelled", "superseded_by_rollback", nil, job.AvailableAt, job.Attempts)
		if err != nil {
			return err
		}
		if err := auditJob(ctx, tx, "job.cancel", job); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) ExecutionJob(ctx context.Context, lease domain.Lease) (domain.ExecutionWork, error) {
	var out domain.ExecutionWork
	var request, applied []byte
	err := s.db.QueryRowContext(ctx, `SELECT e.request,COALESCE(e.connection_version,0),COALESCE(e.prior_status,''),e.started_write,e.applied_result
		FROM public.seo_execution_jobs e JOIN public.seo_jobs j ON (j.organization_id,j.id)=(e.organization_id,e.job_id)
		WHERE e.organization_id=$1 AND e.site_id=$2 AND e.job_id=$3 AND j.lease_token=$4 AND j.state='running' AND j.lease_until>clock_timestamp()`,
		lease.Job.OrganizationID, lease.Job.SiteID, lease.Job.ID, lease.Token).
		Scan(&request, &out.ConnectionVersion, &out.PriorStatus, &out.StartedWrite, &applied)
	if errors.Is(err, sql.ErrNoRows) {
		return out, domain.ErrLeaseLost
	}
	if err != nil {
		return out, err
	}
	out.Applied = applied
	return out, json.Unmarshal(request, &out.Input)
}

func saveExecutionReceipt(ctx context.Context, tx *sql.Tx, job domain.Job, value any) (json.RawMessage, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE public.seo_execution_jobs SET applied_result=$3::jsonb
		WHERE organization_id=$1 AND job_id=$2`, job.OrganizationID, job.ID, string(raw))
	if err != nil {
		return nil, err
	}
	if err = audit(ctx, tx, job.OrganizationID, "execution.job.result", job.ID, map[string]string{"kind": job.Kind, "site_id": job.SiteID}); err != nil {
		return nil, err
	}
	return raw, nil
}
