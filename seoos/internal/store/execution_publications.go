package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/Autumn-27/artex/seoos/internal/domain"
	"github.com/google/uuid"
)

const releaseJSON = `r.request || jsonb_build_object(
	'id',r.id,'project_id',r.project_id,'article_version',p.version,'title',r.article->>'title',
	'status',r.status,'target_id',r.target_id,'draft_id',r.draft_id,'url',r.url,'error',r.error_code,
	'scheduled_at',r.scheduled_at,'created_at',r.created_at,'updated_at',r.updated_at,
	'verification_job_id',(SELECT j.id FROM public.seo_execution_jobs e JOIN public.seo_jobs j
		ON (j.organization_id,j.id)=(e.organization_id,e.job_id) WHERE e.organization_id=r.organization_id AND e.site_id=r.site_id
		AND e.publication_id=r.id AND j.kind='cms_publication_verify' AND j.state IN ('queued','running') LIMIT 1))`
const releaseJoin = ` FROM public.seo_cms_publications r JOIN public.seo_content_publications p ON p.id=r.package_id `

const approvedPackage = `SELECT p.project_id::text,p.data FROM public.seo_content_publications p
	JOIN public.seo_content_projects x ON (x.organization_id,x.site_id,x.id)=(p.organization_id,p.site_id,p.project_id)
	WHERE p.organization_id=$1 AND p.site_id=$2 AND p.id=$3 AND x.status='prepared'
	AND (x.data->>'article_version')::integer=p.version AND (x.data->'quality'->>'passed')::boolean
	AND (x.data->'fact_check'->>'passed')::boolean AND (x.data->'review'->>'passed')::boolean FOR SHARE OF x`

func readRelease(ctx context.Context, db rowQueryer, org, id, lock string) (domain.ExecutionRelease, error) {
	var r domain.ExecutionRelease
	var public, article []byte
	if !domain.ValidID(id) {
		return r, domain.ErrNotFound
	}
	err := db.QueryRowContext(ctx, `SELECT `+releaseJSON+`,r.article`+releaseJoin+
		`WHERE r.organization_id=$1 AND r.id=$2 `+lock, org, id).Scan(&public, &article)
	if err != nil {
		return r, classifyError(err)
	}
	if err = json.Unmarshal(public, &r.ExecutionPublication); err != nil {
		return r, err
	}
	return r, json.Unmarshal(article, &r.Article)
}

func (s *Store) CreateExecutionPublication(ctx context.Context, org string, input domain.ExecutionPublicationInput) (domain.ExecutionPublication, bool, error) {
	var empty domain.ExecutionPublication
	if !executionOwner(ctx, org) {
		return empty, false, domain.ErrExecutionAuthority
	}
	if err := input.Validate(); err != nil {
		return empty, false, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return empty, false, err
	}
	defer tx.Rollback()
	c, _, active, err := execConnection(ctx, tx, org, input.SiteID, input.ConnectionID, "FOR SHARE OF c")
	if err != nil {
		return empty, false, err
	}
	if !active || !c.Enabled || !c.AllowWrites || !domain.IsCMSConnection(c.Kind) {
		return empty, false, domain.ErrExecutionAuthority
	}
	request, err := json.Marshal(input)
	if err != nil {
		return empty, false, err
	}
	lookup := func() (domain.ExecutionPublication, error) {
		var existing string
		var same bool
		err := tx.QueryRowContext(ctx, `SELECT id::text, request=$3::jsonb FROM public.seo_cms_publications
			WHERE organization_id=$1 AND idempotency_key=$2`, org, input.IdempotencyKey, string(request)).Scan(&existing, &same)
		if err != nil {
			return empty, classifyError(err)
		}
		if !same {
			return empty, domain.ErrIdempotencyConflict
		}
		p, err := readRelease(ctx, tx, org, existing, "")
		return p.ExecutionPublication, err
	}
	if found, err := lookup(); !errors.Is(err, domain.ErrNotFound) {
		return found, false, err
	}
	var project string
	var raw []byte
	err = tx.QueryRowContext(ctx, approvedPackage, org, input.SiteID, input.PackageID).Scan(&project, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return empty, false, domain.ErrContentGate
	}
	if err != nil {
		return empty, false, err
	}
	var p domain.ContentPublication
	if err = json.Unmarshal(raw, &p); err != nil {
		return empty, false, err
	}
	article, err := json.Marshal(p.Article)
	if err != nil {
		return empty, false, err
	}
	id := uuid.NewString()
	var inserted string
	err = tx.QueryRowContext(ctx, `INSERT INTO public.seo_cms_publications
		(id,organization_id,site_id,connection_id,package_id,project_id,request,article,idempotency_key,status)
		VALUES ($1,$2,$3,$4,$5,$6,$7::jsonb,$8::jsonb,$9,'PENDING') ON CONFLICT DO NOTHING RETURNING id::text`,
		id, org, input.SiteID, input.ConnectionID, input.PackageID, project, string(request), string(article), input.IdempotencyKey).Scan(&inserted)
	if errors.Is(err, sql.ErrNoRows) {
		old, e := lookup()
		if errors.Is(e, domain.ErrNotFound) {
			e = domain.ErrIdempotencyConflict
		}
		return old, false, e
	}
	if err != nil {
		return empty, false, err
	}
	release, err := readRelease(ctx, tx, org, id, "")
	if err != nil {
		return empty, false, err
	}
	if err = executionEvent(ctx, tx, org, release.ExecutionPublication, "create", ""); err != nil {
		return empty, false, err
	}
	return release.ExecutionPublication, true, tx.Commit()
}

