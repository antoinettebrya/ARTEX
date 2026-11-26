package researchengine

import (
	"fmt"
	"math"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/Autumn-27/artex/seoos/internal/domain"
	"github.com/google/uuid"
)

func TestPageTypeUsesMainPageSignals(t *testing.T) {
	page := domain.PageEvidence{URL: "https://example.com/reviews/robot-vacuum", Title: "Robot vacuum reviews", Schema: []string{"FAQPage"}}
	if got := PageType(page); got != pageComparison {
		t.Fatalf("the presence of an FAQ section changed the page type to %q", got)
	}
	page.Schema = []string{"FAQPage", "Product"}
	if got := PageType(page); got != pageProduct {
		t.Fatalf("the declared product schema did not inform the page type: %q", got)
	}
}

func ptr[T any](value T) *T { return &value }

func serpURLs(host string, pages ...string) []domain.SERPEntry {
	out := make([]domain.SERPEntry, 0, len(pages))
	for i, p := range pages {
		out = append(out, domain.SERPEntry{Rank: i + 1, URL: host + "/" + p, PageType: pageComparison})
	}
	return out
}

func TestClassifyMultilingualIntent(t *testing.T) {
	cases := []struct {
		term, label, landing string
	}{
		{"how to plant lavender", informational, pageGuide},
		{"best coffee grinders", commercial, pageComparison},
		{"购买跑步鞋", transactional, pageProduct},
		{"buchen.example", navigational, pageHome},
		{"フラワーショップ 近く", local, pageLocal},
		{"woocommerce 公式サイト", navigational, pageHome},
		{"lavender shop near me", local, pageLocal},
		{"barista mühlen vergleich", commercial, pageComparison},
	}
	for _, tc := range cases {
		t.Run(tc.term, func(t *testing.T) {
			result := Classify(domain.KeywordEvidence{Term: tc.term}, nil)
			if result.Primary != tc.label || result.PageType != tc.landing || result.Need == "" || len(result.Reasons) == 0 {
				t.Fatalf("classify %q = %+v, want %s and %s", tc.term, result, tc.label, tc.landing)
			}
			if !slices.Contains(result.Sources, "keyword_rules") || result.Confidence < 0.35 || result.Confidence > 0.62 {
				t.Fatalf("unexpected keyword-only evidence: %+v", result)
			}
		})
	}
}

func TestClassifyUsesRankedPageAndVendorEvidence(t *testing.T) {
	term := domain.KeywordEvidence{
		Term:   "latte",
		Metric: domain.KeywordMetric{ProviderIntent: "informational"},
		SERP: []domain.SERPEntry{
			{Rank: 1, URL: "https://one.example/latte", Title: "Latte"},
			{Rank: 2, URL: "https://two.example/product/latte"},
			{Rank: 3, URL: "https://three.example/latte", PageType: "Product Detail"},
			{Rank: 4, URL: "https://four.example/latte", PageType: "Product Detail"},
			{Rank: 5, URL: "https://five.example/learn/how-to-make-latte"},
			{Rank: 11, URL: "https://six.example/latte", PageType: "Guide"},
		},
		Pages: []domain.PageEvidence{
			{URL: "https://one.example/latte", Schema: []string{"Product"}},
			{URL: "https://missing.example/latte", Schema: []string{"Article"}},
		},
	}
	llm := &domain.SearchIntent{Primary: transactional, Confidence: 0.9}
	result := Classify(term, llm)
	if result.Primary != transactional || result.Secondary != informational || result.Confidence <= 0.62 {
		t.Fatalf("combined evidence produced %+v", result)
	}
	for _, source := range []string{"organic_serp_page_types", "captured_page_evidence", "data_provider_intent", "llm_corroboration"} {
		if !slices.Contains(result.Sources, source) {
			t.Fatalf("missing source %q in %+v", source, result)
		}
	}
}

