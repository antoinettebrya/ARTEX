package contentengine

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Autumn-27/artex/seoos/internal/domain"
	"github.com/google/uuid"
)

const (
	fixtureSite   = "https://phase3.example.com"
	fixtureURL    = fixtureSite + "/existing"
	fixtureTerm   = "robot vacuum maintenance"
	fixtureClaim  = "Replace the robot vacuum filter every 90 days."
	fixtureSource = "https://official.example.com/robot-vacuum-guide"
)

func fixtureWorkspace() domain.ContentWorkspace {
	now := time.Now().UTC()
	older := now.AddDate(-3, 0, 0)
	market := domain.Market{CountryCode: "US", LocationCode: 2840, LanguageCode: "en", SearchEngine: "google", Device: "desktop"}
	project := domain.ContentProject{ContentProjectInput: domain.ContentProjectInput{Keyword: fixtureTerm, Market: market}}
	asset := domain.SitePage{
		HTTPStatus: 200, Language: "en-US", State: "collected", IndexStatus: "unverified", SnapshotID: uuid.NewString(),
		Body:         "Dust accumulates in the old housing. Clean the machine after a busy room. Keep the original instructions near the base.",
		PageEvidence: domain.PageEvidence{URL: fixtureURL, Title: "Robot vacuum maintenance", PageType: "guide", LastModified: &older},
	}
	w := domain.ContentWorkspace{SiteURL: fixtureSite, Project: project, Assets: []domain.SitePage{asset}}
	w.Evidence = domain.ContentEvidence{
		SERPSnapshotID: "recorded-serp-id",
		Keyword: domain.KeywordEvidence{
			ID: "recorded-keyword-id", Term: fixtureTerm,
			SERP:   []domain.SERPEntry{{Rank: 1, URL: "https://competitor.example.com/robot-vacuum-guide", PageType: "guide"}, {Rank: 2, URL: fixtureURL, PageType: "guide"}},
			Intent: &domain.SearchIntent{Primary: "informational", PageType: "guide", Sources: []string{"organic_serp_page_types"}},
			MetricSources: map[string]domain.MetricProvenance{
				"search_volume": {SnapshotID: "recorded-metric-id", Source: "kws", CollectedAt: now},
			},
		},
		Mapping: &domain.URLMapping{Action: "update", TargetURL: fixtureURL, Reasons: []string{"Observed site result"}},
		Clusters: []domain.KeywordCluster{
			{ID: "recorded-cluster-id", PrimaryKeyword: fixtureTerm, Secondary: []string{"robot vacuum filter cleaning"}},
		},
		Opportunity: &domain.Opportunity{Version: "v2", KnownPoints: 65, Coverage: 80},
		Competitors: []domain.ContentCompetitor{
			{SnapshotID: "recorded-competitor-id", Gaps: []string{"Explain how readers can verify a successful cleaning."},
				Page: domain.PageEvidence{URL: "https://competitor.example.com/robot-vacuum-guide", Entities: []string{"filter"}, Topics: []string{"housing inspection"}}},
		},
	}
	source := domain.ContentSource{
		ID: "fixture-first-party", URL: fixtureSource, Kind: "official", Title: "Local fixture guide",
		CollectedAt: &now, PublishedAt: &now, Fingerprint: "captured-fingerprint",
	}
	w.Sources = []domain.ContentSource{source}
	w.Facts = Facts([]domain.SourceDocument{{ContentSource: source, Text: fixtureClaim}}, market)
	return w
}

func TestStrategyUsesScopedPhaseTwoResearch(t *testing.T) {
	w := fixtureWorkspace()
	strategy, err := Strategy(w)
	if err != nil || strategy.Action != "update" || strategy.TargetURL != fixtureURL || !strategy.TopicCluster {
		t.Fatalf("strategy = %#v, %v", strategy, err)
	}
	got := strings.Join(strategy.Evidence, "\n")
	for _, evidence := range []string{"serp_snapshot:recorded-serp-id", "metric:search_volume:recorded-metric-id",
		"keyword_cluster:recorded-cluster-id", "competitor_snapshot:recorded-competitor-id", "opportunity:v2", "site_snapshot:"} {
		if !strings.Contains(got, evidence) {
			t.Errorf("missing %s in %q", evidence, got)
		}
	}
	w.Evidence.SERPSnapshotID = ""
	if _, err := Strategy(w); !errors.Is(err, domain.ErrContentPrerequisite) {
		t.Fatalf("accepted missing recorded SERP: %v", err)
	}
}

