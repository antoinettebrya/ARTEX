package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/Autumn-27/artex/seoos/internal/domain"
)

func (s *Store) ListProviderReceipts(ctx context.Context, org, id string, limit, offset int) ([]json.RawMessage, error) {
	if err := validateOrganizationID(org); err != nil {
		return nil, err
	}
	if id != "" && !domain.ValidID(id) {
		return nil, &domain.ValidationError{Field: "provider_id", Message: "provide a valid provider or connection ID"}
	}
	if err := domain.ValidatePage(limit, offset); err != nil {
		return nil, err
	}
	return jsonRows(ctx, s.db, `SELECT jsonb_build_object('call_id',call_id,'provider_id',provider_id,
		'job_id',job_id,'occurred_at',occurred_at,'state',state,
		'reserved_micros',COALESCE((diagnostic->>'reserved_micros')::bigint,reserved_micros),'diagnostic',diagnostic)
		FROM public.seo_provider_receipts WHERE organization_id=$1 AND ($2='' OR provider_id=NULLIF($2,'')::uuid)
		ORDER BY occurred_at DESC,call_id DESC LIMIT $3 OFFSET $4`, org, id, limit, offset)
}

func (s *Store) ReconcileProvider(ctx context.Context, org, id string, day time.Time, vendorMicros int64) (domain.ProviderReconciliation, error) {
	day = day.UTC().Truncate(24 * time.Hour)
	out := domain.ProviderReconciliation{ProviderID: id, DayUTC: day.Format(time.DateOnly), VendorMicros: vendorMicros}
	if err := validateOrganizationID(org); err != nil {
		return out, err
	}
	if !domain.ValidID(id) {
		return out, domain.ErrNotFound
	}
	if vendorMicros < 0 || vendorMicros > 1_000_000_000_000 {
		return out, &domain.ValidationError{Field: "vendor_micros", Message: "provide the vendor's USD total in micros"}
	}
	var exists bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM public.seo_providers WHERE organization_id=$1 AND id=$2)`, org, id).Scan(&exists)
	if err != nil {
		return out, err
	}
	if !exists {
		return out, domain.ErrNotFound
	}
	err = s.db.QueryRowContext(ctx, `SELECT count(*),
		count(*) FILTER (WHERE diagnostic->>'cost_source'='unknown' OR diagnostic->>'cost_micros' IS NULL),
		COALESCE(sum((diagnostic->>'input_tokens')::bigint),0)::bigint,
		COALESCE(sum((diagnostic->>'output_tokens')::bigint),0)::bigint,
		COALESCE(sum((diagnostic->>'cached_tokens')::bigint),0)::bigint,
		COALESCE(sum((diagnostic->>'cost_micros')::bigint) FILTER (WHERE diagnostic->>'cost_source'='provider'),0)::bigint,
		COALESCE(sum((diagnostic->>'cost_micros')::bigint) FILTER (WHERE diagnostic->>'cost_source'='estimated'),0)::bigint,
		COALESCE(sum(COALESCE((diagnostic->>'reserved_micros')::bigint,reserved_micros))
			FILTER (WHERE diagnostic->>'cost_micros' IS NULL),0)::bigint
		FROM public.seo_provider_receipts WHERE organization_id=$1 AND provider_id=$2 AND occurred_at >= $3 AND occurred_at < $4
		AND call_id NOT LIKE 'test-%' AND call_id NOT LIKE 'execution-%'`, org, id, day, day.AddDate(0, 0, 1)).
		Scan(&out.ObservedCalls, &out.UnknownCalls, &out.InputTokens, &out.OutputTokens, &out.CachedTokens,
			&out.ConfirmedMicros, &out.EstimatedMicros, &out.ReservedMicros)
	if err != nil {
		return out, err
	}
	if out.UnknownCalls == 0 {
		difference := vendorMicros - out.ConfirmedMicros - out.EstimatedMicros
		out.DifferenceMicros = &difference
	}
	return out, nil
}