func TestClassifyCapsUnsupportedSignals(t *testing.T) {
	llm := &domain.SearchIntent{Primary: transactional, Confidence: 1}
	empty := Classify(domain.KeywordEvidence{Term: "button"}, llm)
	if empty.Primary == transactional || empty.Confidence > 0.2 || len(empty.Sources) != 0 {
		t.Fatalf("the model alone changed the outcome: %+v", empty)
	}
	phrase := Classify(domain.KeywordEvidence{Term: "define mountain trail"}, llm)
	if phrase.Primary == transactional || slices.Contains(phrase.Sources, "llm_corroboration") {
		t.Fatalf("unsupported model result affected intent: %+v", phrase)
	}
}

func TestClusterRequiresIndependentOrganicOverlap(t *testing.T) {
	const own = "https://media.example/jp"
	first := domain.KeywordEvidence{
		ID: "parent", Term: "ランニングシューズ 比較", Metric: domain.KeywordMetric{SearchVolume: ptr(int64(800))},
		SERP:  serpURLs(own, "best-2026?utm_medium=email", "first", "second"),
		Pages: []domain.PageEvidence{{URL: own + "/first", Entities: []string{"ランニング"}}},
	}
	second := domain.KeywordEvidence{
		ID: "long", Term: "初心者向けランニングシューズ比較おすすめ", Metric: domain.KeywordMetric{SearchVolume: ptr(int64(120))},
		SERP:  serpURLs(own, "best-2026?utm_source=organic", "first", "second"),
		Pages: []domain.PageEvidence{{URL: own + "/second", Entities: []string{"ランニング"}}},
	}
	incompatible := domain.KeywordEvidence{
		ID: "other", Term: "女性ランニングシューズ比較", Metric: domain.KeywordMetric{SearchVolume: ptr(int64(200))},
		SERP: serpURLs("https://elsewhere.example", "foo", "bar", "abc"),
	}
	missing := domain.KeywordEvidence{ID: "unobserved", Term: "比較 ランニングシューズ"}
	clusters := Cluster([]domain.KeywordEvidence{second, incompatible, missing, first}, own)
	if len(clusters) != 3 || clusters[0].PrimaryKeyword != first.Term || len(clusters[0].LongTail) != 1 || clusters[0].LongTail[0] != second.Term {
		t.Fatalf("unexpected grouping: %+v", clusters)
	}
	if got := clusters[0]; got.RecommendedURL != own+"/best-2026" || got.Confidence <= 0.36 || uuid.Validate(got.ID) != nil ||
		!slices.Equal(got.MemberIDs, []string{"long", "parent"}) || len(got.Reasons) < 3 {
		t.Fatalf("unexpected evidence: %+v", got)
	}
	for i := 1; i < len(clusters); i++ {
		if len(clusters[i].MemberIDs) != 1 || clusters[i].Confidence != 0 {
			t.Fatalf("a semantic-only match joined a cluster: %+v", clusters[i])
		}
	}
	stable := Cluster([]domain.KeywordEvidence{missing, first, incompatible, second}, own)
	if !reflect.DeepEqual(clusters, stable) {
		t.Fatalf("input order changed clusters: %+v versus %+v", clusters, stable)
	}
}

func TestClusterDoesNotMergeThroughSingleBridge(t *testing.T) {
	const host = "https://comparison.example"
	a := domain.KeywordEvidence{ID: "a", Term: "best shoe brands", SERP: serpURLs(host, "1", "2", "3"), Metric: domain.KeywordMetric{SearchVolume: ptr(int64(9))}}
	b := domain.KeywordEvidence{ID: "b", Term: "best trail shoe brands", SERP: serpURLs(host, "1", "2", "3", "4", "5"), Metric: domain.KeywordMetric{SearchVolume: ptr(int64(7))}}
	c := domain.KeywordEvidence{ID: "c", Term: "best hiking shoe brands", SERP: serpURLs(host, "3", "4", "5"), Metric: domain.KeywordMetric{SearchVolume: ptr(int64(4))}}
	clusters := Cluster([]domain.KeywordEvidence{c, b, a}, host)
	if len(clusters) != 2 || !slices.Equal(clusters[0].MemberIDs, []string{"a", "b"}) || !slices.Equal(clusters[1].MemberIDs, []string{"c"}) {
		t.Fatalf("bridge joined incompatible evidence: %+v", clusters)
	}
}