func TestStrategyDistinguishesNewTargetAdjustedTypeAndCannibalization(t *testing.T) {
	w := fixtureWorkspace()
	w.Assets[0].PageType = "product"
	decision, err := Strategy(w)
	if err != nil || decision.Action != "adjust_type" {
		t.Fatalf("expected page-type correction, got %#v, %v", decision, err)
	}
	w.Assets[0].PageType = "guide"
	another := w.Assets[0]
	another.URL = fixtureSite + "/reference"
	another.Body = "Different supporting text highlights user needs and devices."
	w.Assets = append(w.Assets, another)
	w.Evidence.Mapping = &domain.URLMapping{TargetURL: fixtureURL, Action: "merge", Cannibalizing: []string{fixtureURL, another.URL, "https://different.example.com/guide"}}
	decision, err = Strategy(w)
	if err != nil || decision.Action != "merge" || strings.Contains(strings.Join(decision.Evidence, " "), "different.example.com") {
		t.Fatalf("expected scoped merge evidence, got %#v, %v", decision, err)
	}
	w.Evidence.Mapping.CanonicalIssue = true
	decision, err = Strategy(w)
	if err != nil || decision.Action != "hold" {
		t.Fatalf("canonical problem did not pause the plan: %#v, %v", decision, err)
	}
	w.Evidence.Mapping = nil
	w.Evidence.Keyword.SERP = w.Evidence.Keyword.SERP[:1]
	w.Assets = w.Assets[:1]
	w.Assets[0].Title, w.Assets[0].Description = "About the site", ""
	decision, err = Strategy(w)
	if err != nil || decision.Action != "create" || decision.TargetURL != fixtureSite+"/robot-vacuum-maintenance" {
		t.Fatalf("expected evidence-based create, got %#v, %v", decision, err)
	}
}

func TestBriefUsesNativeLocaleAndEvidence(t *testing.T) {
	for _, test := range []struct{ lang, country, term, needle string }{
		{"en", "US", fixtureTerm, "practical guide"},
		{"zh-CN", "CN", "扫地机器人保养", "实用指南"},
		{"ja", "JP", "ロボット掃除機のお手入れ", "実践ガイド"},
		{"th", "TH", "การดูแลหุ่นยนต์ดูดฝุ่น", "คู่มือใช้งานจริง"},
		{"my", "MM", "စက်ရုပ်ဖုန်စုပ်စက် ပြုပြင်ထိန်းသိမ်းမှု", "လက်တွေ့လမ်းညွှန်"},
		{"km", "KH", "ការថែទាំម៉ាស៊ីនបូមធូលីរ៉ូបូត", "មគ្គុទ្ទេសក៍"},
	} {
		t.Run(test.lang, func(t *testing.T) {
			w := fixtureWorkspace()
			w.Project.Market.CountryCode, w.Project.Market.LanguageCode = test.country, test.lang
			w.Project.Keyword, w.Evidence.Keyword.Term = test.term, test.term
			w.Assets[0].Language = test.lang
			w.Evidence.Clusters[0].MemberIDs = []string{w.Evidence.Keyword.ID}
			brief, err := Brief(w)
			if err != nil || brief.PrimaryKeyword != test.term || brief.Language != test.lang || brief.Country != test.country ||
				!strings.Contains(brief.Title, test.needle) || !strings.Contains(brief.Title, test.term) || len(brief.Headings) != 3 || len(brief.FactRequirements) != 2 {
				t.Fatalf("brief=%#v error=%v", brief, err)
			}
			if len(brief.SourceURLs) != 1 || brief.SourceURLs[0] != fixtureSource || len(brief.Gaps) != 1 || len(brief.Entities) != 1 {
				t.Fatalf("lost evidence: %#v", brief)
			}
		})
	}
}

