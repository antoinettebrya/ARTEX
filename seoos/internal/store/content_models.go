package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/Autumn-27/artex/seoos/internal/contentmodel"
	"github.com/Autumn-27/artex/seoos/internal/domain"
	"github.com/google/uuid"
)

func (s *Store) ContentModels(ctx context.Context, org string) ([]domain.ContentModelProfile, error) {
	return contentDecodeList[domain.ContentModelProfile](jsonRows(ctx, s.db, `SELECT data FROM public.seo_content_model_profiles WHERE organization_id=$1 ORDER BY provider_id`, org))
}

func (s *Store) SaveContentModel(ctx context.Context, org string, p domain.ContentModelProfile) (domain.ContentModelProfile, error) {
	if err := p.Validate(); err != nil {
		return p, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return p, err
	}
	defer tx.Rollback()
	var kind string
	if err = tx.QueryRowContext(ctx, `SELECT kind FROM public.seo_providers WHERE organization_id=$1 AND id=$2 FOR NO KEY UPDATE`,
		org, p.ProviderID).Scan(&kind); err != nil {
		return p, classifyError(err)
	}
	if !domain.IsModelProvider(kind) {
		return p, domain.ErrContentPrerequisite
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return p, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO public.seo_content_model_profiles (organization_id,provider_id,data)
		VALUES ($1,$2,$3::jsonb) ON CONFLICT (organization_id,provider_id) DO UPDATE SET data=EXCLUDED.data,updated_at=clock_timestamp()`,
		org, p.ProviderID, string(raw)); err != nil {
		return p, err
	}
	if err = audit(ctx, tx, org, "content.model.profile", p.ProviderID, map[string]any{
		"role": p.Role, "daily_tokens": p.DailyTokens, "daily_cost_micros": p.DailyCostMicros,
		"input_micros_per_million": p.InputMicrosPerMillion, "output_micros_per_million": p.OutputMicrosPerMillion,
		"cached_input_micros_per_million": p.CachedInputMicrosPerMillion,
	}); err != nil {
		return p, err
	}
	return p, tx.Commit()
}

func modelCharge(p domain.ContentModelProfile, in, out int64) int64 {
	return (in*p.InputMicrosPerMillion + out*p.OutputMicrosPerMillion + 999999) / 1000000
}

func measuredModelCharge(p domain.ContentModelProfile, in, out int64, cached *int64) int64 {
	if cached == nil || p.CachedInputMicrosPerMillion == nil {
		return modelCharge(p, in, out)
	}
	hit := max(0, min(in, *cached))
	cachedRate := *p.CachedInputMicrosPerMillion
	return ((in-hit)*p.InputMicrosPerMillion + hit*cachedRate + out*p.OutputMicrosPerMillion + 999999) / 1000000
}

func (s *Store) ReserveContentModel(ctx context.Context, lease domain.Lease, step string, req domain.ContentModelRequest,
	project string) (domain.ContentModelReservation, error) {
	var res domain.ContentModelReservation
	if s.providerCipher == nil {
		return res, domain.ErrProviderNotConfigured
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return res, err
	}
	defer tx.Rollback()
	job, err := lockLease(ctx, tx, lease)
	if err != nil {
		return res, err
	}
	if !domain.UsesContentModel(job.Kind) {
		return res, domain.ErrContentPrerequisite
	}
	if err = contentJobProject(ctx, tx, job, project); err != nil {
		return res, err
	}
	var encrypted, profile []byte
	res.Provider, err = scanProvider(tx.QueryRowContext(ctx, `SELECT `+providerColumns+`,credential FROM public.seo_providers
		WHERE organization_id=$1 AND id=$2 FOR NO KEY UPDATE`, job.OrganizationID, job.ProviderID), &encrypted)
	if err != nil {
		return res, err
	}
	p := res.Provider
	if !p.Enabled || !domain.IsModelProvider(p.Kind) {
		return res, domain.ErrProviderDisabled
	}
	err = tx.QueryRowContext(ctx, `SELECT data FROM public.seo_content_model_profiles WHERE organization_id=$1 AND provider_id=$2`,
		job.OrganizationID, p.ID).Scan(&profile)
	if errors.Is(err, sql.ErrNoRows) {
		return res, &domain.DataError{Code: "content_model_profile_required"}
	}
	if err != nil {
		return res, err
	}
	if err = json.Unmarshal(profile, &res.Profile); err != nil {
		return res, err
	}
	settings := res.Profile
	if job.Kind == domain.ContentReview {
		var writerID, writer string
		err = tx.QueryRowContext(ctx, `SELECT COALESCE(data->>'last_writer_id',''),COALESCE(data->>'last_writer','')
			FROM public.seo_content_projects WHERE organization_id=$1 AND site_id=$2 AND id=$3`,
			job.OrganizationID, job.SiteID, project).Scan(&writerID, &writer)
		if err != nil {
			return res, err
		}
		if settings.Role == "writer" || writerID == p.ID || writer == p.Kind+":"+p.Model {
			return res, &domain.DataError{Code: "content_independent_reviewer_required"}
		}
	} else if settings.Role == "reviewer" {
		return res, &domain.DataError{Code: "content_writer_required"}
	}
	material, err := json.Marshal(struct {
		Request domain.ContentModelRequest
		Version int64
		Profile domain.ContentModelProfile
	}{req, p.Version, settings})
	if err != nil {
		return res, err
	}
	fingerprint := sha256.Sum256(material)
	var data []byte
	err = tx.QueryRowContext(ctx, `SELECT id::text,data FROM public.seo_content_model_calls
		WHERE organization_id=$1 AND job_id=$2 AND step=$3 AND fingerprint=$4 AND state='succeeded'
		ORDER BY reserved_at DESC LIMIT 1`, job.OrganizationID, job.ID, step, fingerprint[:]).Scan(&res.ID, &data)
	if err == nil {
		var cached domain.ContentModelResponse
		if err = json.Unmarshal(data, &cached); err != nil {
			return res, err
		}
		res.Cached = &cached
		return res, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return res, err
	}
	var pending int
	err = tx.QueryRowContext(ctx, `SELECT count(*) FROM public.seo_content_model_calls
		WHERE organization_id=$1 AND job_id=$2 AND step=$3 AND state IN ('reserved','uncertain')`, job.OrganizationID, job.ID, step).Scan(&pending)
	if err != nil {
		return res, err
	}
	if pending > 0 {
		if _, err = tx.ExecContext(ctx, `UPDATE public.seo_content_model_calls SET state='uncertain',error_code='missing_receipt',settled_at=clock_timestamp()
			WHERE organization_id=$1 AND job_id=$2 AND step=$3 AND state='reserved'`, job.OrganizationID, job.ID, step); err != nil {
			return res, err
		}
		if err = audit(ctx, tx, job.OrganizationID, "content.model.unknown_receipt", job.ID, map[string]string{"step": step}); err != nil {
			return res, err
		}
		if err = tx.Commit(); err != nil {
			return res, err
		}
		return res, &domain.DataError{Code: "content_model_receipt_unknown"}
	}
	estimatedInput, err := contentmodel.EstimateInputTokens(req, p, settings)
	if err != nil {
		return res, &domain.DataError{Code: "content_model_invalid_request", KnownUnbilled: true}
	}
	if estimatedInput > int64(settings.MaxInputTokens) {
		return res, &domain.DataError{Code: "content_model_input_budget"}
	}
	output := int64(p.MaxOutputTokens)
	tokens, price := estimatedInput+output, modelCharge(settings, estimatedInput, output)
	if tokens > settings.DailyTokens || price > settings.DailyCostMicros {
		return res, &domain.DataError{Code: "content_model_budget_too_low"}
	}
	var now time.Time
	if err = tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return res, err
	}
	day := now.UTC().Truncate(24 * time.Hour)
	minute := now.Add(-time.Minute)
	var usedTokens, usedCost int64
	if err = tx.QueryRowContext(ctx, modelDailySpend, job.OrganizationID, p.ID, day).Scan(&usedTokens, &usedCost); err != nil {
		return res, err
	}
	var daily, recent int
	var oldest sql.NullTime
	err = tx.QueryRowContext(ctx, modelRequests, job.OrganizationID, p.ID, day, minute).Scan(&daily, &recent, &oldest)
	if err != nil {
		return res, err
	}
	if daily >= p.RequestsPerDay || usedTokens > settings.DailyTokens-tokens || usedCost > settings.DailyCostMicros-price {
		res.DeferUntil = day.Add(24 * time.Hour)
		return res, nil
	}
	if recent >= p.RequestsPerMinute {
		res.DeferUntil = oldest.Time.Add(time.Minute)
		return res, nil
	}
	secret, err := s.providerCipher.Open(nil, nil, encrypted, providerBinding(p.Kind, job.OrganizationID, p.ID))
	if err != nil {
		return res, domain.ErrProviderNotConfigured
	}
	res.ID, res.APIKey = uuid.NewString(), string(secret)
	_, err = tx.ExecContext(ctx, `INSERT INTO public.seo_content_model_calls
		(id,organization_id,provider_id,job_id,lease_token,step,fingerprint,state,reserved_tokens,reserved_micros)
		VALUES ($1,$2,$3,$4,$5,$6,$7,'reserved',$8,$9)`,
		res.ID, job.OrganizationID, p.ID, job.ID, lease.Token, step, fingerprint[:], tokens, price)
	if err != nil {
		return domain.ContentModelReservation{}, err
	}
	if err = audit(ctx, tx, job.OrganizationID, "content.model.reserve", res.ID, map[string]any{"job_id": job.ID, "step": step, "tokens": tokens, "micros": price}); err != nil {
		return domain.ContentModelReservation{}, err
	}
	if err = tx.Commit(); err != nil {
		return domain.ContentModelReservation{}, err
	}
	return res, nil
}

// Keep the existing Provider request limits shared with site briefs and Phase 2 intent.
const modelRequests = `SELECT count(*) FILTER (WHERE reserved_at >= $3),
	count(*) FILTER (WHERE reserved_at > $4), min(reserved_at) FILTER (WHERE reserved_at > $4) FROM (
		SELECT reserved_at FROM public.seo_provider_calls WHERE organization_id=$1 AND provider_id=$2
			AND reserved_at >= least($3::timestamptz,$4::timestamptz)
		UNION ALL SELECT reserved_at FROM public.seo_content_model_calls WHERE organization_id=$1 AND provider_id=$2
			AND reserved_at >= least($3::timestamptz,$4::timestamptz)
	) requests`

const modelDailySpend = `SELECT COALESCE(sum(tokens),0)::bigint,COALESCE(sum(micros),0)::bigint FROM (
	SELECT COALESCE(tokens,reserved_tokens) tokens,COALESCE(cost_micros,reserved_micros) micros
	FROM public.seo_content_model_calls WHERE organization_id=$1 AND provider_id=$2 AND reserved_at >= $3
	UNION ALL
	SELECT CASE WHEN diagnostic->>'cost_source'='unbilled' THEN 0 ELSE
		COALESCE((diagnostic->>'input_tokens')::bigint+(diagnostic->>'output_tokens')::bigint,
			(diagnostic->>'reserved_tokens')::bigint,0) END,
		COALESCE((diagnostic->>'cost_micros')::bigint,(diagnostic->>'reserved_micros')::bigint,0)
	FROM public.seo_provider_calls WHERE organization_id=$1 AND provider_id=$2 AND reserved_at >= $3
) spend`

func (s *Store) SettleContentModel(ctx context.Context, lease domain.Lease, res domain.ContentModelReservation,
	response domain.ContentModelResponse, problem *domain.DataError) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	state, code := "succeeded", ""
	var total, price *int64
	diagnostic := response.Diagnostic
	if diagnostic.Provider == "" {
		diagnostic.Provider, diagnostic.Operation = res.Provider.Kind, res.Provider.Model
	}
	diagnostic.CostSource = "unknown"
	if problem != nil && problem.KnownUnbilled {
		zero := int64(0)
		total, price = &zero, &zero
		diagnostic.CostSource = "unbilled"
	} else if response.UsageReported || response.InputTokens > 0 || response.OutputTokens > 0 {
		t := response.InputTokens + response.OutputTokens
		c := measuredModelCharge(res.Profile, response.InputTokens, response.OutputTokens, diagnostic.CachedTokens)
		total, price = &t, &c
		diagnostic.InputTokens, diagnostic.OutputTokens = &response.InputTokens, &response.OutputTokens
		diagnostic.CostSource = "estimated"
	}
	if problem != nil {
		state, code = "failed", problem.Code
		if total == nil {
			state = "uncertain"
		}
		diagnostic.Code = code
	}
	if diagnostic.Category == "" {
		diagnostic.Category = state
	}
	diagnostic.MayBeCharged = problem == nil || !problem.KnownUnbilled
	diagnostic.CostMicros = price
	receipt, err := json.Marshal(diagnostic)
	if err != nil {
		return err
	}
	response.ReceiptID = res.ID
	raw, err := json.Marshal(response)
	if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE public.seo_content_model_calls SET state=$5,error_code=NULLIF($6,''),tokens=$7,cost_micros=$8,
		data=$9::jsonb,diagnostic=$10::jsonb,settled_at=clock_timestamp()
		WHERE id=$1 AND organization_id=$2 AND job_id=$3 AND lease_token=$4 AND state IN ('reserved','uncertain')`,
		res.ID, lease.Job.OrganizationID, lease.Job.ID, lease.Token, state, code, total, price, string(raw), string(receipt))
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return domain.ErrLeaseLost
	}
	if err = audit(ctx, tx, lease.Job.OrganizationID, "content.model.settle", res.ID, map[string]any{
		"state": state, "error_code": code, "tokens": total, "estimated_micros": price, "job_id": lease.Job.ID,
	}); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) ContentUsage(ctx context.Context, org string) ([]json.RawMessage, error) {
	return jsonRows(ctx, s.db, `SELECT jsonb_build_object('provider_id',p.id,'name',p.name,'requests',u.requests,
		'used_tokens',u.used_tokens,'reserved_tokens',u.reserved_tokens,'spent_micros',u.spent_micros,'reserved_micros',u.reserved_micros,
		'daily_tokens',m.data->'daily_tokens','daily_cost_micros',m.data->'daily_cost_micros')
		FROM public.seo_content_model_profiles m JOIN public.seo_providers p ON (p.organization_id,p.id)=(m.organization_id,m.provider_id)
		CROSS JOIN LATERAL (
			SELECT count(*) requests,COALESCE(sum(tokens),0) used_tokens,
			COALESCE(sum(reserved_tokens) FILTER (WHERE tokens IS NULL),0) reserved_tokens,
			COALESCE(sum(cost_micros),0) spent_micros,
			COALESCE(sum(reserved_micros) FILTER (WHERE cost_micros IS NULL),0) reserved_micros
			FROM public.seo_content_model_calls c WHERE c.organization_id=p.organization_id AND c.provider_id=p.id
			AND c.reserved_at >= date_trunc('day',clock_timestamp(),'UTC')
		) u WHERE p.organization_id=$1 ORDER BY p.name,p.id`, org)
}