func (s *Store) ExecutionPublication(ctx context.Context, org, id string) (domain.ExecutionPublication, []domain.ExecutionPublicationEvent, error) {
	r, err := readRelease(ctx, s.db, org, id, "")
	if err != nil {
		return r.ExecutionPublication, nil, err
	}
	events, err := contentDecodeList[domain.ExecutionPublicationEvent](jsonRows(ctx, s.db, `SELECT jsonb_build_object(
		'status',status,'action',action,'note',note,'created_at',created_at) FROM public.seo_cms_publication_events
		WHERE organization_id=$1 AND site_id=$2 AND publication_id=$3 ORDER BY created_at DESC,id DESC LIMIT 200`, org, r.SiteID, id))
	return r.ExecutionPublication, events, err
}

func executionEvent(ctx context.Context, tx *sql.Tx, org string, r domain.ExecutionPublication, action, note string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO public.seo_cms_publication_events
		(id,organization_id,site_id,publication_id,status,action,note) VALUES ($1,$2,$3,$4,$5,$6,NULLIF($7,''))`,
		uuid.NewString(), org, r.SiteID, r.ID, r.Status, action, note)
	if err != nil {
		return err
	}
	return audit(ctx, tx, org, "execution.publication."+action, r.ID, map[string]string{"site_id": r.SiteID, "status": r.Status, "target_id": r.TargetID})
}

func (s *Store) ExecutionRelease(ctx context.Context, lease domain.Lease, work domain.ExecutionWork) (domain.ExecutionRelease, error) {
	r, err := readRelease(ctx, s.db, lease.Job.OrganizationID, work.Input.PublicationID, "")
	if err != nil {
		return r, err
	}
	if r.ConnectionID != work.Input.ConnectionID || r.SiteID != lease.Job.SiteID {
		return r, domain.ErrNotFound
	}
	for _, version := range []struct {
		operation string
		slot      string
		dest      **domain.CMSPage
	}{{"publish", "before", &r.Before}, {"publish", "after", &r.After}, {"draft", "before", &r.Original}} {
		var data []byte
		err = s.db.QueryRowContext(ctx, `SELECT v.data FROM public.seo_cms_publication_versions v JOIN public.seo_execution_jobs e
			ON (e.organization_id,e.job_id)=(v.organization_id,v.job_id)
			WHERE v.organization_id=$1 AND v.site_id=$2 AND v.publication_id=$3 AND v.slot=$4 AND e.request->>'operation'=$5
			ORDER BY v.created_at DESC LIMIT 1`, lease.Job.OrganizationID, r.SiteID, r.ID, version.slot, version.operation).Scan(&data)
		if errors.Is(err, sql.ErrNoRows) {
			err = nil
			continue
		}
		if err != nil {
			return r, err
		}
		out := &domain.CMSPage{}
		if err = json.Unmarshal(data, out); err != nil {
			return r, err
		}
		*version.dest = out
	}
	return r, err
}

func (s *Store) CheckExecutionPackage(ctx context.Context, org, site, id string) error {
	var project string
	var raw []byte
	err := s.db.QueryRowContext(ctx, approvedPackage, org, site, id).Scan(&project, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ErrContentGate
	}
	return err
}

func (s *Store) BeginPublication(ctx context.Context, lease domain.Lease, work domain.ExecutionWork, stage string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	r, err := lockedExecutionRelease(ctx, tx, lease, work)
	if err != nil {
		return err
	}
	if r.Status == stage {
		return nil
	}
	if err := setReleaseStatus(ctx, tx, lease.Job.OrganizationID, r, stage, "", work.Input.Operation+"_start", ""); err != nil {
		return err
	}
	return tx.Commit()
}

// MarkExecutionWrite commits the intent before the first remote write. A
// recovered worker must reconcile the target and may not blindly send again.
func (s *Store) MarkExecutionWrite(ctx context.Context, lease domain.Lease, work domain.ExecutionWork, before *domain.CMSPage) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	job, err := lockLease(ctx, tx, lease)
	if err != nil {
		return err
	}
	if work.Input.PublicationID != "" {
		r, err := lockedExecutionRelease(ctx, tx, lease, work)
		if err != nil {
			return err
		}
		if work.Input.Operation != "rollback" {
			var project string
			var raw []byte
			err = tx.QueryRowContext(ctx, approvedPackage, job.OrganizationID, r.SiteID, r.PackageID).Scan(&project, &raw)
			if errors.Is(err, sql.ErrNoRows) {
				return domain.ErrContentGate
			}
			if err != nil {
				return err
			}
		}
		target := r.TargetID
		if work.Input.Operation == "draft" {
			target = r.RemoteID
		}
		if target != "" && !restorablePublicationSnapshot(before, target, r.Resource) ||
			target == "" && work.Input.Operation != "draft" {
			return &domain.DataError{Code: "cms_backup_precondition_failed", KnownUnbilled: true}
		}
		if before != nil {
			if err = recordExecutionVersion(ctx, s, tx, job, r.ID, "before", *before); err != nil {
				return err
			}
		}
	}
	res, err := tx.ExecContext(ctx, `UPDATE public.seo_execution_jobs SET started_write=true
		WHERE organization_id=$1 AND job_id=$2 AND NOT started_write`, job.OrganizationID, job.ID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return domain.ErrExecutionState
	}
	if err = audit(ctx, tx, job.OrganizationID, "execution.remote.intent", job.ID, map[string]string{"operation": work.Input.Operation, "connection_id": work.Input.ConnectionID}); err != nil {
		return err
	}
	return tx.Commit()
}

func restorablePublicationSnapshot(page *domain.CMSPage, target, resource string) bool {
	if page == nil || page.ID != target || page.Resource != resource || page.Slug == "" {
		return false
	}
	switch page.Status {
	case "publish", "draft", "pending", "private":
		return true
	case "future":
		return page.ScheduledAt != nil
	default:
		return false
	}
}

func (s *Store) ClearExecutionWrite(ctx context.Context, lease domain.Lease) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	job, err := lockLease(ctx, tx, lease)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE public.seo_execution_jobs SET started_write=false WHERE organization_id=$1 AND job_id=$2`, job.OrganizationID, job.ID)
	if err != nil {
		return err
	}
	if err = audit(ctx, tx, job.OrganizationID, "execution.remote.unbilled", job.ID, nil); err != nil {
		return err
	}
	return tx.Commit()
}

