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

const jobColumns = `id::text, organization_id::text, site_id::text, COALESCE(provider_id::text, ''),
	kind, idempotency_key, state, attempts, available_at, COALESCE(error_code, ''),
	COALESCE(result, 'null'::jsonb), created_at, updated_at`

func scanJob(row rowScanner, tail ...any) (domain.Job, error) {
	var job domain.Job
	dest := []any{&job.ID, &job.OrganizationID, &job.SiteID, &job.ProviderID,
		&job.Kind, &job.IdempotencyKey, &job.State, &job.Attempts, &job.AvailableAt,
		&job.ErrorCode, &job.Result, &job.CreatedAt, &job.UpdatedAt}
	err := row.Scan(append(dest, tail...)...)
	job.AvailableAt = job.AvailableAt.UTC()
	job.CreatedAt = job.CreatedAt.UTC()
	job.UpdatedAt = job.UpdatedAt.UTC()
	if string(job.Result) == "null" {
		job.Result = nil
	}
	return job, classifyError(err)
}

func (s *Store) EnqueueJob(ctx context.Context, orgID string, input domain.JobInput) (domain.Job, bool, error) {
	if err := input.Validate(); err != nil {
		return domain.Job{}, false, err
	}
	return s.enqueueJob(ctx, orgID, input, nil, nil)
}