func TestBriefUsesObservedIntentForNativeOutline(t *testing.T) {
	w := fixtureWorkspace()
	w.Project.Market.CountryCode, w.Project.Market.LanguageCode = "JP", "ja"
	w.Project.Keyword, w.Evidence.Keyword.Term = "ロボット掃除機のお手入れ", "ロボット掃除機のお手入れ"
	w.Assets[0].Language = "ja"
	w.Evidence.Keyword.Intent = &domain.SearchIntent{Primary: "commercial", PageType: "comparison"}
	brief, err := Brief(w)
	if err != nil || brief.Intent != "commercial" || brief.PageType != "comparison" || !strings.Contains(brief.Headings[0].Heading, "選択肢を比較する") {
		t.Fatalf("brief missed market intent: %#v, %v", brief, err)
	}
}

func TestLanguageQualitySignals(t *testing.T) {
	for _, test := range []struct{ code, text string }{
		{"en", "Check the filter housing before taking the vacuum apart."},
		{"zh", "先关闭扫地机器人，再检查滤网外壳是否有积尘。"},
		{"ja", "掃除機の電源を切り、フィルターに付いたほこりを拭き取ります。"},
		{"th", "ปิดเครื่องดูดฝุ่นก่อนตรวจดูว่ามีฝุ่นติดอยู่ที่ตัวกรองหรือไม่"},
		{"my", "ဖုန်စုပ်စက်ကို ပိတ်ပြီး စစ်ထုတ်ကိရိယာတွင် ဖုန်ရှိမရှိ စစ်ဆေးပါ။"},
		{"km", "បិទម៉ាស៊ីនបូមធូលីមុនពេលពិនិត្យមើលធូលីនៅលើតម្រង។"},
	} {
		t.Run(test.code, func(t *testing.T) {
			if score := scriptRatio(test.text, test.code); score < .7 {
				t.Fatalf("native writing fell below the script threshold: %0.2f", score)
			}
			if test.code != "en" && scriptRatio("Check the filter housing before starting", test.code) >= .52 {
				t.Fatal("an English draft met a different locale's script check")
			}
		})
	}
}

func TestFactsBoundSourceTrustConflictAndFreshness(t *testing.T) {
	w := fixtureWorkspace()
	if len(w.Facts) != 1 || w.Facts[0].Statement != fixtureClaim || w.Facts[0].Status != factVerified ||
		!domain.ValidID(w.Facts[0].ID) || w.Facts[0].Evidence[0].SourceID != w.Sources[0].ID {
		t.Fatalf("facts: %#v", w.Facts)
	}
	source, second := w.Sources[0], w.Sources[0]
	second.ID = "second-source"
	second.URL = "https://other-official.example.com/specification"
	withConflicts := Facts([]domain.SourceDocument{
		{ContentSource: source, Text: fixtureClaim}, {ContentSource: second, Text: strings.Replace(fixtureClaim, "90", "60", 1)},
		{ContentSource: domain.ContentSource{ID: "competitor", URL: "https://competitor.example.com/analysis", Kind: "competitor", CollectedAt: source.CollectedAt},
			Text: "The product weighs 1 ton."},
	}, w.Project.Market)
	if len(withConflicts) != 2 {
		t.Fatalf("unexpected candidates: %#v", withConflicts)
	}
	for _, fact := range withConflicts {
		if fact.Status != factConflict || len(fact.Evidence) != 2 || !fact.Evidence[1].Conflict {
			t.Fatalf("missed contradictory values: %#v", fact)
		}
	}
	lastYear := time.Now().UTC().AddDate(-3, 0, 0)
	source.PublishedAt = &lastYear
	old := Facts([]domain.SourceDocument{{ContentSource: source, Text: "The current replacement price is $12."}}, w.Project.Market)
	if len(old) != 1 || old[0].Status != factStale {
		t.Fatalf("undetected stale policy/price: %#v", old)
	}
	source.Kind = "authorized"
	partial := Facts([]domain.SourceDocument{{ContentSource: source, Text: fixtureClaim}}, w.Project.Market)
	if len(partial) != 1 || partial[0].Status != factPartial {
		t.Fatalf("weak corroboration must remain partial: %#v", partial)
	}
}