func lockedExecutionRelease(ctx context.Context, tx *sql.Tx, lease domain.Lease, work domain.ExecutionWork) (domain.ExecutionPublication, error) {
	job, err := lockLease(ctx, tx, lease)
	if err != nil {
		return domain.ExecutionPublication{}, err
	}
	r, err := readRelease(ctx, tx, job.OrganizationID, work.Input.PublicationID, "FOR NO KEY UPDATE OF r")
	if err != nil {
		return r.ExecutionPublication, err
	}
	if r.SiteID != job.SiteID || r.ConnectionID != work.Input.ConnectionID {
		return r.ExecutionPublication, domain.ErrNotFound
	}
	var bound bool
	err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM public.seo_execution_jobs WHERE organization_id=$1 AND site_id=$2
		AND job_id=$3 AND publication_id=$4 AND connection_id=$5)`, job.OrganizationID, job.SiteID, job.ID, r.ID, r.ConnectionID).Scan(&bound)
	if err == nil && !bound {
		err = domain.ErrNotFound
	}
	return r.ExecutionPublication, err
}

func setReleaseStatus(ctx context.Context, tx *sql.Tx, org string, r domain.ExecutionPublication, status, reason, action, note string) error {
	r.Status, r.Error = status, reason
	_, err := tx.ExecContext(ctx, `UPDATE public.seo_cms_publications SET status=$4,error_code=$5,
		target_id=$6,draft_id=$7,url=$8,scheduled_at=$9,updated_at=clock_timestamp()
		WHERE organization_id=$1 AND site_id=$2 AND id=$3`, org, r.SiteID, r.ID, status, reason,
		r.TargetID, r.DraftID, r.URL, r.ScheduledAt)
	if err != nil {
		return err
	}
	return executionEvent(ctx, tx, org, r, action, note)
}

func recordExecutionVersion(ctx context.Context, s *Store, tx *sql.Tx, job domain.Job, pub, slot string, page domain.CMSPage) error {
	raw, err := json.Marshal(page)
	if err != nil {
		return err
	}
	var encrypted []byte
	if len(page.Raw) > 0 && len(page.Raw) <= domain.MaxProviderRawBytes && json.Valid(page.Raw) {
		if s.providerCipher == nil {
			return domain.ErrProviderNotConfigured
		}
		encrypted = s.providerCipher.Seal(nil, nil, page.Raw, []byte("seoos/cms-receipt/"+job.OrganizationID+"/"+job.ID+"/"+slot))
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO public.seo_cms_publication_versions
		(organization_id,site_id,publication_id,job_id,slot,data,raw_encrypted) VALUES ($1,$2,$3,$4,$5,$6::jsonb,$7)
		ON CONFLICT (organization_id,job_id,slot) DO NOTHING`, job.OrganizationID, job.SiteID, pub, job.ID, slot, string(raw), encrypted)
	return err
}

