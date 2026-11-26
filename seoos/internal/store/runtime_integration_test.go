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

func TestPostgresQueueIdempotencyRecoveryAndCancellation(t *testing.T) {
	s, ctx, _ := openTestStore(t, true)
	org := testOrganization(t, s, ctx)
	other := testOrganization(t, s, ctx)
	site, err := s.CreateSite(ctx, org, validSiteInput())
	if err != nil {
		t.Fatal(err)
	}
	input := domain.JobInput{SiteID: site.ID, Kind: domain.JobSiteInventory, IdempotencyKey: uuid.NewString()}
	if _, _, err := s.EnqueueJob(ctx, other, input); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("cross-tenant enqueue = %v", err)
	}

	type receipt struct {
		job     domain.Job
		created bool
		err     error
	}
	receipts := make(chan receipt, 6)
	for range cap(receipts) {
		go func() {
			job, created, err := s.EnqueueJob(ctx, org, input)
			receipts <- receipt{job, created, err}
		}()
	}
	first := <-receipts
	created := 0
	if first.created {
		created++
	}
	if first.err != nil {
		t.Fatal(first.err)
	}
	for range cap(receipts) - 1 {
		item := <-receipts
		if item.err != nil || first.job.ID != item.job.ID {
			t.Fatalf("duplicate submit returned a different job or error %v", item.err)
		}
		if item.created {
			created++
		}
	}
	if created != 1 {
		t.Fatalf("inserted %d copies of one job", created)
	}
	different := input
	different.SiteID = uuid.NewString()
	if _, _, err := s.EnqueueJob(ctx, org, different); !errors.Is(err, domain.ErrIdempotencyConflict) {
		t.Fatalf("reused key did not conflict: %v", err)
	}

	type claim struct {
		lease *domain.Lease
		err   error
	}
	claims := make(chan claim, 6)
	for range cap(claims) {
		go func() {
			lease, err := s.ClaimJob(ctx)
			claims <- claim{lease, err}
		}()
	}
	var old *domain.Lease
	for range cap(claims) {
		item := <-claims
		if item.err != nil {
			t.Fatal(item.err)
		}
		if item.lease != nil {
			if old != nil {
				t.Fatal("different workers received the same lease")
			}
			old = item.lease
		}
	}
	if old == nil || old.Job.ID != first.job.ID || old.Job.Attempts != 1 {
		t.Fatal("job did not receive one lease")
	}
	expireLease(t, s, ctx, old.Job.ID)
	recovered, err := s.ClaimJob(ctx)
	if err != nil || recovered == nil || recovered.Token == old.Token || recovered.Job.Attempts != 2 {
		t.Fatalf("expired lease not recovered: %v", err)
	}
	outcome := domain.JobOutcome{Result: json.RawMessage(`{"inventory":true}`)}
	if err := s.FinishJob(ctx, *old, outcome); !errors.Is(err, domain.ErrLeaseLost) {
		t.Fatalf("expired lease still completes a job: %v", err)
	}
	if err := s.FinishJob(ctx, *recovered, domain.JobOutcome{Code: "temporary", Retry: true}); err != nil {
		t.Fatal(err)
	}
	queued, err := s.GetJob(ctx, org, old.Job.ID)
	if err != nil || queued.State != "queued" || !queued.AvailableAt.After(time.Now()) {
		t.Fatalf("retry did not back off: %#v %v", queued, err)
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE public.seo_jobs SET available_at = now() WHERE id=$1`, old.Job.ID); err != nil {
		t.Fatal(err)
	}
	last, err := s.ClaimJob(ctx)
	if err != nil || last == nil || last.Job.Attempts != 3 {
		t.Fatalf("retry claim: %v", err)
	}
	expireLease(t, s, ctx, last.Job.ID)
	if lease, err := s.ClaimJob(ctx); err != nil || lease != nil {
		t.Fatalf("expired final attempt incorrectly claimed: %v", err)
	}
	failed, err := s.GetJob(ctx, org, last.Job.ID)
	if err != nil || failed.State != "failed" || failed.ErrorCode != "lease_expired" {
		t.Fatalf("final expired job not settled: %#v %v", failed, err)
	}

	input.IdempotencyKey = uuid.NewString()
	pending, _, err := s.EnqueueJob(ctx, org, input)
	if err != nil {
		t.Fatal(err)
	}
	inflight, err := s.ClaimJob(ctx)
	if err != nil || inflight == nil {
		t.Fatalf("claim pending job: %v", err)
	}
	if _, err := s.CancelJob(ctx, other, pending.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("cross-tenant cancellation = %v", err)
	}
	if cancelled, err := s.CancelJob(ctx, org, pending.ID); err != nil || cancelled.State != "cancelled" {
		t.Fatalf("cancel job: %s, %v", cancelled.State, err)
	}
	if err := s.FinishJob(ctx, *inflight, outcome); !errors.Is(err, domain.ErrLeaseLost) {
		t.Fatalf("cancelled job accepted late completion: %v", err)
	}
}

func expireLease(t *testing.T, s *Store, ctx context.Context, id string) {
	t.Helper()
	if _, err := s.db.ExecContext(ctx, `UPDATE public.seo_jobs SET lease_until = now() - interval '1 second' WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresProviderKeyIsolationAndBudget(t *testing.T) {
	s, ctx, _ := openTestStore(t, true)
	org := testOrganization(t, s, ctx)
	other := testOrganization(t, s, ctx)
	if err := s.SetProviderKey(bytes.Repeat([]byte{27}, 32)); err != nil {
		t.Fatal(err)
	}
	site, err := s.CreateSite(ctx, org, validSiteInput())
	if err != nil {
		t.Fatal(err)
	}
	otherSite, err := s.CreateSite(ctx, other, validSiteInput())
	if err != nil {
		t.Fatal(err)
	}

	const key = "test-only-credential-abcdef"
	providerInput := domain.ProviderInput{ProviderSettings: domain.ProviderSettings{
		Name: "SEO Test", Model: "test-model", RequestsPerMinute: 1, RequestsPerDay: 2,
		MaxOutputTokens: 256, TimeoutSeconds: 10, Enabled: true,
	}, APIKey: key}
	p, err := s.SaveProvider(ctx, org, "", providerInput)
	if err != nil {
		t.Fatal(err)
	}
	var stored []byte
	if err := s.db.QueryRowContext(ctx, `SELECT credential FROM public.seo_providers WHERE id=$1`, p.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(stored, []byte(key)) {
		t.Fatal("stored credential contains plaintext")
	}
	if items, err := s.ListProviders(ctx, other, 10, 0); err != nil || len(items) != 0 {
		t.Fatalf("other organization can list the provider or query failed: %v", err)
	}
	input := domain.JobInput{SiteID: otherSite.ID, ProviderID: p.ID, Kind: domain.JobSiteBrief, IdempotencyKey: uuid.NewString()}
	if _, _, err := s.EnqueueJob(ctx, other, input); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("cross-tenant provider association: %v", err)
	}
	input.SiteID = site.ID
	second, first := (*domain.Lease)(nil), (*domain.Lease)(nil)
	for i := range 2 {
		input.IdempotencyKey = uuid.NewString()
		if _, _, err := s.EnqueueJob(ctx, org, input); err != nil {
			t.Fatal(err)
		}
		lease, err := s.ClaimJob(ctx)
		if err != nil || lease == nil {
			t.Fatalf("claim error %v", err)
		}
		if i == 0 {
			first = lease
		} else {
			second = lease
		}
	}
	type reservation struct {
		lease *domain.Lease
		call  domain.ProviderCall
		err   error
	}
	results := make(chan reservation, 2)
	for _, lease := range []*domain.Lease{first, second} {
		go func() {
			call, err := s.ReserveProviderCall(ctx, *lease)
			results <- reservation{lease, call, err}
		}()
	}
	used, deferred := 0, 0
	for range 2 {
		res := <-results
		if res.err != nil {
			t.Fatal(res.err)
		}
		if res.call.DeferUntil.IsZero() {
			used++
			if res.call.APIKey != key {
				t.Fatal("worker did not get the correct scoped key")
			}
			if err := s.FinishJob(ctx, *res.lease, domain.JobOutcome{Code: "provider_retryable", Retry: true}); err != nil {
				t.Fatal(err)
			}
			continue
		}
		deferred++
		if res.call.APIKey != "" {
			t.Fatal("budget-denied job received a credential")
		}
		if err := s.FinishJob(ctx, *res.lease, domain.JobOutcome{Code: "provider_budget_wait", DeferUntil: res.call.DeferUntil}); err != nil {
			t.Fatal(err)
		}
		waiting, err := s.GetJob(ctx, org, res.lease.Job.ID)
		if err != nil || waiting.State != "queued" || waiting.Attempts != 0 {
			t.Fatal("a budget wait spent an attempt")
		}
	}
	if used != 1 || deferred != 1 {
		t.Fatalf("concurrent reservations got %d allowed and %d deferred", used, deferred)
	}

	providerInput.Version, providerInput.APIKey = p.Version, ""
	providerInput.RequestsPerDay, providerInput.RequestsPerMinute = 1, 60
	if _, err := s.SaveProvider(ctx, org, p.ID, providerInput); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveProvider(ctx, org, p.ID, providerInput); !errors.Is(err, domain.ErrVersionConflict) {
		t.Fatalf("stale provider settings mutation = %v", err)
	}
	input.IdempotencyKey = uuid.NewString()
	if _, _, err := s.EnqueueJob(ctx, org, input); err != nil {
		t.Fatal(err)
	}
	lease, err := s.ClaimJob(ctx)
	if err != nil || lease == nil {
		t.Fatalf("claim after changing budget: %v", err)
	}
	day, err := s.ReserveProviderCall(ctx, *lease)
	if err != nil || day.DeferUntil.IsZero() || day.DeferUntil.Hour() != 0 || day.DeferUntil.Location() != time.UTC {
		t.Fatalf("daily quota does not defer to midnight UTC: %v", err)
	}
}