func reviewedFixture(t *testing.T) domain.ContentWorkspace {
	t.Helper()
	w := fixtureWorkspace()
	strategy, err := Strategy(w)
	if err != nil {
		t.Fatal(err)
	}
	w.Strategy = &strategy
	brief, err := Brief(w)
	if err != nil {
		t.Fatal(err)
	}
	w.Brief = &brief
	a := domain.ContentArticle{Language: "en", Author: "Fixture editor"}
	for _, heading := range brief.Headings {
		a.Sections = append(a.Sections, domain.ArticleSection{Heading: heading.Heading, Level: heading.Level})
	}
	a.Sections[0].Body = "Switch off the robot vacuum before opening the filter housing. Check the housing and lift out trapped dust with a dry cloth. Put a clean container nearby to catch debris as the dustbin comes free."
	a.Sections[1].Body = fixtureClaim + " Follow the product manual before washing removable parts. Seat the clean filter firmly and close the cover before reconnecting the vacuum."
	a.Sections[1].Claims = []domain.ContentClaim{{Text: fixtureClaim, FactIDs: []string{w.Facts[0].ID}}}
	a.Sections[2].Body = "Check the result by looking for gaps around the filter. After closing the cover, run a short cleaning cycle and listen for changes. Pause maintenance if the housing is loose."
	a = Optimize(a, brief, w.Assets)
	w.Article = &a
	w.Review = &domain.EditorialReview{Model: "independent-fixture-model", Passed: true, Intent: true, Language: true, OriginalValue: true}
	return w
}

func TestFactCheckBindsClaimsToRenderedContent(t *testing.T) {
	w := reviewedFixture(t)
	report := FactCheck(*w.Article, w.Facts)
	if !report.Passed || len(report.Claims) != 1 || report.Claims[0].Status != factVerified {
		t.Fatalf("fixture fact report = %#v", report)
	}
	w.Article.Sections[0].Body += " Replace the belt every 35 days."
	report = FactCheck(*w.Article, w.Facts)
	if report.Passed || len(report.Claims) != 2 || report.Claims[0].Status != factUnknown {
		t.Fatalf("unmarked numerical assertion escaped review: %#v", report)
	}
	w = reviewedFixture(t)
	part := &w.Article.Sections[1]
	part.Body = strings.Replace(part.Body, "90", "30", 1)
	part.Claims[0].Text = strings.Replace(part.Claims[0].Text, "90", "30", 1)
	report = FactCheck(*w.Article, w.Facts)
	if report.Passed || len(report.Claims) != 1 || !strings.Contains(report.Claims[0].Reason, "does not support") {
		t.Fatalf("known ID with altered number escaped review: %#v", report)
	}
	w = reviewedFixture(t)
	w.Article.Sections[1].Body = "Additional practical commentary."
	report = FactCheck(*w.Article, w.Facts)
	if report.Passed || len(report.Claims) != 1 || !strings.Contains(report.Claims[0].Reason, "not attached") {
		t.Fatalf("a floating citation escaped review: %#v", report)
	}
}

