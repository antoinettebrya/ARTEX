package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Autumn-27/artex/seoos/internal/domain"
	"github.com/google/uuid"
)

func TestPostgresModelReceiptsAndReconciliationAfterRecovery(t *testing.T) {
	s, ctx, _ := openTestStore(t, true)
	org, other := testOrganization(t, s, ctx), testOrganization(t, s, ctx)
	if err := s.SetProviderKey(bytes.Repeat([]byte{17}, 32)); err != nil {
		t.Fatal(err)
	}
	site := newResearchTestSite(t, s, ctx, org, "https://model-receipts.example.com")
	provider, profile := contentModelFixture(t, s, ctx, org, 20)
	cachedRate := int64(500_000)
	profile.CachedInputMicrosPerMillion = &cachedRate
	profile.InputMicrosPerMillion, profile.OutputMicrosPerMillion = 1_500_000, 3_000_000
	if _, err := s.SaveContentModel(ctx, org, profile); err != nil {
		t.Fatal(err)
	}
	create := func() (domain.Job, domain.Lease) {
		t.Helper()
		job, _, err := s.EnqueueJob(ctx, org, domain.JobInput{SiteID: site.ID, Kind: domain.JobSiteBrief,
			ProviderID: provider.ID, IdempotencyKey: uuid.NewString()})
		if err != nil {
			t.Fatal(err)
		}
		return job, claimContentFixture(t, s, ctx, job)
	}
	job, lease := create()
	call, err := s.ReserveProviderCall(ctx, lease)
	if err != nil || call.APIKey == "" {
		t.Fatalf("reserve legacy inference: %v", err)
	}
	in, out, cached := int64(800), int64(200), int64(300)
	receipt := domain.ProviderReceipt{Provider: provider.Kind, Operation: provider.Model, HTTPStatus: 200,
		RequestID: "req_fixture_recover", Category: "success", MayBeCharged: true,
		InputTokens: &in, OutputTokens: &out, CachedTokens: &cached}
	outcome := domain.JobOutcome{Result: json.RawMessage(`{"brief":"verified output"}`), Diagnostic: &receipt}
	if err := s.SettleProviderCall(ctx, lease, call, outcome); err != nil {
		t.Fatal(err)
	}
	expireLease(t, s, ctx, job.ID)
	recovered := claimContentFixture(t, s, ctx, job)
	read, err := s.ReserveProviderCall(ctx, recovered)
	if err != nil || read.APIKey != "" || read.Cached == nil || !bytes.Contains(read.Cached.Result, []byte("verified output")) {
		t.Fatalf("recovery didn't reuse the persisted reply: %v", err)
	}
	if err := s.FinishJob(ctx, recovered, *read.Cached); err != nil {
		t.Fatal(err)
	}
	bill, err := s.ReconcileProvider(ctx, org, provider.ID, time.Now(), 1450)
	if err != nil || bill.ObservedCalls != 1 || bill.UnknownCalls != 0 || bill.EstimatedMicros != 1500 ||
		bill.ConfirmedMicros != 0 || bill.DifferenceMicros == nil || *bill.DifferenceMicros != -50 ||
		bill.InputTokens != 800 || bill.OutputTokens != 200 || bill.CachedTokens != 300 {
		t.Fatalf("invalid estimated cost or statement variance: %#v, %v", bill, err)
	}
	list, err := s.ListProviderReceipts(ctx, org, provider.ID, 20, 0)
	if err != nil || len(list) != 1 || !bytes.Contains(list[0], []byte("req_fixture_recover")) ||
		bytes.Contains(list[0], []byte(call.APIKey)) {
		t.Fatalf("safe trace missing from the tenant ledger: %v", err)
	}
	if rows, err := s.ListProviderReceipts(ctx, other, provider.ID, 20, 0); err != nil || len(rows) != 0 {
		t.Fatalf("provider receipts leaked tenants: %v", err)
	}
	if _, err := s.ReconcileProvider(ctx, other, provider.ID, time.Now(), 1450); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("cross-tenant billing: %v", err)
	}
	lost, inflight := create()
	if _, err := s.ReserveProviderCall(ctx, inflight); err != nil {
		t.Fatal(err)
	}
	expireLease(t, s, ctx, lost.ID)
	retry := claimContentFixture(t, s, ctx, lost)
	var pending *domain.DataError
	if _, err := s.ReserveProviderCall(ctx, retry); !errors.As(err, &pending) || pending.Code != "provider_receipt_unknown" {
		t.Fatalf("recovery would duplicate possibly billed request: %v", err)
	}
	bill, err = s.ReconcileProvider(ctx, org, provider.ID, time.Now(), 1450)
	if err != nil || bill.UnknownCalls != 1 || bill.DifferenceMicros != nil {
		t.Fatalf("unsettled call was hidden from invoice reconciliation: %#v, %v", bill, err)
	}
}

func TestPostgresIndexingIntentSurvivesAnInterruptedWorker(t *testing.T) {
	s, ctx, _ := openTestStore(t, true)
	org := testOrganization(t, s, ctx)
	owner := authenticatedContentRole(t, s, ctx, org, domain.RoleOwner)
	site := newResearchTestSite(t, s, ctx, org, "https://index-recovery.example.com")
	if err := s.SetProviderKey(bytes.Repeat([]byte{38}, 32)); err != nil {
		t.Fatal(err)
	}
	connection, err := s.SaveExecutionConnection(owner, org, "", domain.ExecutionConnectionSave{
		ExecutionConnectionInput: domain.ExecutionConnectionInput{SiteID: site.ID, Kind: domain.ExecutionIndexNow,
			Name: "Local IndexNow contract", KeyLocation: site.BaseURL + "/fixture-key.txt", Enabled: true, AllowWrites: true},
		Credential: &domain.ExecutionCredential{Secret: "fixture-key"},
	})
	if err != nil {
		t.Fatal(err)
	}
	target := site.BaseURL + "/page"
	job, _, err := s.EnqueueExecution(owner, org, domain.ExecutionJobInput{Kind: domain.ExecIndexNow, SiteID: site.ID,
		ConnectionID: connection.ID, URL: target, IdempotencyKey: uuid.NewString()})
	if err != nil {
		t.Fatal(err)
	}
	first := claimContentFixture(t, s, ctx, job)
	if err := s.BeginExecutionSubmission(ctx, first, connection.Kind, target); err != nil {
		t.Fatal(err)
	}
	if err := s.ExecutionSubmissionFailure(ctx, first, connection.Kind, target, 429, "execution_upstream_quota", false); err != nil {
		t.Fatal(err)
	}
	if err := s.BeginExecutionSubmission(ctx, first, connection.Kind, target); err != nil {
		t.Fatalf("explicit HTTP rejection should allow a bounded retry: %v", err)
	}
	expireLease(t, s, ctx, job.ID)
	next := claimContentFixture(t, s, ctx, job)
	var pending *domain.DataError
	if err := s.BeginExecutionSubmission(ctx, next, connection.Kind, target); !errors.As(err, &pending) ||
		pending.Code != "execution_indexing_receipt_unknown" {
		t.Fatalf("an interrupted submission may be replayed: %v", err)
	}
}
