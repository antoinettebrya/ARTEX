package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/Autumn-27/artex/seoos/internal/domain"
)

func (s *Store) EnqueueContent(ctx context.Context, org string, input domain.ContentJobInput) (domain.Job, bool, error) {
	in, err := input.Normalize()
	if err != nil {
		return domain.Job{}, false, err
	}
	job := domain.JobInput{SiteID: in.SiteID, Kind: in.Kind, ProviderID: in.ProviderID, IdempotencyKey: in.IdempotencyKey}
	return s.enqueueJob(ctx, org, job, nil, &in)
}

func createContentJob(ctx context.Context, tx *sql.Tx, job domain.Job, input domain.ContentJobInput, payload []byte) error {
	if input.ProjectID != "" {
		var exists bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM public.seo_content_projects
			WHERE organization_id=$1 AND site_id=$2 AND id=$3)`, job.OrganizationID, job.SiteID, input.ProjectID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return domain.ErrNotFound
		}
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO public.seo_content_jobs (organization_id,site_id,job_id,project_id,request)
		VALUES ($1,$2,$3,NULLIF($4,'')::uuid,$5::jsonb)`, job.OrganizationID, job.SiteID, job.ID, input.ProjectID, string(payload))
	return err
}

func (s *Store) ContentJob(ctx context.Context, lease domain.Lease) (domain.ContentJobInput, bool, error) {
	var input domain.ContentJobInput
	var raw []byte
	err := s.db.QueryRowContext(ctx, `SELECT c.request FROM public.seo_jobs j JOIN public.seo_content_jobs c
		ON c.organization_id=j.organization_id AND c.job_id=j.id
		WHERE j.organization_id=$1 AND j.id=$2 AND j.lease_token=$3 AND j.state='running' AND j.lease_until > clock_timestamp()`,
		lease.Job.OrganizationID, lease.Job.ID, lease.Token).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return input, false, nil
	}
	if err != nil {
		return input, false, err
	}
	return input, true, json.Unmarshal(raw, &input)
}

func (s *Store) ContentJobs(ctx context.Context, org, site, project string, limit, offset int) ([]domain.ContentJobRecord, error) {
	if !domain.ValidID(site) || project != "" && !domain.ValidID(project) {
		return nil, domain.ErrNotFound
	}
	if _, err := s.GetSite(ctx, org, site); err != nil {
		return nil, err
	}
	if err := domain.ValidatePage(limit, offset); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+jobColumns+`,
		(SELECT c.request FROM public.seo_content_jobs c WHERE c.organization_id=j.organization_id AND c.job_id=j.id)
		FROM public.seo_jobs j WHERE j.organization_id=$1 AND j.site_id=$2
		AND EXISTS (SELECT 1 FROM public.seo_content_jobs c WHERE c.organization_id=j.organization_id AND c.job_id=j.id
			AND (NULLIF($3,'') IS NULL OR c.project_id=NULLIF($3,'')::uuid))
		ORDER BY created_at DESC,id DESC LIMIT $4 OFFSET $5`, org, site, project, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []domain.ContentJobRecord{}
	for rows.Next() {
		var request []byte
		job, err := scanJob(rows, &request)
		if err != nil {
			return nil, err
		}
		var input domain.ContentJobInput
		if err := json.Unmarshal(request, &input); err != nil {
			return nil, err
		}
		result = append(result, domain.ContentJobRecord{Job: job, Request: input})
	}
	return result, rows.Err()
}

// An active tenant/site lease can write only the project in the original request.
func contentJobProject(ctx context.Context, tx *sql.Tx, job domain.Job, project string) error {
	if !domain.ValidID(project) {
		return domain.ErrNotFound
	}
	var ok bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM public.seo_content_jobs
		WHERE organization_id=$1 AND site_id=$2 AND job_id=$3 AND project_id=$4)`,
		job.OrganizationID, job.SiteID, job.ID, project).Scan(&ok)
	if err == nil && !ok {
		return domain.ErrNotFound
	}
	return err
}