func TestMapScopeAndInventoryCoverage(t *testing.T) {
	const own = "https://www.example.test/store"
	e := domain.KeywordEvidence{
		Term: "compare coffee grinders",
		SERP: []domain.SERPEntry{
			{Rank: 1, URL: "https://www.example.test.evil.test/store/products/coffee"},
			{Rank: 2, URL: "https://www.example.test/storeroom/grinders"},
			{Rank: 3, URL: "https://www.example.test/store/../unscoped"},
			{Rank: 4, URL: "http://www.example.test/store/grinders"},
		},
	}
	for _, tc := range []struct {
		pages  []domain.PageEvidence
		action string
	}{
		{[]domain.PageEvidence{{URL: "https://www.example.test/admin", Title: "Compare coffee grinders"}}, "inventory_missing"},
		{[]domain.PageEvidence{{URL: own + "/index", Title: "Office desks"}}, "create"},
		{[]domain.PageEvidence{{URL: own + "/grinders", Title: "Compare coffee grinders", PageType: pageComparison}}, "update"},
	} {
		result := Map(e, own, tc.pages)
		if result.Action != tc.action || len(result.Reasons) == 0 {
			t.Fatalf("scope did not preserve action %q: %+v", tc.action, result)
		}
		if tc.action == "update" && result.TargetURL != own+"/grinders" {
			t.Fatalf("expected actual scoped page: %+v", result)
		}
	}
}

func TestMapDistinguishesOverlapAndCanonicals(t *testing.T) {
	const own = "https://www.example.test/store"
	e := domain.KeywordEvidence{Term: "compare coffee grinders", SERP: serpURLs(own, "grinders", "guide")}
	primary := domain.PageEvidence{URL: own + "/grinders", Title: "Compare Coffee Grinders", PageType: pageComparison}
	duplicate := domain.PageEvidence{URL: own + "/guide", Title: "Compare Coffee Grinders with our Guide", PageType: pageComparison}
	conflict := Map(e, own, []domain.PageEvidence{primary, duplicate})
	if conflict.Action != "merge" || conflict.TargetURL != primary.URL || len(conflict.Cannibalizing) != 2 || conflict.CanonicalIssue {
		t.Fatalf("co-ranking duplicate handling: %+v", conflict)
	}
	noRank := e
	noRank.SERP = []domain.SERPEntry{e.SERP[0]}
	potential := Map(noRank, own, []domain.PageEvidence{primary, duplicate})
	if potential.Action != "update" || len(potential.Cannibalizing) != 0 || !strings.Contains(strings.Join(potential.Reasons, ","), "相近的候选") {
		t.Fatalf("potential overlap mislabeled: %+v", potential)
	}

	duplicate.CanonicalURL = "./grinders"
	merged := Map(e, own, []domain.PageEvidence{primary, duplicate})
	if merged.Action != "update" || merged.TargetURL != primary.URL || merged.CanonicalIssue || len(merged.Cannibalizing) != 0 {
		t.Fatalf("known canonical duplicate should consolidate evidence: %+v", merged)
	}
	duplicate.CanonicalURL = "https://other.example/steal-canonical"
	canonical := Map(e, own, []domain.PageEvidence{primary, duplicate})
	if !canonical.CanonicalIssue || canonical.TargetURL == duplicate.CanonicalURL {
		t.Fatalf("cross-site canonical was treated as owned: %+v", canonical)
	}
}

