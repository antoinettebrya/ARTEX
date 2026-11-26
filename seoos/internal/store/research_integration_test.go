package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Autumn-27/artex/seoos/internal/domain"
	"github.com/google/uuid"
)

var researchTestMarket = domain.Market{CountryCode: "US", LocationCode: 2840, LanguageCode: "en", SearchEngine: "google", Device: "desktop"}

func newResearchTestSite(t *testing.T, s *Store, ctx context.Context, org, base string) domain.Site {
	t.Helper()
	in := validSiteInput()
	in.BaseURL = base
	site, err := s.CreateSite(ctx, org, in)
	if err != nil {
		t.Fatal(err)
	}
	return site
}

func newResearchTestProvider(t *testing.T, s *Store, ctx context.Context, org string, minute int, cap int64) domain.Provider {
	t.Helper()
	if err := s.SetProviderKey(bytes.Repeat([]byte{75}, 32)); err != nil {
		t.Fatal(err)
	}
	p, err := s.SaveProvider(ctx, org, "", domain.ProviderInput{Kind: domain.ProviderDataForSEO,
		Login: "fixture-login", Password: "fixture-credential", ProviderSettings: domain.ProviderSettings{
			Name: "Data provider fixture", RequestsPerMinute: minute, RequestsPerDay: 10,
			TimeoutSeconds: 5, Enabled: true, DailyCostMicros: &cap,
		}})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func claimResearch(t *testing.T, s *Store, ctx context.Context, job domain.Job) (domain.Lease, domain.ResearchContext) {
	t.Helper()
	lease, err := s.ClaimJob(ctx)
	if err != nil || lease == nil || lease.Job.ID != job.ID {
		t.Fatalf("failed to claim expected job %s: %#v, %v", job.ID, lease, err)
	}
	scope, err := s.ResearchContext(ctx, *lease)
	if err != nil {
		t.Fatal(err)
	}
	return *lease, scope
}

func enqueueResearchTest(t *testing.T, s *Store, ctx context.Context, org string, input domain.ResearchInput) domain.Job {
	t.Helper()
	job, created, err := s.EnqueueResearch(ctx, org, input)
	if err != nil || !created {
		t.Fatalf("expected a new research job: %v", err)
	}
	return job
}

func TestPostgresResearchRetainsTenantMarketAndRawEvidence(t *testing.T) {
	s, ctx, _ := openTestStore(t, true)
	org, other := testOrganization(t, s, ctx), testOrganization(t, s, ctx)
	first := newResearchTestSite(t, s, ctx, org, "https://example.com")
	second := newResearchTestSite(t, s, ctx, org, "https://second.example.com")
	foreign := newResearchTestSite(t, s, ctx, other, "https://example.com")
	p := newResearchTestProvider(t, s, ctx, org, 12, 50000)
	input := domain.ResearchInput{SiteID: first.ID, ProviderID: p.ID, IdempotencyKey: uuid.NewString(),
		Kind: domain.KeywordMetrics, Keyword: "robot vacuum", Market: researchTestMarket, Priority: 60}
	job := enqueueResearchTest(t, s, ctx, org, input)
	if replay, created, err := s.EnqueueResearch(ctx, org, input); err != nil || created || replay.ID != job.ID {
		t.Fatalf("idempotent replay: %#v %t %v", replay, created, err)
	}
	changed := input
	changed.Market.Device = "mobile"
	if _, _, err := s.EnqueueResearch(ctx, org, changed); !errors.Is(err, domain.ErrIdempotencyConflict) {
		t.Fatalf("changed replay succeeded: %v", err)
	}
	changed.IdempotencyKey, changed.SiteID = uuid.NewString(), foreign.ID
	if _, _, err := s.EnqueueResearch(ctx, other, changed); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("foreign credentials were accepted: %v", err)
	}
	var sealed []byte
	if err := s.db.QueryRowContext(ctx, `SELECT credential FROM public.seo_providers WHERE id=$1`, p.ID).Scan(&sealed); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, []byte("fixture-credential")) || bytes.Contains(sealed, []byte("fixture-login")) {
		t.Fatal("the database contains plaintext provider credentials")
	}
	lease, scope := claimResearch(t, s, ctx, job)
	plan := domain.DataPlan{ReserveMicros: 1000, TTL: time.Hour}
	request := domain.DataRequest{Operation: domain.DataOverview, Market: input.Market, Keyword: input.Keyword}
	difficulty, volume, cost := 39, int64(800), int64(100)
	var firstID string
	for _, op := range []domain.DataOperation{domain.DataOverview, domain.DataVolume} {
		request.Operation = op
		r, err := s.ReserveData(ctx, lease, scope, request, plan)
		if err != nil || r.ID == "" || r.Cached != nil || r.Access.Password != "fixture-credential" || r.Access.Login != "fixture-login" {
			t.Fatalf("credential reservation failed: %v", err)
		}
		metric := domain.KeywordMetric{Keyword: input.Keyword, DeviceScope: "all"}
		if op == domain.DataOverview {
			firstID, metric.KeywordDifficulty = r.ID, &difficulty
		} else {
			metric.SearchVolume = &volume
		}
		response := domain.DataResponse{Source: string(op), TaskID: "fixture-receipt", CostMicros: &cost,
			Raw: json.RawMessage(`{"fixture": true}`), CollectedAt: time.Now().UTC(), Keywords: []domain.KeywordMetric{metric}}
		if err := s.SettleData(ctx, lease, r.ID, response, plan, nil); err != nil {
			t.Fatal(err)
		}
		if err := s.PersistData(ctx, lease, scope, op, r.ID, response, nil); err != nil {
			t.Fatal(err)
		}
	}
	evidence, err := s.ResearchEvidence(ctx, job, scope)
	if err != nil || len(evidence) != 1 || evidence[0].Metric.SearchVolume == nil || *evidence[0].Metric.SearchVolume != volume ||
		evidence[0].Metric.KeywordDifficulty == nil || *evidence[0].Metric.KeywordDifficulty != difficulty ||
		evidence[0].MetricSources["keyword_difficulty"].SnapshotID != firstID {
		t.Fatalf("independent provider fields were not combined with provenance: %#v %v", evidence, err)
	}
	if err := s.FinishJob(ctx, lease, domain.JobOutcome{Result: json.RawMessage(`{}`)}); err != nil {
		t.Fatal(err)
	}
	if raw, err := s.ResearchSnapshot(ctx, org, firstID); err != nil || string(raw) != `{"fixture": true}` {
		t.Fatalf("exact upstream evidence lost: %s %v", raw, err)
	}
	if _, err := s.ResearchSnapshot(ctx, other, firstID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("foreign snapshot was accessible: %v", err)
	}
	q := domain.ResearchQuery{SiteID: first.ID, Market: &input.Market, Limit: 50}
	if _, err := s.ListResearch(ctx, other, "keywords", q); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("foreign evidence was accessible: %v", err)
	}
	q.SiteID = foreign.ID
	if rows, err := s.ListResearch(ctx, other, "keywords", q); err != nil || len(rows) != 0 {
		t.Fatalf("foreign site inherited an organization's data: %s %v", rows, err)
	}
	input.SiteID, input.IdempotencyKey, input.Market.Device = second.ID, uuid.NewString(), "mobile"
	job = enqueueResearchTest(t, s, ctx, org, input)
	lease, scope = claimResearch(t, s, ctx, job)
	request.Market, request.Operation = input.Market, domain.DataOverview
	reservation, err := s.ReserveData(ctx, lease, scope, request, plan)
	if err != nil || reservation.Cached == nil || reservation.ID != firstID || reservation.Access != (domain.DataAccess{}) {
		t.Fatalf("cross-site/device Google keyword cache failed: %v", err)
	}
	if err := s.PersistData(ctx, lease, scope, request.Operation, reservation.ID, *reservation.Cached, nil); err != nil {
		t.Fatal(err)
	}
	q.SiteID, q.Market = second.ID, &input.Market
	if rows, err := s.ListResearch(ctx, org, "keywords", q); err != nil || len(rows) != 1 || !bytes.Contains(rows[0], []byte(`"mobile"`)) {
		t.Fatalf("mobile association with device-agnostic evidence missing: %s %v", rows, err)
	}
	for _, market := range []domain.Market{
		{CountryCode: "JP", LocationCode: 2392, LanguageCode: "ja", SearchEngine: "google", Device: "mobile"},
		{CountryCode: "US", LocationCode: 2840, LanguageCode: "es", SearchEngine: "google", Device: "mobile"},
	} {
		q.Market = &market
		if rows, err := s.ListResearch(ctx, org, "keywords", q); err != nil || len(rows) != 0 {
			t.Fatalf("data leaked across country/language: %s %v", rows, err)
		}
	}
	if err := s.FinishJob(ctx, lease, domain.JobOutcome{Result: json.RawMessage(`{}`)}); err != nil {
		t.Fatal(err)
	}
	var attempts int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM public.seo_research_calls WHERE organization_id=$1`, org).Scan(&attempts); err != nil || attempts != 2 {
		t.Fatalf("expected only two paid calls, got %d: %v", attempts, err)
	}
}

func TestPostgresResearchBudgetPriorityAndWorkerRecovery(t *testing.T) {
	s, ctx, _ := openTestStore(t, true)
	org := testOrganization(t, s, ctx)
	site := newResearchTestSite(t, s, ctx, org, "https://example.com")
	p := newResearchTestProvider(t, s, ctx, org, 1, 2000)
	in := domain.ResearchInput{SiteID: site.ID, ProviderID: p.ID, Market: researchTestMarket,
		Kind: domain.SERPAnalysis, Keyword: "second term", IdempotencyKey: uuid.NewString()}
	low := enqueueResearchTest(t, s, ctx, org, in)
	in.Keyword, in.Priority, in.IdempotencyKey = "priority term", 100, uuid.NewString()
	high := enqueueResearchTest(t, s, ctx, org, in)
	lease, scope := claimResearch(t, s, ctx, high)
	plan := domain.DataPlan{ReserveMicros: 1000, TTL: time.Hour}
	req := domain.DataRequest{Operation: domain.DataSERP, Market: in.Market, Keyword: in.Keyword}
	r, err := s.ReserveData(ctx, lease, scope, req, plan)
	if err != nil || r.ID == "" {
		t.Fatalf("first call not journaled: %v", err)
	}
	// Emulate process loss after sending the request but before receiving its receipt.
	if _, err := s.db.ExecContext(ctx, `UPDATE public.seo_research_calls SET reserved_at=clock_timestamp()-interval '91 seconds' WHERE id=$1`, r.ID); err != nil {
		t.Fatal(err)
	}
	expireLease(t, s, ctx, high.ID)
	recovered, scope := claimResearch(t, s, ctx, high)
	if recovered.Token == lease.Token || recovered.Job.Attempts != 2 {
		t.Fatal("the worker did not acquire a new recovery lease")
	}
	var problem *domain.DataError
	if got, err := s.ReserveData(ctx, recovered, scope, req, plan); !errors.As(err, &problem) || problem.Code != "provider_receipt_unknown" || got.ID != "" {
		t.Fatalf("ambiguous charge was automatically replayed: %v", err)
	}
	duplicate := in
	duplicate.IdempotencyKey = uuid.NewString()
	copyJob := enqueueResearchTest(t, s, ctx, org, duplicate)
	copyLease, copyScope := claimResearch(t, s, ctx, copyJob)
	if got, err := s.ReserveData(ctx, copyLease, copyScope, req, plan); !errors.As(err, &problem) || problem.Code != "provider_receipt_unknown" || got.ID != "" {
		t.Fatalf("new job ID bypassed the ambiguous-charge barrier: %v", err)
	}
	if err := s.FinishJob(ctx, copyLease, domain.JobOutcome{Code: "provider_receipt_unknown"}); err != nil {
		t.Fatal(err)
	}
	if err := s.FinishJob(ctx, lease, domain.JobOutcome{Result: json.RawMessage(`{}`)}); !errors.Is(err, domain.ErrLeaseLost) {
		t.Fatalf("stale lease was able to finish the task: %v", err)
	}
	cost := int64(600)
	response := domain.DataResponse{CostMicros: &cost, Raw: json.RawMessage(`{"receipt":"late"}`)}
	if err := s.SettleData(ctx, lease, r.ID, response, plan, nil); err != nil {
		t.Fatalf("late accounting receipt was lost: %v", err)
	}
	if err := s.FinishJob(ctx, recovered, domain.JobOutcome{Code: "provider_receipt_unknown"}); err != nil {
		t.Fatal(err)
	}
	lease, scope = claimResearch(t, s, ctx, low)
	req.Keyword = scope.Request.Keyword
	r, err = s.ReserveData(ctx, lease, scope, req, plan)
	if err != nil || r.ID == "" {
		t.Fatalf("new charge could not reserve the remaining allowance: %v", err)
	}
	if err := s.SettleData(ctx, lease, r.ID, response, plan, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.FinishJob(ctx, lease, domain.JobOutcome{Result: json.RawMessage(`{}`)}); err != nil {
		t.Fatal(err)
	}
	in.Keyword, in.IdempotencyKey = "third term", uuid.NewString()
	job := enqueueResearchTest(t, s, ctx, org, in)
	lease, scope = claimResearch(t, s, ctx, job)
	req.Keyword = in.Keyword
	small := domain.DataPlan{ReserveMicros: 1, TTL: time.Hour}
	minute, err := s.ReserveData(ctx, lease, scope, req, small)
	if err != nil || minute.DeferUntil.IsZero() || minute.DeferUntil.After(time.Now().Add(time.Minute)) || minute.Access.Login != "" {
		t.Fatalf("minute rate limit failed: %v", err)
	}
	// A prior priced call in the current UTC day also blocks the larger plan.
	var remaining int64
	if err := s.db.QueryRowContext(ctx, `SELECT 2000 - COALESCE(sum(cost_micros),0) FROM public.seo_research_calls
		WHERE organization_id=$1 AND reserved_at >= date_trunc('day',clock_timestamp(),'UTC')`, org).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	plan.ReserveMicros = remaining + 1
	day, err := s.ReserveData(ctx, lease, scope, req, plan)
	if err != nil || day.DeferUntil.IsZero() || day.DeferUntil.Hour() != 0 || day.DeferUntil.Minute() != 0 || day.Access.Login != "" {
		t.Fatalf("daily budget limit failed: %v", err)
	}
	plan.ReserveMicros = 2001
	if _, err := s.ReserveData(ctx, lease, scope, req, plan); !errors.Is(err, ErrResearchBudgetTooLow) {
		t.Fatalf("plan above the absolute budget did not fail: %v", err)
	}
	if job, err := s.CancelJob(ctx, org, job.ID); err != nil || job.State != "cancelled" {
		t.Fatalf("cannot cancel a research job: %v", err)
	}
}
