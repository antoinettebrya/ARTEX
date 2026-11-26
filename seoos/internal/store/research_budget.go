package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/Autumn-27/artex/seoos/internal/domain"
	"github.com/google/uuid"
)

var ErrResearchBudgetTooLow = &domain.DataError{Code: "provider_budget_too_low", KnownUnbilled: true}

func dataFingerprint(r domain.DataRequest, providerID string) [32]byte {
	term := domain.KeywordKey(r.Keyword)
	market := r.Market
	if !r.DeviceSpecific() {
		market.Device = ""
	}
	var account string
	switch r.Operation {
	case domain.DataPage, domain.DataPageMicrodata:
		// The Microdata API resolves the Instant Pages task inside its originating account.
		account = providerID
		term = ""
	case domain.DataPageContent:
		term = ""
	}
	key, _ := json.Marshal(struct {
		Operation domain.DataOperation
		Market    domain.Market
		Keyword   string
		URL       string
		Account   string
		Task      string
	}{r.Operation, market, term, r.TargetURL, account, r.TargetTaskID})
	return sha256.Sum256(key)
}

// ReserveData guards a paid operation under its current queue lease. Fresh
// results are shared within one organization across sites and provider entries.
func (s *Store) ReserveData(ctx context.Context, lease domain.Lease, scope domain.ResearchContext, request domain.DataRequest, plan domain.DataPlan) (domain.DataReservation, error) {
	var reservation domain.DataReservation
	if plan.ReserveMicros <= 0 || plan.TTL <= 0 {
		return reservation, errors.New("invalid data plan")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return reservation, err
	}
	defer tx.Rollback()
	job, err := lockLease(ctx, tx, lease)
	if err != nil {
		return reservation, err
	}
	fingerprint := dataFingerprint(request, job.ProviderID)
	// Coordinate the same operation across credential records within the tenant.
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1 || encode($2::bytea,'hex'), 0))`,
		job.OrganizationID, fingerprint[:]); err != nil {
		return reservation, err
	}
	var cached []byte
	var state string
	err = tx.QueryRowContext(ctx, `SELECT id::text, state, COALESCE(parsed_response,'null'::jsonb)
		FROM public.seo_research_calls WHERE organization_id=$1 AND fingerprint=$2 AND
		((state='succeeded' AND expires_at > clock_timestamp()) OR
		 (state='reserved' AND reserved_at > clock_timestamp()-interval '90 seconds'))
		ORDER BY (state='succeeded') DESC, expires_at DESC LIMIT 1`,
		job.OrganizationID, fingerprint[:]).Scan(&reservation.ID, &state, &cached)
	if err == nil {
		if state == "reserved" {
			return domain.DataReservation{DeferUntil: time.Now().UTC().Add(3 * time.Second)}, nil
		}
		var result domain.DataResponse
		if err = json.Unmarshal(cached, &result); err != nil {
			return reservation, err
		}
		reservation.Cached = &result
		return reservation, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return reservation, err
	}
	if s.providerCipher == nil {
		return reservation, domain.ErrProviderNotConfigured
	}
	var sealed []byte
	provider, err := scanProvider(tx.QueryRowContext(ctx, `SELECT `+providerColumns+`, credential
		FROM public.seo_providers WHERE organization_id=$1 AND id=$2 FOR NO KEY UPDATE`, job.OrganizationID, job.ProviderID), &sealed)
	if err != nil {
		return reservation, err
	}
	if !provider.Enabled || provider.Kind != domain.ProviderDataForSEO || provider.DailyCostMicros == nil {
		return reservation, domain.ErrProviderDisabled
	}
	cap := *provider.DailyCostMicros
	if cap < plan.ReserveMicros {
		return reservation, ErrResearchBudgetTooLow
	}

	var now time.Time
	if err := tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return reservation, err
	}
	now = now.UTC()
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	minute := now.Add(-time.Minute)

	stale, err := tx.QueryContext(ctx, `UPDATE public.seo_research_calls
		SET state='uncertain', error_code='provider_receipt_missing', settled_at=$3
		WHERE organization_id=$1 AND fingerprint=$2 AND state='reserved' AND reserved_at <= $3::timestamptz - interval '90 seconds'
		RETURNING id::text`, job.OrganizationID, fingerprint[:], now)
	if err != nil {
		return reservation, err
	}
	var expired []string
	for stale.Next() {
		var id string
		if err := stale.Scan(&id); err != nil {
			stale.Close()
			return reservation, err
		}
		expired = append(expired, id)
	}
	stale.Close()
	if err := stale.Err(); err != nil {
		return reservation, err
	}
	for _, id := range expired {
		if err := audit(ctx, tx, job.OrganizationID, "research.call.uncertain", id, map[string]string{"operation": string(request.Operation)}); err != nil {
			return reservation, err
		}
	}
	var receiptUnknown bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM public.seo_research_calls
		WHERE organization_id=$1 AND job_id=$2 AND fingerprint=$3 AND state='uncertain')`,
		job.OrganizationID, job.ID, fingerprint[:]).Scan(&receiptUnknown); err != nil {
		return reservation, err
	}
	if receiptUnknown {
		if err := tx.Commit(); err != nil {
			return reservation, err
		}
		return reservation, &domain.DataError{Code: "provider_receipt_unknown"}
	}

	var countDay, countMinute int
	var used int64
	var oldest sql.NullTime
	err = tx.QueryRowContext(ctx, `SELECT count(*) FILTER (WHERE reserved_at >= $3),
		count(*) FILTER (WHERE reserved_at > $4), min(reserved_at) FILTER (WHERE reserved_at > $4),
		COALESCE(sum(COALESCE(cost_micros, reserved_micros)) FILTER (WHERE reserved_at >= $3),0)::bigint
		FROM public.seo_research_calls
		WHERE organization_id=$1 AND provider_id=$2 AND reserved_at >= least($3::timestamptz,$4::timestamptz)`,
		job.OrganizationID, provider.ID, day, minute).Scan(&countDay, &countMinute, &oldest, &used)
	if err != nil {
		return reservation, err
	}
	switch {
	case countDay >= provider.RequestsPerDay || used > cap-plan.ReserveMicros:
		reservation.DeferUntil = day.AddDate(0, 0, 1)
		return reservation, tx.Commit()
	case countMinute >= provider.RequestsPerMinute:
		reservation.DeferUntil = oldest.Time.Add(time.Minute)
		return reservation, tx.Commit()
	}
	secret, err := s.providerCipher.Open(nil, nil, sealed, providerBinding(provider.Kind, job.OrganizationID, provider.ID))
	var credentials dataForSEOCredentials
	if err != nil || json.Unmarshal(secret, &credentials) != nil || credentials.Login == "" || credentials.Password == "" {
		return reservation, domain.ErrProviderNotConfigured
	}

	err = tx.QueryRowContext(ctx, `INSERT INTO public.seo_research_calls
		(id, organization_id, provider_id, job_id, market_id, lease_token, operation,
		fingerprint, state, reserved_micros, reserved_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,'reserved',$9,$10)
		ON CONFLICT (organization_id, fingerprint) WHERE state='reserved' DO NOTHING
		RETURNING id::text`, uuid.NewString(), job.OrganizationID, provider.ID, job.ID, scope.MarketID,
		lease.Token, request.Operation, fingerprint[:], plan.ReserveMicros, now).Scan(&reservation.ID)
	if errors.Is(err, sql.ErrNoRows) {
		reservation.DeferUntil = now.Add(3 * time.Second)
		return reservation, tx.Commit()
	}
	if err != nil {
		return reservation, err
	}
	if err := audit(ctx, tx, job.OrganizationID, "research.call.reserve", reservation.ID, map[string]any{
		"provider_id": provider.ID, "job_id": job.ID, "operation": request.Operation, "reserved_micros": plan.ReserveMicros,
	}); err != nil {
		return reservation, err
	}
	if err := tx.Commit(); err != nil {
		return reservation, err
	}
	reservation.Access = domain.DataAccess{Login: credentials.Login, Password: credentials.Password,
		Timeout: time.Duration(provider.TimeoutSeconds) * time.Second}
	return reservation, nil
}

