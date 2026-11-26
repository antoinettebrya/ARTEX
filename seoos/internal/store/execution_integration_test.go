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

func executionFixtureConnection(t *testing.T, s *Store, ctx context.Context, site domain.Site, rpm int) domain.ExecutionConnection {
	t.Helper()
	if err := s.SetProviderKey(bytes.Repeat([]byte{42}, 32)); err != nil {
		t.Fatal(err)
	}
	in := domain.ExecutionConnectionSave{ExecutionConnectionInput: domain.ExecutionConnectionInput{SiteID: site.ID, Kind: domain.ExecutionGeneric,
		Name: "approved cms", Enabled: true, AllowWrites: true, RequestsPerMinute: rpm, RequestsPerDay: 300, TimeoutSeconds: 5},
		Credential: &domain.ExecutionCredential{Secret: "fixture-private-token"}}
	c, err := s.SaveExecutionConnection(ctx, site.OrganizationID, "", in)
	if err != nil || c.ID == "" {
		t.Fatalf("create execution connection: %v", err)
	}
	return c
}

func TestPostgresExecutionConnectionScopeQuotaAndOAuthState(t *testing.T) {
	s, ctx, _ := openTestStore(t, true)
	org, other := testOrganization(t, s, ctx), testOrganization(t, s, ctx)
	owner := authenticatedContentRole(t, s, ctx, org, domain.RoleOwner)
	editor := authenticatedContentRole(t, s, ctx, org, domain.RoleEditor)
	a := newResearchTestSite(t, s, ctx, org, "https://execution-a.example.com")
	b := newResearchTestSite(t, s, ctx, other, "https://execution-b.example.com")
	c := executionFixtureConnection(t, s, owner, a, 1)
	if _, err := s.SaveExecutionConnection(editor, org, c.ID, domain.ExecutionConnectionSave{ExecutionConnectionInput: c.ExecutionConnectionInput,
		Version: c.Version}); !errors.Is(err, domain.ErrExecutionAuthority) {
		t.Fatalf("editor rewrote credentials: %v", err)
	}
	if _, _, err := s.ExecutionConnectionAccess(ctx, other, b.ID, c.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("tenant boundary: %v", err)
	}
	var stored []byte
	if err := s.db.QueryRowContext(ctx, `SELECT credential FROM public.seo_cms_connections WHERE id=$1`, c.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(stored, []byte("fixture-private-token")) {
		t.Fatal("credential persisted in plaintext")
	}
	if err := s.UseExecutionRequest(owner, org, a.ID, c.ID, c.Version, nil); err != nil {
		t.Fatal(err)
	}
	var quota *domain.DataError
	if err := s.UseExecutionRequest(owner, org, a.ID, c.ID, c.Version, nil); !errors.As(err, &quota) || quota.Code != "execution_budget" || quota.RetryAfter <= 0 {
		t.Fatalf("second request exceeded a real rolling budget: %v", err)
	}
	if err := s.UseExecutionRequest(owner, org, a.ID, c.ID, c.Version+1, nil); !errors.Is(err, domain.ErrExecutionAuthority) {
		t.Fatalf("version fence: %v", err)
	}
	check, _, err := s.EnqueueExecution(owner, org, domain.ExecutionJobInput{SiteID: a.ID, ConnectionID: c.ID,
		Kind: domain.ExecConnectionTest, IdempotencyKey: uuid.NewString()})
	if err != nil {
		t.Fatal(err)
	}
	lease := claimContentFixture(t, s, ctx, check)
	work, err := s.ExecutionJob(ctx, lease)
	if err != nil {
		t.Fatal(err)
	}
	info := domain.CMSSiteInfo{Name: "Authorized CMS", URL: a.BaseURL, Categories: []domain.CMSTerm{{ID: "6", Name: "Care"}},
		Capabilities: domain.CMSCapabilities{ReadPages: true}, Raw: json.RawMessage(`{"secret":"fixture-private-token"}`)}
	saved, err := s.SaveExecutionResult(ctx, lease, work, ExecutionResult{Info: &info})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.FinishJob(ctx, lease, domain.JobOutcome{Result: saved}); err != nil {
		t.Fatal(err)
	}
	reported, err := s.ExecutionRows(owner, org, "connections", domain.ExecutionFilter{SiteID: a.ID, Limit: 10})
	if err != nil || len(reported) != 1 || !bytes.Contains(reported[0], []byte(`"categories"`)) ||
		bytes.Contains(reported[0], []byte("fixture-private-token")) {
		t.Fatalf("connection status did not expose safe CMS site information: %v", err)
	}
	changed := validSiteInput()
	changed.BaseURL = "https://execution-a-new.example.com"
	if _, err := s.UpdateSite(ctx, org, a.ID, a.Version, changed); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.ExecutionConnectionAccess(ctx, org, a.ID, c.ID); !errors.Is(err, domain.ErrExecutionAuthority) {
		t.Fatalf("site edit inherited authorization: %v", err)
	}
	google, err := s.SaveExecutionConnection(owner, org, "", domain.ExecutionConnectionSave{ExecutionConnectionInput: domain.ExecutionConnectionInput{
		SiteID: a.ID, Kind: domain.ExecutionGSC, Enabled: true, Name: "Google", Property: "sc-domain:execution-a-new.example.com",
		RedirectURI: "https://dashboard.example.com/seo/execution",
	}, Credential: &domain.ExecutionCredential{ClientID: "test-client", ClientSecret: "test-secret"}})
	if err != nil {
		t.Fatal(err)
	}
	state, proof, err := s.OAuthState(owner, google)
	if err != nil || len(state) != 43 || len(proof) != 43 {
		t.Fatalf("OAuth state: %v", err)
	}
	if got, err := s.ConsumeOAuthState(owner, google, state); err != nil || got != proof {
		t.Fatalf("valid state did not resolve: %v", err)
	}
	if _, err := s.ConsumeOAuthState(owner, google, state); !errors.Is(err, domain.ErrExecutionAuthority) {
		t.Fatalf("OAuth replay: %v", err)
	}
}

func preparedExecutionFixture(t *testing.T, s *Store, ctx context.Context, site domain.Site, project domain.ContentProject, eligible bool) domain.ContentPublication {
	t.Helper()
	// Storage test fixture. The browser test earns its Phase 3 approval through
	// the full content, independent-review and owner-approval UI.
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	r, err := readContentRecord(ctx, tx, site.OrganizationID, project.ID, "FOR NO KEY UPDATE OF p")
	if err != nil {
		t.Fatal(err)
	}
	article := domain.ContentArticle{Title: "Filter care guide", H1: "Filter care guide", Language: "en", Slug: "care", Canonical: site.BaseURL + "/care",
		Sections: []domain.ArticleSection{{Level: 2, Heading: "Care", Body: "Clean the filter gently."}}}
	version, err := writeVersion(ctx, tx, r, false, article, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	r.state.ArticleVersion = version
	r.state.FactCheck = &domain.FactReport{Passed: true}
	r.state.Review = &domain.EditorialReview{Passed: true, Intent: true, Language: true, OriginalValue: true, Model: "independent"}
	r.state.Quality = &domain.ContentQuality{Passed: true, Score: 90}
	if err := preparePublication(ctx, tx, &r); err != nil {
		t.Fatal(err)
	}
	if !eligible {
		r.Status = "active"
	}
	if err := saveContentState(ctx, tx, r, "execution.fixture", nil); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var raw []byte
	if err := s.db.QueryRowContext(ctx, `SELECT data FROM public.seo_content_publications WHERE organization_id=$1 AND project_id=$2`, site.OrganizationID, project.ID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var result domain.ContentPublication
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestPostgresExecutionPublicationApprovalAndRecoveredWriteIntent(t *testing.T) {
	s, ctx, _ := openTestStore(t, true)
	org, other := testOrganization(t, s, ctx), testOrganization(t, s, ctx)
	site := newResearchTestSite(t, s, ctx, org, "https://publisher.example.com")
	foreign := newResearchTestSite(t, s, ctx, other, "https://foreign-publisher.example.com")
	owner := authenticatedContentRole(t, s, ctx, org, domain.RoleOwner)
	editor := authenticatedContentRole(t, s, ctx, org, domain.RoleEditor)
	serp := newResearchTestProvider(t, s, ctx, org, 12, 50000)
	seedContentResearch(t, s, ctx, site, serp, qaContentKeyword)
	seedContentResearch(t, s, ctx, site, serp, "another query")
	approved := preparedExecutionFixture(t, s, ctx, site, createContentFixture(t, s, ctx, site, qaContentKeyword), true)
	outdated := preparedExecutionFixture(t, s, ctx, site, createContentFixture(t, s, ctx, site, "another query"), false)
	c := executionFixtureConnection(t, s, owner, site, 40)
	in := domain.ExecutionPublicationInput{SiteID: site.ID, ConnectionID: c.ID, PackageID: outdated.ID, Resource: "post", IdempotencyKey: uuid.NewString()}
	if _, _, err := s.CreateExecutionPublication(owner, org, in); !errors.Is(err, domain.ErrContentGate) {
		t.Fatalf("stale gate released: %v", err)
	}
	in.PackageID = approved.ID
	r, created, err := s.CreateExecutionPublication(owner, org, in)
	if err != nil || !created || r.Status != domain.PublicationPending {
		t.Fatalf("publication create: %v %t %s", err, created, r.Status)
	}
	replayed, created, err := s.CreateExecutionPublication(owner, org, in)
	if err != nil || created || replayed.ID != r.ID {
		t.Fatalf("publication replay: %v", err)
	}
	if _, _, err := s.ExecutionPublication(ctx, foreign.OrganizationID, r.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("other tenant read publication: %v", err)
	}
	jobInput := domain.ExecutionJobInput{SiteID: site.ID, ConnectionID: c.ID, PublicationID: r.ID,
		Kind: domain.ExecPublication, Operation: "draft", IdempotencyKey: uuid.NewString()}
	if _, _, err := s.EnqueueExecution(editor, org, jobInput); !errors.Is(err, domain.ErrExecutionAuthority) {
		t.Fatalf("editor scheduled write: %v", err)
	}
	job, _, err := s.EnqueueExecution(owner, org, jobInput)
	if err != nil {
		t.Fatal(err)
	}
	parallel := jobInput
	parallel.IdempotencyKey = uuid.NewString()
	if _, _, err := s.EnqueueExecution(owner, org, parallel); !errors.Is(err, domain.ErrExecutionState) {
		t.Fatalf("parallel write: %v", err)
	}
	lease := claimContentFixture(t, s, ctx, job)
	work, err := s.ExecutionJob(ctx, lease)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.BeginPublication(ctx, lease, work, domain.PublicationPreparing); err != nil {
		t.Fatal(err)
	}
	if err = s.MarkExecutionWrite(ctx, lease, work, nil); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.ExecContext(ctx, `UPDATE public.seo_jobs SET lease_until=clock_timestamp()-interval '1 second' WHERE id=$1`, job.ID); err != nil {
		t.Fatal(err)
	}
	recovered := claimContentFixture(t, s, ctx, job)
	if _, err = s.ExecutionJob(ctx, lease); !errors.Is(err, domain.ErrLeaseLost) {
		t.Fatalf("obsolete lease read the write: %v", err)
	}
	work, err = s.ExecutionJob(ctx, recovered)
	if err != nil || !work.StartedWrite {
		t.Fatalf("recovery lost the write fence: %v", err)
	}
	if err = s.MarkExecutionWrite(ctx, recovered, work, nil); !errors.Is(err, domain.ErrExecutionState) {
		t.Fatalf("recovery started a second mutation: %v", err)
	}
	cms := domain.CMSPage{ID: "123", Status: "draft", Resource: "post", Title: "Test", Body: "<p>Text</p>",
		URL: site.BaseURL + "/care", Raw: json.RawMessage(`{"content":"private authenticated receipt"}`)}
	data, err := s.CompletePublication(ctx, recovered, work, domain.PublicationDraft, "", &cms, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.FinishJob(ctx, recovered, domain.JobOutcome{Result: data}); err != nil {
		t.Fatal(err)
	}
	var encrypted []byte
	if err = s.db.QueryRowContext(ctx, `SELECT raw_encrypted FROM public.seo_cms_publication_versions WHERE publication_id=$1`, r.ID).Scan(&encrypted); err != nil {
		t.Fatal(err)
	}
	if len(encrypted) == 0 || bytes.Contains(encrypted, []byte("private authenticated receipt")) {
		t.Fatal("raw CMS receipt was not encrypted")
	}
	publish := jobInput
	publish.IdempotencyKey, publish.Operation = uuid.NewString(), "publish"
	if _, _, err := s.EnqueueExecution(owner, org, publish); err == nil {
		t.Fatal("published without explicit approval")
	}
	publish.Note = "Approved for this isolated fixture"
	scheduledAt := time.Now().UTC().Add(2 * time.Hour).Truncate(time.Second)
	publish.ScheduledAt = &scheduledAt
	publishJob, _, err := s.EnqueueExecution(owner, org, publish)
	if err != nil {
		t.Fatal(err)
	}
	publishLease := claimContentFixture(t, s, ctx, publishJob)
	publishWork, err := s.ExecutionJob(ctx, publishLease)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.MarkExecutionWrite(ctx, publishLease, publishWork, &cms); err != nil {
		t.Fatal(err)
	}
	updated := cms
	updated.Status, updated.ScheduledAt = "future", &scheduledAt
	data, err = s.CompletePublication(ctx, publishLease, publishWork, domain.PublicationScheduled, "", &updated, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.FinishJob(ctx, publishLease, domain.JobOutcome{Result: data}); err != nil {
		t.Fatal(err)
	}
	live, _, err := s.ExecutionPublication(ctx, org, r.ID)
	if err != nil || live.VerificationJobID == "" || live.Status != domain.PublicationScheduled {
		t.Fatalf("scheduled publication lost automatic verification: %#v %v", live, err)
	}
	verification, err := s.GetJob(ctx, org, live.VerificationJobID)
	if err != nil || verification.State != "queued" || !verification.AvailableAt.Equal(scheduledAt) {
		t.Fatalf("verification should wait until the native CMS schedule: %#v %v", verification, err)
	}
	rollback := jobInput
	rollback.IdempotencyKey, rollback.Operation, rollback.Note = uuid.NewString(), "rollback", "Restore the scheduled draft"
	rollbackJob, _, err := s.EnqueueExecution(owner, org, rollback)
	if err != nil {
		t.Fatalf("owner could not roll back before scheduled verification: %v", err)
	}
	verification, err = s.GetJob(ctx, org, verification.ID)
	if err != nil || verification.State != "cancelled" || verification.ErrorCode != "superseded_by_rollback" {
		t.Fatalf("the queued verification was not superseded: %#v %v", verification, err)
	}
	if _, err = s.CancelJob(ctx, org, rollbackJob.ID); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresExecutionDailyFeedbackUsesObservedCoverage(t *testing.T) {
	s, ctx, _ := openTestStore(t, true)
	org := testOrganization(t, s, ctx)
	owner := authenticatedContentRole(t, s, ctx, org, domain.RoleOwner)
	site := newResearchTestSite(t, s, ctx, org, "https://publisher-metrics.example.com")
	p := newResearchTestProvider(t, s, ctx, org, 12, 50000)
	seedContentResearch(t, s, ctx, site, p, qaContentKeyword)
	project := createContentFixture(t, s, ctx, site, qaContentKeyword)
	if _, err := s.db.ExecContext(ctx, `UPDATE public.seo_content_projects SET data=jsonb_build_object('strategy',jsonb_build_object('target_url',$2::text))
		WHERE id=$1`, project.ID, site.BaseURL+"/care"); err != nil {
		t.Fatal(err)
	}
	c, err := s.SaveExecutionConnection(owner, org, "", domain.ExecutionConnectionSave{ExecutionConnectionInput: domain.ExecutionConnectionInput{
		SiteID: site.ID, Kind: domain.ExecutionGA4, Name: "GA4", Enabled: true, Property: "77",
	}, Credential: &domain.ExecutionCredential{ClientID: "test-client", ClientSecret: "test-secret"}})
	if err != nil {
		t.Fatal(err)
	}
	report := domain.ExecutionSync{}
	now := time.Now().UTC()
	for i := 0; i < 14; i++ {
		n := 100.0
		if i >= 7 {
			n = 200
		}
		report.Metrics = append(report.Metrics, domain.ExecutionMetric{Date: now.AddDate(0, 0, -i).Format(time.DateOnly),
			Page: "/care", Country: "US", Values: map[string]float64{"sessions": n}})
	}
	queued, _, err := s.EnqueueExecution(owner, org, domain.ExecutionJobInput{Kind: domain.ExecGA4Sync, SiteID: site.ID,
		ConnectionID: c.ID, IdempotencyKey: uuid.NewString()})
	if err != nil {
		t.Fatal(err)
	}
	lease := claimContentFixture(t, s, ctx, queued)
	work, err := s.ExecutionJob(ctx, lease)
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.SaveExecutionResult(ctx, lease, work, ExecutionResult{Sync: &report})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.FinishJob(ctx, lease, domain.JobOutcome{Result: result}); err != nil {
		t.Fatal(err)
	}
	w, err := s.ContentWorkspace(ctx, org, project.ID)
	if err != nil || w.RefreshSignals.TrafficChange == nil || *w.RefreshSignals.TrafficChange != -50 {
		t.Fatalf("actual analytics window did not reach refresh: %#v %v", w.RefreshSignals, err)
	}
	rows, err := s.ExecutionRows(owner, org, "metrics", domain.ExecutionFilter{SiteID: site.ID, Kind: domain.ExecutionGA4, Country: "us", Page: site.BaseURL + "/care", Limit: 50})
	if err != nil || len(rows) != 14 {
		t.Fatalf("scoped analytics filter: %d %v", len(rows), err)
	}
}