func (s *Store) CompletePublication(ctx context.Context, lease domain.Lease, work domain.ExecutionWork, status, reason string,
	after, public *domain.CMSPage) (json.RawMessage, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	r, err := lockedExecutionRelease(ctx, tx, lease, work)
	if err != nil {
		return nil, err
	}
	if after != nil {
		if err = recordExecutionVersion(ctx, s, tx, lease.Job, r.ID, "after", *after); err != nil {
			return nil, err
		}
		switch work.Input.Operation {
		case "draft":
			if status == domain.PublicationDraft {
				r.DraftID = after.ID
				r.TargetID = r.RemoteID
				if r.TargetID == "" {
					r.TargetID = after.ID
				}
				r.URL = after.URL
			}
		case "publish", "rollback":
			if after.ID == r.TargetID {
				r.URL = after.URL
			}
		default:
			if work.Input.Kind == domain.ExecVerify && after.URL != "" {
				r.URL = after.URL
			}
		}
	}
	if status == domain.PublicationScheduled {
		r.ScheduledAt = work.Input.ScheduledAt
	} else if status == domain.PublicationRolledBack {
		r.ScheduledAt = nil
	}
	if public != nil {
		if err = recordExecutionVersion(ctx, s, tx, lease.Job, r.ID, "public", *public); err != nil {
			return nil, err
		}
	}
	if err = setReleaseStatus(ctx, tx, lease.Job.OrganizationID, r, status, reason, "result", ""); err != nil {
		return nil, err
	}
	receipt := map[string]string{"publication_id": r.ID, "status": status, "target_id": r.TargetID, "url": r.URL, "error": reason}
	if work.Input.Operation == "publish" && (status == domain.PublicationPublished || status == domain.PublicationScheduled) {
		in := domain.ExecutionJobInput{Kind: domain.ExecVerify, SiteID: r.SiteID, ConnectionID: r.ConnectionID,
			PublicationID: r.ID, IdempotencyKey: "artex-auto-verify-" + uuid.NewString(), Priority: work.Input.Priority}
		raw, err := json.Marshal(in)
		if err != nil {
			return nil, err
		}
		job, err := insertExecutionJob(ctx, tx, lease.Job.OrganizationID, in, work.ConnectionVersion, status, r.ScheduledAt, raw)
		if err != nil {
			return nil, err
		}
		r.Status = status
		if err = executionEvent(ctx, tx, lease.Job.OrganizationID, r, "verify_queued", ""); err != nil {
			return nil, err
		}
		receipt["verification_job_id"] = job.ID
	}
	value, err := saveExecutionReceipt(ctx, tx, lease.Job, receipt)
	if err != nil {
		return nil, err
	}
	return value, tx.Commit()
}