func TestQualityGateAndExistingDuplicate(t *testing.T) {
	w := reviewedFixture(t)
	report := Quality(w, w.Review)
	if !report.Passed || report.Score < 75 || len(report.Dimensions) != 12 || report.Algorithm == "" {
		t.Fatalf("fixture should pass actual rules. Quality: %#v", report)
	}
	if report := Quality(w, nil); report.Passed || !strings.Contains(strings.Join(report.Issues, " "), "Independent editorial") {
		t.Fatalf("quality passed without independent review: %#v", report)
	}
	stale := time.Now().UTC().AddDate(0, -7, 0)
	w.Sources[0].CollectedAt = &stale
	if report := Quality(w, w.Review); report.Passed || !strings.Contains(strings.Join(report.Issues, " "), "material assertions") {
		t.Fatalf("quality passed after the evidence expired: %#v", report)
	}
	w = reviewedFixture(t)
	// Comparing the captured target to its own updated draft is not a duplicate.
	w.Assets[0].Body = articleText(*w.Article)
	if report := Quality(w, w.Review); !report.Passed {
		t.Fatalf("comparing to self produced a duplicate: %#v", report)
	}
	duplicate := w.Assets[0]
	duplicate.URL = fixtureSite + "/published-elsewhere"
	w.Assets = append(w.Assets, duplicate)
	if report := Quality(w, w.Review); report.Passed || !strings.Contains(strings.Join(report.Issues, " "), "Possible duplicate") {
		t.Fatalf("duplicate passed despite another page in this site: %#v", report)
	}
	duplicate.URL = "https://not-the-authorized-site.example.com/copy"
	w.Assets[1] = duplicate
	if report := Quality(w, w.Review); !report.Passed {
		t.Fatalf("cross-site data affected quality: %#v", report)
	}
	w.Article.Canonical = "https://unauthorized.example.com/guide"
	if report := Quality(w, w.Review); report.Passed {
		t.Fatal("unsafe canonical passed")
	}
}

func TestOptimizePreservesAuthoredSectionAndCitations(t *testing.T) {
	w := reviewedFixture(t)
	article := *w.Article
	article.Title = "  "
	article.Description = strings.Repeat(" long description ", 25)
	article.Canonical = "https://unrelated.example.com"
	article.Sections[0].Level = 5
	wantBody := article.Sections[1].Body
	wantClaims := article.Sections[1].Claims
	linked := domain.SitePage{HTTPStatus: 200, Language: "en",
		PageEvidence: domain.PageEvidence{URL: fixtureSite + "/related", Title: "Robot vacuum filter maintenance and comparisons"}}
	w.Assets = append(w.Assets, linked)
	optimized := Optimize(article, *w.Brief, w.Assets)
	if optimized.Title != w.Brief.Title || optimized.Canonical != fixtureURL || len(optimized.InternalLinks) != 1 ||
		optimized.InternalLinks[0] != linked.URL || optimized.Sections[0].Level != 2 || article.Sections[0].Level != 5 ||
		optimized.Sections[1].Body != wantBody || !reflect.DeepEqual(optimized.Sections[1].Claims, wantClaims) {
		t.Fatalf("unexpected optimization: %#v", optimized)
	}
}

func TestRefreshRetainsSourceSnapshotAndExplainsSignal(t *testing.T) {
	w := reviewedFixture(t)
	r, err := Refresh(w)
	if err != nil || r.Action != "minor_update" || r.SnapshotID != w.Assets[0].SnapshotID {
		t.Fatalf("old page did not get preservation-guided update: %#v, error=%v", r, err)
	}
	w.RefreshSignals = domain.RefreshSignals{IntentChanged: true, EvidenceURL: fixtureSource}
	r, err = Refresh(w)
	if err != nil || r.Action != "rewrite" || !strings.Contains(strings.Join(r.Reasons, " "), fixtureSource) {
		t.Fatalf("observed intent change lacked rewrite or evidence: %#v, error=%v", r, err)
	}
	w.Assets[0].Robots = "noindex, follow"
	r, err = Refresh(w)
	if err != nil || r.Action != "review_noindex" {
		t.Fatalf("noindex change should be an editorial recommendation: %#v, error=%v", r, err)
	}
	w.Assets[0].SnapshotID = ""
	if _, err := Refresh(w); !errors.Is(err, domain.ErrContentPrerequisite) {
		t.Fatalf("refresh without original snapshot accepted: %v", err)
	}
}
