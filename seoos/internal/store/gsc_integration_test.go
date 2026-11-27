package store

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/Autumn-27/artex/seoos/internal/domain"
	"github.com/google/uuid"
)

func TestPostgresGSCAtomicDailySnapshotsAndReclaimedCursor(t *testing.T) {
	s, ctx, _ := openTestStore(t, true)
	if err := s.SetProviderKey(bytes.Repeat([]byte{42}, 32)); err != nil {
		t.Fatal(err)
	}
	org, other := testOrganization(t, s, ctx), testOrganization(t, s, ctx)
	site := newResearchTestSite(t, s, ctx, org, "https://gsc-cursor.example.com")
	owner := authenticatedContentRole(t, s, ctx, org, domain.RoleOwner)
	connection, err := s.SaveExecutionConnection(owner, org, "", domain.ExecutionConnectionSave{
		ExecutionConnectionInput: domain.ExecutionConnectionInput{
			SiteID: site.ID, Kind: domain.ExecutionGSC, Enabled: true, Name: "Google", Property: "sc-domain:gsc-cursor.example.com",
			RedirectURI: "https://dashboard.example.com/seo/execution",
		},
		Credential: &domain.ExecutionCredential{ClientID: "test-client", ClientSecret: "test-secret"},
	})
	if err != nil {
		t.Fatal(err)
	}
	from := time.Now().UTC().AddDate(0, 0, -5).Format(time.DateOnly)
	to := time.Now().UTC().AddDate(0, 0, -4).Format(time.DateOnly)
	input := domain.ExecutionJobInput{Kind: domain.ExecGSCSync, SiteID: site.ID, ConnectionID: connection.ID,
		From: from, To: to}
	start := func() (domain.Lease, domain.ExecutionWork) {
		t.Helper()
		input.IdempotencyKey = uuid.NewString()
		job, _, err := s.EnqueueExecution(owner, org, input)
		if err != nil {
			t.Fatal(err)
		}
		lease := claimContentFixture(t, s, ctx, job)
		work, err := s.ExecutionJob(ctx, lease)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.StartGSCDays(ctx, lease, connection, input.From, input.To); err != nil {
			t.Fatal(err)
		}
		return lease, work
	}
	save := func(lease domain.Lease, work domain.ExecutionWork, expected domain.GSCCursor, rows []domain.ExecutionMetric) {
		t.Helper()
		cursor, err := s.NextGSCDay(ctx, org, lease.Job.ID)
		if err != nil || cursor == nil || *cursor != expected {
			t.Fatalf("unexpected cursor: %+v want %+v: %v", cursor, expected, err)
		}
		if err := s.SaveGSCPage(ctx, lease, work, *cursor, rows); err != nil {
			t.Fatal(err)
		}
	}
	finish := func(lease domain.Lease) {
		t.Helper()
		if err := s.FinishJob(ctx, lease, domain.JobOutcome{Result: json.RawMessage(`{"ready":true}`)}); err != nil {
			t.Fatal(err)
		}
	}
	countMetrics := func(org, site, from, to string, count int) {
		t.Helper()
		rows, err := s.ExecutionRows(ctx, org, "metrics", domain.ExecutionFilter{SiteID: site,
			Kind: domain.ExecutionGSC, From: from, To: to, Limit: 200})
		if err != nil || len(rows) != count {
			t.Fatalf("daily rows: %d want %d (%v)", len(rows), count, err)
		}
	}
	lease, work := start()
	totals := domain.ExecutionMetric{Date: from, Values: map[string]float64{"clicks": 3, "impressions": 10, "position": 4}}
	save(lease, work, domain.GSCCursor{Day: from}, []domain.ExecutionMetric{totals})
	full := make([]domain.ExecutionMetric, domain.GSCPageSize)
	for i := range full {
		full[i] = domain.ExecutionMetric{Date: from, Page: site.BaseURL + "/page", Query: fmt.Sprintf("query %d", i),
			Values: map[string]float64{"clicks": 1, "impressions": 2}}
	}
	save(lease, work, domain.GSCCursor{Day: from, TotalsChecked: true}, full)
	countMetrics(org, site.ID, from, to, 0)
	if _, err := s.db.ExecContext(ctx, "UPDATE public.seo_jobs SET lease_until=clock_timestamp()-interval '1 second' WHERE id=$1", lease.Job.ID); err != nil {
		t.Fatal(err)
	}
	reclaimed := claimContentFixture(t, s, ctx, lease.Job)
	save(reclaimed, work, domain.GSCCursor{Day: from, TotalsChecked: true, NextRow: domain.GSCPageSize}, nil)
	zero := domain.ExecutionMetric{Date: to, Values: map[string]float64{"clicks": 0, "impressions": 0}}
	save(reclaimed, work, domain.GSCCursor{Day: to}, []domain.ExecutionMetric{zero})
	save(reclaimed, work, domain.GSCCursor{Day: to, TotalsChecked: true}, nil)
	finish(reclaimed)
	progress, err := s.GSCProgress(ctx, org, reclaimed.Job.ID)
	if err != nil || progress.Complete != 2 || progress.Rows != domain.GSCPageSize {
		t.Fatalf("wrong completed window: %+v %v", progress, err)
	}
	var count int
	if err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM public.seo_gsc_daily_metrics WHERE organization_id=$1", org).Scan(&count); err != nil || count != domain.GSCPageSize {
		t.Fatalf("partial pages were published incorrectly: %d %v", count, err)
	}
	dashboard, err := s.Dashboard(ctx, org, domain.SiteScope{SiteID: site.ID}, false)
	if err != nil {
		t.Fatal(err)
	}
	var chart struct {
		GSC struct {
			Clicks  float64 `json:"clicks"`
			Covered int     `json:"covered_days"`
		} `json:"gsc"`
		AI *json.RawMessage `json:"ai"`
	}
	if err := json.Unmarshal(dashboard, &chart); err != nil || chart.GSC.Clicks != 3 || chart.GSC.Covered != 2 || chart.AI != nil {
		t.Fatalf("dashboard summed query detail or leaked owner data: %s", dashboard)
	}

	// A newer empty refresh removes details. A later failed day is distinct
	// from the explicit zero collected in the other day.
	input.To = from
	refresh, refreshWork := start()
	save(refresh, refreshWork, domain.GSCCursor{Day: from}, nil)
	save(refresh, refreshWork, domain.GSCCursor{Day: from, TotalsChecked: true}, nil)
	finish(refresh)
	countMetrics(org, site.ID, from, to, 0)
	input.From, input.To = to, to
	failed, _ := start()
	if err := s.FinishJob(ctx, failed, domain.JobOutcome{Code: "execution_upstream_forbidden"}); err != nil {
		t.Fatal(err)
	}
	rows, err := s.ExecutionRows(ctx, org, "snapshots", domain.ExecutionFilter{SiteID: site.ID, Limit: 20})
	if err != nil || len(rows) != 2 {
		t.Fatalf("snapshots: %s %v", rows, err)
	}
	var latest struct {
		State       string `json:"state"`
		Error       string `json:"error_code"`
		TotalsState string `json:"totals_state"`
	}
	if err := json.Unmarshal(rows[0], &latest); err != nil || latest.State != "failed" ||
		latest.Error != "execution_upstream_forbidden" || latest.TotalsState != "unknown" {
		t.Fatalf("latest daily error hidden: %s", rows[0])
	}
	if _, err := s.ExecutionRows(ctx, other, "snapshots", domain.ExecutionFilter{SiteID: site.ID, Limit: 20}); err == nil {
		t.Fatal("read another tenant's daily snapshots")
	}
	found, err := s.ListJobs(context.Background(), org, 1, 0, domain.JobFilter{
		SiteScope: domain.SiteScope{SiteID: site.ID}, State: "failed", Kind: domain.ExecGSCSync,
	})
	if err != nil || len(found) != 1 || found[0].ID != failed.Job.ID {
		t.Fatalf("server-side filters returned the wrong job: %+v %v", found, err)
	}
}