func (s *Store) PublicationRetry(ctx context.Context, lease domain.Lease, work domain.ExecutionWork, status, reason string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	r, err := lockedExecutionRelease(ctx, tx, lease, work)
	if err != nil {
		return err
	}
	if err = setReleaseStatus(ctx, tx, lease.Job.OrganizationID, r, status, reason, "wait", ""); err != nil {
		return err
	}
	return tx.Commit()
}

func executionPublicationAllowed(status string, in domain.ExecutionJobInput) bool {
	if in.Kind == domain.ExecVerify {
		return status == domain.PublicationPublished || status == domain.PublicationVerified || status == domain.PublicationScheduled || status == domain.PublicationRollback
	}
	switch in.Operation {
	case "draft":
		return status == domain.PublicationPending || status == domain.PublicationFailed || status == domain.PublicationCancelled
	case "publish":
		return status == domain.PublicationDraft
	case "rollback":
		return status == domain.PublicationPublished || status == domain.PublicationVerified || status == domain.PublicationScheduled || status == domain.PublicationRollback
	}
	return false
}

func executionAbandoned(ctx context.Context, tx *sql.Tx, job domain.Job, reason, action string) error {
	if job.Kind == domain.ExecGSCSync {
		return abandonGSCDays(ctx, tx, job, reason)
	}
	if !domain.IsExecutionKind(job.Kind) {
		return nil
	}
	var p, prior sql.NullString
	var started, settled bool
	err := tx.QueryRowContext(ctx, `SELECT publication_id::text, prior_status, started_write, applied_result IS NOT NULL FROM public.seo_execution_jobs
		WHERE organization_id=$1 AND job_id=$2`, job.OrganizationID, job.ID).Scan(&p, &prior, &started, &settled)
	if errors.Is(err, sql.ErrNoRows) || err == nil && !p.Valid {
		return nil
	}
	if err != nil {
		return err
	}
	if settled {
		return nil
	}
	r, err := readRelease(ctx, tx, job.OrganizationID, p.String, "FOR NO KEY UPDATE OF r")
	if err != nil {
		return err
	}
	status := domain.PublicationCancelled
	if action != "cancel" {
		status = domain.PublicationFailed
	}
	if prior.Valid && prior.String != domain.PublicationPending && prior.String != domain.PublicationFailed && prior.String != domain.PublicationCancelled {
		status = prior.String
	}
	if started {
		status = domain.PublicationRollback
	}
	return setReleaseStatus(ctx, tx, job.OrganizationID, r.ExecutionPublication, status, reason, action, "")
}