// SettleData retains billing facts even when a user cancelled the job while
// the request was in flight. Materializing evidence still checks the live lease.
func (s *Store) SettleData(ctx context.Context, lease domain.Lease, id string, response domain.DataResponse, plan domain.DataPlan, failure *domain.DataError) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var reserved int64
	err = tx.QueryRowContext(ctx, `SELECT reserved_micros FROM public.seo_research_calls
		WHERE organization_id=$1 AND id=$2 AND job_id=$3 AND lease_token=$4 AND state IN ('reserved','uncertain')
		FOR NO KEY UPDATE`, lease.Job.OrganizationID, id, lease.Job.ID, lease.Token).Scan(&reserved)
	if err != nil {
		return classifyError(err)
	}
	state, code := "succeeded", ""
	now := time.Now().UTC()
	if response.CollectedAt.IsZero() {
		response.CollectedAt = now
	}
	var expires *time.Time
	if failure != nil {
		state, code = "failed", failure.Code
		if response.CostMicros == nil && failure.KnownUnbilled {
			zero := int64(0)
			response.CostMicros = &zero
		}
		if response.CostMicros == nil {
			state = "uncertain"
		}
	} else {
		until := now.Add(plan.TTL)
		expires = &until
	}
	var raw []byte
	if len(response.Raw) <= domain.MaxProviderRawBytes && json.Valid(response.Raw) {
		raw = response.Raw
	}
	data, err := json.Marshal(response)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE public.seo_research_calls SET state=$3,error_code=NULLIF($4,''),
		cost_micros=$5,settled_at=clock_timestamp(),expires_at=$6,collected_at=$7,provider_task_id=NULLIF($8,''),
		source=NULLIF($9,''),raw_response=$10,parsed_response=$11::jsonb
		WHERE organization_id=$1 AND id=$2`, lease.Job.OrganizationID, id, state, code, response.CostMicros, expires,
		response.CollectedAt, response.TaskID, response.Source, raw, string(data))
	if err != nil {
		return err
	}
	if err := audit(ctx, tx, lease.Job.OrganizationID, "research.call.settle", id, map[string]any{
		"job_id": lease.Job.ID, "outcome": state, "reserved_micros": reserved,
		"cost_micros": response.CostMicros, "error_code": code,
	}); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) ResearchSnapshot(ctx context.Context, org, id string) (json.RawMessage, error) {
	if !domain.ValidID(id) {
		return nil, domain.ErrNotFound
	}
	var raw []byte
	err := s.db.QueryRowContext(ctx, `SELECT raw_response FROM public.seo_research_calls
		WHERE organization_id=$1 AND id=$2 AND raw_response IS NOT NULL`, org, id).Scan(&raw)
	return json.RawMessage(raw), classifyError(err)
}

func (s *Store) ResearchUsage(ctx context.Context, org string, q domain.ResearchQuery) ([]json.RawMessage, error) {
	if _, err := s.validateResearchQuery(ctx, org, q); err != nil {
		return nil, err
	}
	return jsonRows(ctx, s.db, `WITH today AS (SELECT date_trunc('day',clock_timestamp(),'UTC') AS start)
		SELECT jsonb_build_object('provider_id',p.id,'name',p.name,'requests',u.requests,
			'spent_micros',u.spent,'reserved_micros',u.reserved,'day_limit_micros',p.daily_cost_micros,
			'date_utc',to_char(today.start AT TIME ZONE 'UTC','YYYY-MM-DD'))
		FROM public.seo_providers p CROSS JOIN today
		CROSS JOIN LATERAL (SELECT count(*) AS requests,COALESCE(sum(cost_micros),0) AS spent,
			COALESCE(sum(reserved_micros) FILTER (WHERE cost_micros IS NULL),0) AS reserved
			FROM public.seo_research_calls c WHERE c.organization_id=p.organization_id AND c.provider_id=p.id
			AND c.reserved_at >= today.start) u
		WHERE p.organization_id=$1 AND p.kind='dataforseo'
		ORDER BY p.created_at DESC,p.id LIMIT $2 OFFSET $3`, org, q.Limit, q.Offset)
}