func TestOpportunityExactWeightsAndEvidence(t *testing.T) {
	const site = "https://example.test/blog"
	ownerPage := domain.PageEvidence{URL: site + "/shoes", Title: "Compare Trail Shoes", PageType: pageComparison}
	e := domain.KeywordEvidence{
		Term:   "compare trail shoes",
		Metric: domain.KeywordMetric{SearchVolume: ptr(int64(999)), KeywordDifficulty: ptr(25)},
		SERP: []domain.SERPEntry{
			{Rank: 1, URL: "https://alternative.example/shop/comparison/", PageType: pageComparison},
			{Rank: 2, URL: "https://outside.example/shoes", PageType: pageComparison},
			{Rank: 3, URL: ownerPage.URL, PageType: pageComparison},
		},
	}
	inventory := []domain.PageEvidence{ownerPage}
	mapping := Map(e, site, inventory)
	score := Score(e, site, inventory, &mapping, ptr(7.5))
	if score.Score == nil || math.Abs(*score.Score-84.5) > 0.001 || score.Coverage != 100 || score.Version == "" {
		t.Fatalf("unexpected score: %+v", score)
	}
	want := []int{20, 20, 20, 15, 15, 10}
	for i, part := range score.Parts {
		if part.Weight != want[i] || part.Points == nil || *part.Points > float64(part.Weight) || part.Evidence == "" {
			t.Fatalf("unexpected scoring part: %+v", part)
		}
	}
	parts := Score(domain.KeywordEvidence{Term: e.Term, Metric: domain.KeywordMetric{SearchVolume: ptr(int64(0)), Competition: ptr(0.8)}}, site, nil, nil, nil)
	if parts.Score != nil || parts.Coverage != 20 || parts.KnownPoints != 0 || parts.Parts[0].Points == nil || parts.Parts[1].Points != nil {
		t.Fatalf("missing data or ad competition was fabricated: %+v", parts)
	}
}

func TestOpportunityDistinguishesAbsentFromZeroRankPoints(t *testing.T) {
	results := []domain.SERPEntry{}
	for i := 1; i <= 10; i++ {
		results = append(results, domain.SERPEntry{Rank: i, URL: fmt.Sprintf("https://others.example/%d", i)})
	}
	full := rankingScore(results, "https://example.test")
	partial := rankingScore(results[:8], "https://example.test")
	if full.Points == nil || *full.Points != 0 || partial.Points != nil {
		t.Fatalf("complete and partial observations were conflated: %+v versus %+v", full, partial)
	}
}

func TestContentGapsUsesObservedTopicsWithoutCopying(t *testing.T) {
	competitor := domain.PageEvidence{
		URL: "https://alternative.example/compare", Title: "料金比較",
		Headings: []domain.HeadingEvidence{{Level: 2, Text: "Installation using an unusually distinctive proprietary subtitle"}},
		FAQ:      []string{"A proprietary FAQ line for testing"}, Schema: []string{"FAQPage", "Product"},
		Entities: []string{"Alpha Platform", "Beta Browser"},
	}
	local := domain.PageEvidence{URL: "https://own.example/compare", Title: "Product compatibility", Entities: []string{"Alpha Platform"}}
	gaps := ContentGaps(competitor, []domain.PageEvidence{local})
	joined := strings.Join(gaps, "\n")
	for _, word := range []string{"价格或费用", "设置或安装", "FAQ", "Schema", "1 个已采集实体"} {
		if !strings.Contains(joined, word) {
			t.Fatalf("missing gap %q: %v", word, gaps)
		}
	}
	for _, leaked := range []string{"proprietary", "Beta Browser", "A proprietary FAQ"} {
		if strings.Contains(joined, leaked) {
			t.Fatalf("competitor text copied: %q in %v", leaked, gaps)
		}
	}
	if none := ContentGaps(competitor, nil); len(none) != 0 {
		t.Fatalf("invented gaps without local evidence: %v", none)
	}
}

func TestMapKeepsKnownInventoryAndFlagsCanonicalLoops(t *testing.T) {
	const site = "https://site.example/store"
	first := domain.PageEvidence{URL: site + "/coffee", Title: "Compare coffee grinders", PageType: pageComparison}
	second := first
	second.URL = site + "/grinders"
	first.CanonicalURL = second.URL
	second.CanonicalURL = first.URL
	e := domain.KeywordEvidence{
		Term:  "compare coffee grinders",
		Pages: []domain.PageEvidence{{URL: first.URL}},
	}
	got := Map(e, site, []domain.PageEvidence{first, second})
	if got.Action != "update" || got.TargetURL != first.URL || !got.CanonicalIssue {
		t.Fatalf("a sparse capture masked owned data or a canonical loop: %+v", got)
	}
}
