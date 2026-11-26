package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Autumn-27/artex/seoos/internal/contentengine"
	"github.com/Autumn-27/artex/seoos/internal/domain"
	"github.com/google/uuid"
)

const qaContentKeyword = "robot vacuum maintenance"

func seedContentResearch(t *testing.T, s *Store, ctx context.Context, site domain.Site, provider domain.Provider, term string) string {
	t.Helper()
	in := domain.ResearchInput{SiteID: site.ID, ProviderID: provider.ID, Kind: domain.SERPAnalysis,
		Market: researchTestMarket, Keyword: term, IdempotencyKey: uuid.NewString()}
	job := enqueueResearchTest(t, s, ctx, site.OrganizationID, in)
	lease, scope := claimResearch(t, s, ctx, job)
	request := domain.DataRequest{Operation: domain.DataSERP, Market: in.Market, Keyword: term}
	plan := domain.DataPlan{ReserveMicros: 1000, TTL: time.Hour}
	res, err := s.ReserveData(ctx, lease, scope, request, plan)
	if err != nil || res.ID == "" {
		t.Fatalf("reserve SERP fixture: %v", err)
	}
	cost := int64(100)
	body := domain.DataResponse{TaskID: uuid.NewString(), Raw: json.RawMessage(`{"fixture":"organic SERP"}`),
		Source: "dataforseo", CostMicros: &cost, CollectedAt: time.Now().UTC(),
		Results: []domain.SERPEntry{{Rank: 1, URL: site.BaseURL + "/care", Title: "Robot vacuum maintenance guide", PageType: "guide"}},
	}
	if err := s.SettleData(ctx, lease, res.ID, body, plan, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.PersistData(ctx, lease, scope, domain.DataSERP, res.ID, body, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.FinishJob(ctx, lease, domain.JobOutcome{Result: json.RawMessage(`{}`)}); err != nil {
		t.Fatal(err)
	}
	return res.ID
}

func createContentFixture(t *testing.T, s *Store, ctx context.Context, site domain.Site, term string) domain.ContentProject {
	t.Helper()
	project, created, err := s.CreateContentProject(ctx, site.OrganizationID, domain.ContentProjectInput{
		SiteID: site.ID, Keyword: term, Market: researchTestMarket,
	})
	if err != nil || !created || project.Revision != 1 {
		t.Fatalf("create content project: %#v, created=%t, err=%v", project, created, err)
	}
	return project
}

func authenticatedContentRole(t *testing.T, s *Store, ctx context.Context, org string, role domain.Role) context.Context {
	t.Helper()
	receipt, err := s.AddMember(ctx, org, uuid.NewString()+"@editorial.example.com", role)
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.Authenticate(ctx, sha256.Sum256([]byte(receipt.Token)))
	if err != nil || p.OrganizationID != org || p.Role != role || p.UserID == "" {
		t.Fatalf("could not authenticate %s in fixture organization: %v", role, err)
	}
	return domain.WithPrincipal(ctx, p)
}

func claimContentFixture(t *testing.T, s *Store, ctx context.Context, want domain.Job) domain.Lease {
	t.Helper()
	lease, err := s.ClaimJob(ctx)
	if err != nil || lease == nil || lease.Job.ID != want.ID {
		t.Fatalf("claim expected job %s: %v", want.ID, err)
	}
	return *lease
}

func contentQueueFixture(t *testing.T, s *Store, ctx context.Context, org string, input domain.ContentJobInput) domain.Job {
	t.Helper()
	job, created, err := s.EnqueueContent(ctx, org, input)
	if err != nil || !created {
		t.Fatalf("submit %s: created=%t, err=%v", input.Kind, created, err)
	}
	return job
}

func TestPostgresContentProjectScopeAndInventoryCompatibility(t *testing.T) {
	s, ctx, _ := openTestStore(t, true)
	org, other := testOrganization(t, s, ctx), testOrganization(t, s, ctx)
	site := newResearchTestSite(t, s, ctx, org, "https://content-a.example.com")
	sibling := newResearchTestSite(t, s, ctx, org, "https://content-b.example.com")
	foreign := newResearchTestSite(t, s, ctx, other, site.BaseURL)
	in := domain.ContentProjectInput{SiteID: site.ID, Keyword: qaContentKeyword, Market: researchTestMarket}
	if _, _, err := s.CreateContentProject(ctx, org, in); !errors.Is(err, domain.ErrContentPrerequisite) {
		t.Fatalf("content created before Phase 2 association: %v", err)
	}
	provider := newResearchTestProvider(t, s, ctx, org, 12, 50000)
	snapshot := seedContentResearch(t, s, ctx, site, provider, in.Keyword)
	project := createContentFixture(t, s, ctx, site, in.Keyword)
	if duplicate, created, err := s.CreateContentProject(ctx, org, in); err != nil || created || duplicate.ID != project.ID {
		t.Fatalf("same site, market and keyword duplicated content project: %#v %t %v", duplicate, created, err)
	}
	w, err := s.ContentWorkspace(ctx, org, project.ID)
	if err != nil || w.Evidence.SERPSnapshotID != snapshot || len(w.Evidence.Keyword.SERP) != 1 ||
		w.Project.SiteID != site.ID || w.Evidence.Keyword.Term != in.Keyword {
		t.Fatalf("content did not inherit the exact Phase 2 evidence: %#v %v", w.Evidence, err)
	}
	if _, err := s.ContentWorkspace(ctx, other, project.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("other tenant read the content project: %v", err)
	}
	for _, missingSite := range []domain.Site{sibling, foreign} {
		copy := in
		copy.SiteID = missingSite.ID
		if _, _, err := s.CreateContentProject(ctx, missingSite.OrganizationID, copy); !errors.Is(err, domain.ErrContentPrerequisite) {
			t.Fatalf("another site inherited the Phase 2 association: %v", err)
		}
	}
	wrongMarket := in
	wrongMarket.Market.Device = "mobile"
	if _, _, err := s.CreateContentProject(ctx, org, wrongMarket); !errors.Is(err, domain.ErrContentPrerequisite) {
		t.Fatalf("another device inherited the Phase 2 association: %v", err)
	}
	if _, _, err := s.EnqueueContent(ctx, org, domain.ContentJobInput{SiteID: sibling.ID, ProjectID: project.ID,
		Kind: domain.ContentPlan, IdempotencyKey: uuid.NewString()}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("content job was associated with another site: %v", err)
	}

	legacy, created, err := s.EnqueueJob(ctx, org, domain.JobInput{SiteID: site.ID, Kind: domain.JobSiteInventory, IdempotencyKey: uuid.NewString()})
	if err != nil || !created {
		t.Fatalf("legacy inventory: %v", err)
	}
	inventory := domain.ContentJobInput{SiteID: site.ID, Kind: domain.ContentInventory, IdempotencyKey: legacy.IdempotencyKey}
	if _, _, err := s.EnqueueContent(ctx, org, inventory); !errors.Is(err, domain.ErrIdempotencyConflict) {
		t.Fatalf("content inventory reused legacy job key: %v", err)
	}
	legacyLease := claimContentFixture(t, s, ctx, legacy)
	if _, isContent, err := s.ContentJob(ctx, legacyLease); err != nil || isContent {
		t.Fatalf("legacy inventory changed dispatch behavior: %v", err)
	}
	if err := s.FinishJob(ctx, legacyLease, domain.JobOutcome{Result: json.RawMessage(`{}`)}); err != nil {
		t.Fatal(err)
	}
	inventory.IdempotencyKey = uuid.NewString()
	enriched := contentQueueFixture(t, s, ctx, org, inventory)
	if _, _, err := s.EnqueueJob(ctx, org, enriched.JobInput); !errors.Is(err, domain.ErrIdempotencyConflict) {
		t.Fatalf("legacy inventory reused content job key: %v", err)
	}
	if same, created, err := s.EnqueueContent(ctx, org, inventory); err != nil || created || same.ID != enriched.ID {
		t.Fatalf("content inventory replay created a different job: %v", err)
	}
	if _, err := s.CancelJob(ctx, org, enriched.ID); err != nil {
		t.Fatal(err)
	}

	secondTerm := "robot vacuum replacement parts"
	seedContentResearch(t, s, ctx, site, provider, secondTerm)
	another := createContentFixture(t, s, ctx, site, secondTerm)
	planJob := contentQueueFixture(t, s, ctx, org, domain.ContentJobInput{SiteID: site.ID, ProjectID: project.ID,
		Kind: domain.ContentPlan, IdempotencyKey: uuid.NewString()})
	planLease := claimContentFixture(t, s, ctx, planJob)
	if _, err := s.SaveContentStage(ctx, planLease, another.ID, another.Revision, domain.ContentStageResult{
		Strategy: &domain.ContentStrategy{Action: "create", TargetURL: site.BaseURL + "/care"},
	}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("an active lease modified an unbound same-site project: %v", err)
	}
	member := authenticatedContentRole(t, s, ctx, other, domain.RoleEditor)
	if _, err := s.ApplyContentAction(member, org, project.ID, domain.ContentAction{Revision: project.Revision,
		Action: "save_signals", Signals: &domain.RefreshSignals{SourceChanged: true}}); !errors.Is(err, domain.ErrContentForbidden) {
		t.Fatalf("an external principal passed an explicit foreign org parameter: %v", err)
	}
}

func contentModelFixture(t *testing.T, s *Store, ctx context.Context, org string, rpm int) (domain.Provider, domain.ContentModelProfile) {
	t.Helper()
	if err := s.SetProviderKey(bytes.Repeat([]byte{75}, 32)); err != nil {
		t.Fatal(err)
	}
	p, err := s.SaveProvider(ctx, org, "", domain.ProviderInput{Kind: domain.ProviderOpenAI, APIKey: "fixture-content-key",
		ProviderSettings: domain.ProviderSettings{Name: "Local model fixture", Model: "test-model",
			RequestsPerMinute: rpm, RequestsPerDay: 20, MaxOutputTokens: 256, TimeoutSeconds: 10, Enabled: true}})
	if err != nil {
		t.Fatal(err)
	}
	profile := domain.ContentModelProfile{ProviderID: p.ID, Role: "any", MaxInputTokens: 1024, DailyTokens: 10000,
		DailyCostMicros: 20000, InputMicrosPerMillion: 1000000, OutputMicrosPerMillion: 2000000, MaxRetries: 1, RetrySeconds: 2}
	if _, err := s.SaveContentModel(ctx, org, profile); err != nil {
		t.Fatal(err)
	}
	return p, profile
}

func TestPostgresContentVersionRestoreReviewAndPublicationGate(t *testing.T) {
	s, ctx, _ := openTestStore(t, true)
	org := testOrganization(t, s, ctx)
	site := newResearchTestSite(t, s, ctx, org, "https://content-editor.example.com")
	serpProvider := newResearchTestProvider(t, s, ctx, org, 12, 50000)
	seedContentResearch(t, s, ctx, site, serpProvider, qaContentKeyword)
	project := createContentFixture(t, s, ctx, site, qaContentKeyword)
	editor := authenticatedContentRole(t, s, ctx, org, domain.RoleEditor)
	owner := authenticatedContentRole(t, s, ctx, org, domain.RoleOwner)

	apply := func(member context.Context, action domain.ContentAction) domain.ContentWorkspace {
		t.Helper()
		w, err := s.ApplyContentAction(member, org, project.ID, action)
		if err != nil {
			t.Fatalf("%s: %v", action.Action, err)
		}
		return w
	}
	w := domain.ContentWorkspace{Project: project}
	save := func(member context.Context, action domain.ContentAction) {
		t.Helper()
		action.Revision = w.Project.Revision
		w = apply(member, action)
	}
	saveStage := func(kind, provider string, result domain.ContentStageResult) {
		t.Helper()
		job := contentQueueFixture(t, s, ctx, org, domain.ContentJobInput{SiteID: site.ID, ProjectID: project.ID,
			Kind: kind, ProviderID: provider, IdempotencyKey: uuid.NewString()})
		lease := claimContentFixture(t, s, ctx, job)
		receipt, err := s.SaveContentStage(ctx, lease, project.ID, w.Project.Revision, result)
		if err != nil {
			t.Fatalf("apply stage %s: %v", kind, err)
		}
		if err := s.FinishJob(ctx, lease, domain.JobOutcome{Result: receipt}); err != nil {
			t.Fatal(err)
		}
		w, err = s.ContentWork(ctx, org, project.ID)
		if err != nil {
			t.Fatal(err)
		}
	}
	headings := []domain.ContentHeading{{Level: 2, Heading: "What to check"}, {Level: 2, Heading: "Practical steps"}}
	brief := domain.ContentBrief{PrimaryKeyword: project.Keyword, Country: project.Market.CountryCode, Language: project.Market.LanguageCode,
		Intent: "informational", PageType: "guide", URL: site.BaseURL + "/care", Title: project.Keyword + ": a practical guide",
		Description: "Learn how to maintain the filter and check the results.", H1: project.Keyword, Headings: headings,
		Topics: []string{"filter maintenance"},
	}
	save(editor, domain.ContentAction{Action: "save_brief", Brief: &brief})
	baseRevision := w.Project.Revision
	changed := brief
	changed.Title = "Comparing filter care techniques"
	save(editor, domain.ContentAction{Action: "save_brief", Brief: &changed})
	if _, err := s.ApplyContentAction(editor, org, project.ID, domain.ContentAction{Action: "restore_brief", Revision: baseRevision,
		Version: 1}); !errors.Is(err, domain.ErrVersionConflict) {
		t.Fatalf("stale editor wrote over a later revision: %v", err)
	}
	save(editor, domain.ContentAction{Action: "restore_brief", Version: 1})
	if w.BriefVersion != 3 || w.Brief == nil || w.Brief.Title != brief.Title {
		t.Fatalf("brief restore failed: version=%d, brief=%#v", w.BriefVersion, w.Brief)
	}

	const claimText = "Replace the robot vacuum filter every 90 days."
	now := time.Now().UTC()
	source := domain.SourceDocument{ContentSource: domain.ContentSource{ID: uuid.NewString(),
		URL: "https://official.example.com/guide", Kind: "official", Title: "Fixture Guide",
		Status: "collected", Fingerprint: "fixture", CollectedAt: &now}, Text: claimText}
	facts := contentengine.Facts([]domain.SourceDocument{source}, project.Market)
	saveStage(domain.ContentResearch, "", domain.ContentStageResult{Sources: []domain.SourceDocument{source}, Facts: facts})
	if len(w.Facts) != 1 || w.Facts[0].Evidence[0].SourceID != source.ID {
		t.Fatalf("source provenance was not persisted: %#v", w.Facts)
	}
	article := domain.ContentArticle{Language: project.Market.LanguageCode, Title: brief.Title, H1: brief.H1,
		Description: brief.Description, Canonical: brief.URL, Slug: "care", Author: "Fixture editor",
		Sections: []domain.ArticleSection{
			{Heading: headings[0].Heading, Level: 2, Body: "Disconnect the robot before lifting the filter cover. Read the manual and look for visible dirt around the cover before removing it. Keep your cloth nearby to collect debris as you work."},
			{Heading: headings[1].Heading, Level: 2, Body: claimText + " Hold the filter over a tray and remove loose dust with a dry cloth. Refit the cover and check that it closes fully before reconnecting the robot.",
				Claims: []domain.ContentClaim{{Text: claimText, FactIDs: []string{w.Facts[0].ID}}}},
		},
	}
	save(editor, domain.ContentAction{Action: "save_article", Article: &article})
	edited := article
	edited.Title = "Draft change to robot vacuum maintenance"
	save(editor, domain.ContentAction{Action: "save_article", Article: &edited})
	save(editor, domain.ContentAction{Action: "restore_article", Version: 1})
	if w.ArticleVersion != 3 || w.Article == nil || w.Article.Title != article.Title ||
		!reflect.DeepEqual(w.Article.Sections, article.Sections) || len(w.History) != 6 {
		t.Fatalf("article restore lost version, sections or history: %d history=%d", w.ArticleVersion, len(w.History))
	}
	for _, name := range []string{"approve", "prepare_publication"} {
		action := domain.ContentAction{Revision: w.Project.Revision, Action: name, Note: "Fixture editorial approval"}
		if _, err := s.ApplyContentAction(editor, org, project.ID, action); !errors.Is(err, domain.ErrContentForbidden) {
			t.Fatalf("editor could %s: %v", name, err)
		}
		if _, err := s.ApplyContentAction(owner, org, project.ID, action); !errors.Is(err, domain.ErrContentGate) {
			t.Fatalf("owner bypassed the quality gate for %s: %v", name, err)
		}
	}
	check := contentengine.FactCheck(*w.Article, w.Facts)
	if !check.Passed {
		t.Fatalf("local source fact should pass: %#v", check)
	}
	saveStage(domain.ContentFactCheck, "", domain.ContentStageResult{FactCheck: &check})
	reviewer, _ := contentModelFixture(t, s, ctx, org, 12)
	review := &domain.EditorialReview{Model: "fixture-reviewer", Passed: true, Intent: true, Language: true, OriginalValue: true}
	report := contentengine.Quality(w, review)
	if !report.Passed {
		t.Fatalf("fixture really failed quality rules: %v", report.Issues)
	}
	saveStage(domain.ContentReview, reviewer.ID, domain.ContentStageResult{Review: review, Quality: &report})
	if _, err := s.ApplyContentAction(owner, org, project.ID, domain.ContentAction{Revision: w.Project.Revision,
		Action: "prepare_publication"}); !errors.Is(err, domain.ErrContentGate) {
		t.Fatalf("passing review implicitly approved a release: %v", err)
	}
	save(owner, domain.ContentAction{Action: "approve", Note: "Fixture editorial approval"})
	save(owner, domain.ContentAction{Action: "prepare_publication"})
	if len(w.Publications) != 1 || w.Publications[0].Version != 3 || w.Publications[0].Status != "prepared" ||
		w.Publications[0].Article.Title != article.Title || len(w.Publications[0].Checksum) != 64 {
		t.Fatalf("invalid CMS payload: %#v", w.Publications)
	}
	save(editor, domain.ContentAction{Action: "restore_article", Version: 2})
	if w.Quality != nil || w.FactCheck != nil || len(w.Publications) != 1 || w.Publications[0].Version != 3 {
		t.Fatal("restored version inherited the old approval or rewrote the archived CMS payload")
	}
}

func TestPostgresContentModelLimitsAndLeaseRecovery(t *testing.T) {
	s, ctx, _ := openTestStore(t, true)
	org := testOrganization(t, s, ctx)
	site := newResearchTestSite(t, s, ctx, org, "https://content-budget.example.com")
	serp := newResearchTestProvider(t, s, ctx, org, 12, 50000)
	seedContentResearch(t, s, ctx, site, serp, qaContentKeyword)
	project := createContentFixture(t, s, ctx, site, qaContentKeyword)
	provider, profile := contentModelFixture(t, s, ctx, org, 1)

	legacy, created, err := s.EnqueueJob(ctx, org, domain.JobInput{SiteID: site.ID, Kind: domain.JobSiteBrief,
		ProviderID: provider.ID, IdempotencyKey: uuid.NewString()})
	if err != nil || !created {
		t.Fatalf("submit site-brief quota fixture: %v", err)
	}
	legacyLease := claimContentFixture(t, s, ctx, legacy)
	if call, err := s.ReserveProviderCall(ctx, legacyLease); err != nil || call.APIKey == "" {
		t.Fatalf("reserve legacy job's request: %v", err)
	}
	if err := s.FinishJob(ctx, legacyLease, domain.JobOutcome{Result: json.RawMessage(`{}`)}); err != nil {
		t.Fatal(err)
	}
	req := domain.ContentModelRequest{Stage: "outline", Language: "en", System: "Use verified evidence.",
		Input: json.RawMessage(`{"query":"robot vacuum maintenance"}`)}
	job := contentQueueFixture(t, s, ctx, org, domain.ContentJobInput{SiteID: site.ID, ProjectID: project.ID,
		Kind: domain.ContentOutline, ProviderID: provider.ID, IdempotencyKey: uuid.NewString()})
	lease := claimContentFixture(t, s, ctx, job)
	if _, err := s.ReserveContentModel(ctx, lease, "outline", req, uuid.NewString()); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unbound project acquired a provider request: %v", err)
	}
	rate, err := s.ReserveContentModel(ctx, lease, "outline", req, project.ID)
	if err != nil || rate.ID != "" || rate.APIKey != "" || !rate.DeferUntil.After(time.Now()) {
		t.Fatalf("content bypassed the inherited per-minute limit: %v", err)
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE public.seo_provider_calls SET reserved_at=clock_timestamp()-interval '2 minutes'
		WHERE organization_id=$1 AND job_id=$2`, org, legacy.ID); err != nil {
		t.Fatal(err)
	}
	large := req
	large.Input, err = json.Marshal(strings.Repeat("evidence", 200))
	if err != nil {
		t.Fatal(err)
	}
	var problem *domain.DataError
	if res, err := s.ReserveContentModel(ctx, lease, "oversized", large, project.ID); !errors.As(err, &problem) ||
		problem.Code != "content_model_input_budget" || res.APIKey != "" {
		t.Fatalf("oversized input received credentials: %v", err)
	}
	low := profile
	low.DailyCostMicros = 1
	if _, err := s.SaveContentModel(ctx, org, low); err != nil {
		t.Fatal(err)
	}
	if res, err := s.ReserveContentModel(ctx, lease, "outline", req, project.ID); !errors.As(err, &problem) ||
		problem.Code != "content_model_budget_too_low" || res.APIKey != "" {
		t.Fatalf("over-budget call received credentials: %v", err)
	}
	if _, err := s.SaveContentModel(ctx, org, profile); err != nil {
		t.Fatal(err)
	}
	res, err := s.ReserveContentModel(ctx, lease, "outline", req, project.ID)
	if err != nil || res.ID == "" || res.APIKey == "" || res.Cached != nil {
		t.Fatalf("reserve model call: %v", err)
	}
	usage := func() (requests, tokens, micros, reserved int64) {
		t.Helper()
		rows, err := s.ContentUsage(ctx, org)
		if err != nil || len(rows) != 1 {
			t.Fatalf("model usage: %v", err)
		}
		var row struct {
			Requests int64 `json:"requests"`
			Tokens   int64 `json:"used_tokens"`
			Micros   int64 `json:"spent_micros"`
			Reserved int64 `json:"reserved_tokens"`
		}
		if err := json.Unmarshal(rows[0], &row); err != nil {
			t.Fatal(err)
		}
		return row.Requests, row.Tokens, row.Micros, row.Reserved
	}
	if requests, tokens, micros, reserved := usage(); requests != 1 || tokens != 0 || micros != 0 || reserved <= 256 {
		t.Fatalf("call wasn't provisionally accounted: requests=%d, tokens=%d, micros=%d, reserved=%d", requests, tokens, micros, reserved)
	}
	answer := domain.ContentModelResponse{Text: `[{"level":2,"heading":"Practical checks"}]`, Model: provider.Model,
		InputTokens: 120, OutputTokens: 45}
	if err := s.SettleContentModel(ctx, lease, res, answer, nil); err != nil {
		t.Fatal(err)
	}
	if requests, tokens, micros, reserved := usage(); requests != 1 || tokens != 165 || micros != 210 || reserved != 0 {
		t.Fatalf("incorrect settled usage: requests=%d, tokens=%d, micros=%d, reserved=%d", requests, tokens, micros, reserved)
	}
	expireLease(t, s, ctx, job.ID)
	next := claimContentFixture(t, s, ctx, job)
	if next.Token == lease.Token || next.Job.Attempts != 2 {
		t.Fatal("expired model lease was not recovered")
	}
	saved, err := s.ReserveContentModel(ctx, next, "outline", req, project.ID)
	if err != nil || saved.Cached == nil || saved.Cached.ReceiptID != res.ID || saved.Cached.Text != answer.Text || saved.APIKey != "" {
		t.Fatalf("recovery did not reuse the settled checkpoint: %v", err)
	}
	if requests, tokens, micros, _ := usage(); requests != 1 || tokens != 165 || micros != 210 {
		t.Fatal("replaying the checkpoint changed billing")
	}
	if err := s.FinishJob(ctx, lease, domain.JobOutcome{Result: json.RawMessage(`{}`)}); !errors.Is(err, domain.ErrLeaseLost) {
		t.Fatalf("stale worker wrote the result: %v", err)
	}

	legacy.IdempotencyKey = uuid.NewString()
	followUp, created, err := s.EnqueueJob(ctx, org, legacy.JobInput)
	if err != nil || !created {
		t.Fatalf("submit second site brief: %v", err)
	}
	followLease := claimContentFixture(t, s, ctx, followUp)
	if call, err := s.ReserveProviderCall(ctx, followLease); err != nil || call.APIKey != "" || !call.DeferUntil.After(time.Now()) {
		t.Fatalf("legacy provider code bypassed a content reservation: %v", err)
	}
	if err := s.FinishJob(ctx, followLease, domain.JobOutcome{Result: json.RawMessage(`{}`)}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE public.seo_content_model_calls SET reserved_at=clock_timestamp()-interval '2 minutes'
		WHERE organization_id=$1 AND id=$2`, org, res.ID); err != nil {
		t.Fatal(err)
	}
	inflight, err := s.ReserveContentModel(ctx, next, "section", req, project.ID)
	if err != nil || inflight.ID == "" {
		t.Fatalf("reserve second checkpoint: %v", err)
	}
	expireLease(t, s, ctx, job.ID)
	last := claimContentFixture(t, s, ctx, job)
	if retry, err := s.ReserveContentModel(ctx, last, "section", req, project.ID); !errors.As(err, &problem) ||
		problem.Code != "content_model_receipt_unknown" || retry.APIKey != "" {
		t.Fatalf("recovery retried an ambiguous charge: %v", err)
	}
	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM public.seo_content_model_calls
		WHERE organization_id=$1 AND job_id=$2`, org, job.ID).Scan(&count); err != nil || count != 2 {
		t.Fatalf("checkpoint recovery inserted another reservation: %d, %v", count, err)
	}
}

func TestPostgresInventorySnapshotsDeduplicateAndRespectRevocation(t *testing.T) {
	s, ctx, _ := openTestStore(t, true)
	org, outsider := testOrganization(t, s, ctx), testOrganization(t, s, ctx)
	site := newResearchTestSite(t, s, ctx, org, "https://content-snapshot.example.com")
	owner := authenticatedContentRole(t, s, ctx, org, domain.RoleOwner)
	policy := domain.CrawlPolicy{SiteID: site.ID, Connector: "sitemap", SitemapURL: site.BaseURL + "/sitemap.xml", MaxPages: 10, Enabled: true}
	if _, err := s.SaveCrawlPolicy(owner, site, policy); err != nil {
		t.Fatal(err)
	}
	discover := contentQueueFixture(t, s, ctx, org, domain.ContentJobInput{SiteID: site.ID,
		Kind: domain.ContentInventory, IdempotencyKey: uuid.NewString()})
	lease := claimContentFixture(t, s, ctx, discover)
	address := site.BaseURL + "/guide"
	batch := domain.InventoryBatch{Entries: []domain.InventoryEntry{{URL: address, Kind: "guide"}}}
	for range 2 {
		if count, err := s.SaveInventoryBatch(ctx, lease, policy, batch); err != nil || count != 1 {
			t.Fatalf("persist inventory discovery retry: %d %v", count, err)
		}
	}
	rows, err := s.ContentInventory(ctx, org, site.ID, 10, 0)
	if err != nil || len(rows) != 1 || rows[0].State != "discovered" || rows[0].IndexStatus != "unverified" || rows[0].SnapshotID != "" {
		t.Fatalf("discovery implied indexing or collection: %#v %v", rows, err)
	}
	if err := s.FinishJob(ctx, lease, domain.JobOutcome{Result: json.RawMessage(`{}`)}); err != nil {
		t.Fatal(err)
	}
	jobs, err := s.ContentJobs(ctx, org, site.ID, "", 10, 0)
	if err != nil || len(jobs) != 2 {
		t.Fatalf("discovery generated duplicate extraction jobs: %d %v", len(jobs), err)
	}
	var extraction domain.Job
	for _, row := range jobs {
		if row.Request.Kind == domain.ContentExtract && row.Request.URL == address {
			extraction = row.Job
		}
	}
	lease = claimContentFixture(t, s, ctx, extraction)
	page := domain.SitePage{State: "collected", IndexStatus: "indexed", Language: "en", HTTPStatus: 200,
		Fingerprint: "fixture-one", Body: "Captured local HTML text, first observation.",
		PageEvidence: domain.PageEvidence{URL: address, Title: "Stored guide"}}
	id, err := s.SaveInventoryPage(ctx, lease, policy, page)
	if err != nil {
		t.Fatal(err)
	}
	first, err := s.InventoryPage(ctx, org, site.ID, id)
	if err != nil || first.Body != page.Body || first.IndexStatus != "unverified" || !domain.ValidID(first.SnapshotID) {
		t.Fatalf("unexpected captured evidence: %#v %v", first, err)
	}
	if _, err := s.InventoryPage(ctx, outsider, site.ID, id); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("foreign tenant read captured content: %v", err)
	}
	page.Unchanged = true
	if got, err := s.SaveInventoryPage(ctx, lease, policy, page); err != nil || got != id {
		t.Fatalf("identical observation duplicated the inventory entry: %s %v", got, err)
	}
	latest, err := s.InventoryPage(ctx, org, site.ID, id)
	if err != nil || latest.SnapshotID != first.SnapshotID {
		t.Fatalf("identical fingerprint duplicated history: %#v %v", latest, err)
	}
	revoked := policy
	revoked.Enabled = false
	if _, err := s.SaveCrawlPolicy(owner, site, revoked); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveInventoryPage(ctx, lease, policy, page); !errors.Is(err, domain.ErrCrawlUnauthorized) {
		t.Fatalf("revoked lease wrote observation: %v", err)
	}
}