func (s *Store) enqueueJob(ctx context.Context, orgID string, input domain.JobInput, research *domain.ResearchInput, content *domain.ContentJobInput) (domain.Job, bool, error) {
	if err := validateOrganizationID(orgID); err != nil {
		return domain.Job{}, false, err
	}
	var priority int
	var request []byte
	if research != nil || content != nil {
		payload := any(research)
		if research != nil {
			priority = research.Priority
		} else {
			priority, payload = content.Priority, content
		}
		var err error
		request, err = json.Marshal(payload)
		if err != nil {
			return domain.Job{}, false, err
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Job{}, false, err
	}
	defer tx.Rollback()
	getByKey := func() (domain.Job, error) {
		job, err := scanJob(tx.QueryRowContext(ctx, `SELECT `+jobColumns+`
			FROM public.seo_jobs WHERE organization_id = $1 AND idempotency_key = $2`, orgID, input.IdempotencyKey))
		if err != nil {
			return job, err
		}
		if job.JobInput != input {
			return domain.Job{}, domain.ErrIdempotencyConflict
		}
		if research != nil {
			var same bool
			err = tx.QueryRowContext(ctx, `SELECT request = $3::jsonb FROM public.seo_research_jobs
				WHERE organization_id = $1 AND job_id = $2`, orgID, job.ID, string(request)).Scan(&same)
			if err != nil {
				return domain.Job{}, err
			}
			if !same {
				return domain.Job{}, domain.ErrIdempotencyConflict
			}
		}
		if content != nil || input.Kind == domain.ContentInventory {
			var same bool
			err = tx.QueryRowContext(ctx, `SELECT COALESCE((SELECT CASE WHEN $4 THEN false ELSE request=NULLIF($3,'')::jsonb END
				FROM public.seo_content_jobs WHERE organization_id=$1 AND job_id=$2),$4)`, orgID, job.ID, string(request), content == nil).Scan(&same)
			if err != nil {
				return domain.Job{}, err
			}
			if !same {
				return domain.Job{}, domain.ErrIdempotencyConflict
			}
		}
		return job, nil
	}
	if job, err := getByKey(); !errors.Is(err, domain.ErrNotFound) {
		return job, false, err
	}
	var status string
	if err := tx.QueryRowContext(ctx, `SELECT status FROM public.seo_sites
		WHERE organization_id = $1 AND id = $2`, orgID, input.SiteID).Scan(&status); err != nil {
		return domain.Job{}, false, classifyError(err)
	}
	if status != "active" {
		return domain.Job{}, false, domain.ErrSiteInactive
	}
	if input.ProviderID != "" {
		var enabled bool
		var kind string
		if err := tx.QueryRowContext(ctx, `SELECT enabled, kind FROM public.seo_providers
			WHERE organization_id = $1 AND id = $2`, orgID, input.ProviderID).Scan(&enabled, &kind); err != nil {
			return domain.Job{}, false, classifyError(err)
		}
		data := domain.UsesDataProvider(input.Kind)
		if data && kind != domain.ProviderDataForSEO || !data && !domain.IsModelProvider(kind) {
			return domain.Job{}, false, &domain.ValidationError{Field: "provider_id", Message: "provider type does not serve this job"}
		}
		if !enabled {
			return domain.Job{}, false, domain.ErrProviderDisabled
		}
	}
	job, err := scanJob(tx.QueryRowContext(ctx, `
		INSERT INTO public.seo_jobs (id, organization_id, site_id, provider_id, kind, idempotency_key, priority)
		VALUES ($1, $2, $3, NULLIF($4, '')::uuid, $5, $6, $7)
		ON CONFLICT (organization_id, idempotency_key) DO NOTHING
		RETURNING `+jobColumns, uuid.NewString(), orgID, input.SiteID, input.ProviderID, input.Kind, input.IdempotencyKey, priority))
	if errors.Is(err, domain.ErrNotFound) {
		job, err := getByKey()
		return job, false, err
	}
	if err != nil {
		return domain.Job{}, false, err
	}
	if research != nil {
		if err := createResearchJob(ctx, tx, job, *research, request); err != nil {
			return domain.Job{}, false, err
		}
	}
	if content != nil {
		if err := createContentJob(ctx, tx, job, *content, request); err != nil {
			return domain.Job{}, false, err
		}
	}
	if err := auditJob(ctx, tx, "job.enqueue", job); err != nil {
		return domain.Job{}, false, err
	}
	return job, true, tx.Commit()
}

func (s *Store) GetJob(ctx context.Context, orgID, jobID string) (domain.Job, error) {
	if err := validateOrganizationID(orgID); err != nil {
		return domain.Job{}, err
	}
	if !domain.ValidID(jobID) {
		return domain.Job{}, domain.ErrNotFound
	}
	job, err := scanJob(s.db.QueryRowContext(ctx, `SELECT `+jobColumns+`
		FROM public.seo_jobs WHERE organization_id = $1 AND id = $2`, orgID, jobID))
	if err == nil && job.Kind == domain.ExecGSCSync {
		var progress domain.GSCProgress
		progress, err = s.GSCProgress(ctx, orgID, jobID)
		if err == nil && progress.From != "" {
			job.GSCProgress = &progress
		}
	}
	return job, err
}

func (s *Store) ListJobs(ctx context.Context, orgID string, limit, offset int, filters ...domain.JobFilter) ([]domain.Job, error) {
	if err := validateOrganizationID(orgID); err != nil {
		return nil, err
	}
	if err := domain.ValidatePage(limit, offset); err != nil {
		return nil, err
	}
	var f domain.JobFilter
	if len(filters) > 0 {
		f = filters[0]
	}
	if err := f.Validate(); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+jobColumns+` FROM public.seo_jobs j
		WHERE organization_id=$1 AND ($4='' OR site_id=NULLIF($4,'')::uuid)
		AND ($5='' OR state=$5) AND ($6='' OR kind=$6)
		AND ($7='' OR (created_at >= (NULLIF($7,'')::date::timestamp AT TIME ZONE 'UTC')
		  AND created_at < ((NULLIF($7,'')::date + 1)::timestamp AT TIME ZONE 'UTC')))
		AND (($8='' AND $9='') OR EXISTS (SELECT 1 FROM public.seo_sites s
		  WHERE (s.organization_id,s.id)=(j.organization_id,j.site_id)
		  AND ($8='' OR s.country_code=$8) AND ($9='' OR s.language_tag=$9)))
		ORDER BY created_at DESC,id DESC LIMIT $2 OFFSET $3`, orgID, limit, offset,
		f.SiteID, f.State, f.Kind, f.Day, f.Country, f.Language)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	jobs := []domain.Job{}
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

func (s *Store) CancelJob(ctx context.Context, orgID, jobID string) (domain.Job, error) {
	if err := validateOrganizationID(orgID); err != nil {
		return domain.Job{}, err
	}
	if !domain.ValidID(jobID) {
		return domain.Job{}, domain.ErrNotFound
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Job{}, err
	}
	defer tx.Rollback()
	job, err := scanJob(tx.QueryRowContext(ctx, `SELECT `+jobColumns+` FROM public.seo_jobs
		WHERE organization_id = $1 AND id = $2 FOR NO KEY UPDATE`, orgID, jobID))
	if err != nil {
		return domain.Job{}, err
	}
	switch job.State {
	case "cancelled":
		return job, nil
	case "queued", "running":
	default:
		return domain.Job{}, domain.ErrJobSettled
	}
	job, err = setJobState(ctx, tx, job, "cancelled", "", nil, job.AvailableAt, job.Attempts)
	if err != nil {
		return domain.Job{}, err
	}
	if err := executionAbandoned(ctx, tx, job, "job_cancelled", "cancel"); err != nil {
		return domain.Job{}, err
	}
	if err := auditJob(ctx, tx, "job.cancel", job); err != nil {
		return domain.Job{}, err
	}
	return job, tx.Commit()
}

// ClaimJob grants one 90-second lease. An expired final attempt is settled
// here as well, so repeated worker restarts cannot leave it running forever.
func (s *Store) ClaimJob(ctx context.Context) (*domain.Lease, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	job, err := scanJob(tx.QueryRowContext(ctx, `SELECT `+jobColumns+` FROM public.seo_jobs
		WHERE (state = 'queued' AND available_at <= now())
		   OR (state = 'running' AND lease_until <= now())
		ORDER BY priority DESC, available_at, id LIMIT 1 FOR UPDATE SKIP LOCKED`))
	if errors.Is(err, domain.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if job.Attempts >= domain.MaxJobAttempts {
		job, err = setJobState(ctx, tx, job, "failed", "lease_expired", nil, job.AvailableAt, job.Attempts)
		if err != nil {
			return nil, err
		}
		if err := executionAbandoned(ctx, tx, job, "lease_expired", "lease_expired"); err != nil {
			return nil, err
		}
		if err := auditJob(ctx, tx, "job.fail", job); err != nil {
			return nil, err
		}
		return nil, tx.Commit()
	}
	token := uuid.NewString()
	action := "job.claim"
	if job.State == "running" {
		action = "job.recover"
	}
	job, err = scanJob(tx.QueryRowContext(ctx, `UPDATE public.seo_jobs
		SET state = 'running', lease_token = $3, lease_until = clock_timestamp() +
			CASE WHEN kind='content_draft' THEN interval '150 seconds' ELSE interval '90 seconds' END,
			attempts = attempts + 1, updated_at = clock_timestamp()
		WHERE organization_id = $1 AND id = $2 RETURNING `+jobColumns,
		job.OrganizationID, job.ID, token))
	if err != nil {
		return nil, err
	}
	if err := auditJob(ctx, tx, action, job); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &domain.Lease{Job: job, Token: token}, nil
}

func lockLease(ctx context.Context, tx *sql.Tx, lease domain.Lease) (domain.Job, error) {
	job, err := scanJob(tx.QueryRowContext(ctx, `SELECT `+jobColumns+` FROM public.seo_jobs
		WHERE organization_id = $1 AND id = $2 AND lease_token = $3 AND state = 'running'
		  AND lease_until > clock_timestamp() FOR NO KEY UPDATE`,
		lease.Job.OrganizationID, lease.Job.ID, lease.Token))
	if errors.Is(err, domain.ErrNotFound) {
		return domain.Job{}, domain.ErrLeaseLost
	}
	return job, err
}

func (s *Store) FinishJob(ctx context.Context, lease domain.Lease, outcome domain.JobOutcome) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	job, err := lockLease(ctx, tx, lease)
	if err != nil {
		return err
	}
	state, action := "failed", "job.fail"
	runAt, attempts := job.AvailableAt, job.Attempts
	switch {
	case !outcome.DeferUntil.IsZero():
		state, action, runAt = "queued", "job.defer", outcome.DeferUntil
		attempts--
	case outcome.Code == "" && json.Valid(outcome.Result):
		state, action = "succeeded", "job.succeed"
	case outcome.Retry && attempts < domain.MaxJobAttempts:
		state, action = "queued", "job.retry"
		delay := time.Duration(attempts*attempts) * 5 * time.Second
		if err := tx.QueryRowContext(ctx, `SELECT clock_timestamp() + ($1::int * interval '1 second')`, int(delay/time.Second)).Scan(&runAt); err != nil {
			return err
		}
		if outcome.RetryAfter.After(runAt) {
			runAt = outcome.RetryAfter
		}
	case outcome.Code == "":
		return errors.New("invalid job result")
	}
	job, err = setJobState(ctx, tx, job, state, outcome.Code, outcome.Result, runAt, attempts)
	if err != nil {
		return err
	}
	if state == "failed" {
		if err := executionAbandoned(ctx, tx, job, outcome.Code, "failure"); err != nil {
			return err
		}
	}
	if err := auditJob(ctx, tx, action, job); err != nil {
		return err
	}
	return tx.Commit()
}

func setJobState(ctx context.Context, tx *sql.Tx, job domain.Job, state, code string, result json.RawMessage, runAt time.Time, attempts int) (domain.Job, error) {
	return scanJob(tx.QueryRowContext(ctx, `UPDATE public.seo_jobs
		SET state = $3, error_code = NULLIF($4, ''), result = NULLIF($5, '')::jsonb,
			available_at = $6, attempts = $7, lease_token = NULL, lease_until = NULL, updated_at = clock_timestamp()
		WHERE organization_id = $1 AND id = $2 RETURNING `+jobColumns,
		job.OrganizationID, job.ID, state, code, string(result), runAt, attempts))
}

func auditJob(ctx context.Context, tx *sql.Tx, action string, job domain.Job) error {
	return audit(ctx, tx, job.OrganizationID, action, job.ID, map[string]any{
		"site_id": job.SiteID, "kind": job.Kind, "provider_id": job.ProviderID,
		"state": job.State, "attempts": job.Attempts, "error_code": job.ErrorCode, "available_at": job.AvailableAt,
	})
}
