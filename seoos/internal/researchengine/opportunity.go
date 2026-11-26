package researchengine

import (
	"fmt"
	"math"

	"github.com/Autumn-27/artex/seoos/internal/domain"
)

const opportunityVersion = "phase2-v1"

func scored(name string, weight int, points float64, evidence string) domain.ScorePart {
	rounded := math.Round(points*100) / 100
	return domain.ScorePart{Name: name, Weight: weight, Points: &rounded, Evidence: evidence}
}

func unavailable(name string, weight int, evidence string) domain.ScorePart {
	return domain.ScorePart{Name: name, Weight: weight, Evidence: evidence}
}

// Score uses six independently evidenced parts. Partial data produces known
// points and weighted coverage while the complete 100-point score stays absent.
func Score(e domain.KeywordEvidence, siteURL string, owned []domain.PageEvidence, mapping *domain.URLMapping, businessValue *float64) domain.Opportunity {
	out := domain.Opportunity{
		Version: opportunityVersion,
		Parts: []domain.ScorePart{
			demandScore(e.Metric.SearchVolume), difficultyScore(e.Metric.KeywordDifficulty),
			intentFitScore(e, siteURL, owned, mapping), relevanceScore(e, siteURL, owned),
			rankingScore(e.SERP, siteURL), businessScore(businessValue),
		},
	}
	for _, part := range out.Parts {
		if part.Points != nil {
			out.KnownPoints += *part.Points
			out.Coverage += part.Weight
		}
	}
	out.KnownPoints = math.Round(out.KnownPoints*100) / 100
	if out.Coverage == 100 {
		score := out.KnownPoints
		out.Score = &score
	}
	return out
}

func demandScore(volume *int64) domain.ScorePart {
	if volume == nil || *volume < 0 {
		return unavailable("demand", 20, "缺少提供方的搜索量。")
	}
	points := 20 * math.Min(1, math.Log10(float64(*volume)+1)/4)
	return scored("demand", 20, points, fmt.Sprintf("提供方月搜索量为 %d，采用对数尺度并在约 10,000 次达到满分。", *volume))
}

func difficultyScore(difficulty *int) domain.ScorePart {
	if difficulty == nil || *difficulty < 0 || *difficulty > 100 {
		return unavailable("difficulty", 20, "缺少自然搜索 Keyword Difficulty。")
	}
	points := 20 * float64(100-*difficulty) / 100
	return scored("difficulty", 20, points, fmt.Sprintf("提供方 Keyword Difficulty 为 %d，按 100 减难度折算。", *difficulty))
}

func intentFitScore(e domain.KeywordEvidence, siteURL string, owned []domain.PageEvidence, mapping *domain.URLMapping) domain.ScorePart {
	intent := intentOf(e)
	if intent.Confidence < 0.5 {
		return unavailable("intent_fit", 20, "搜索意图证据尚不足以评估页面匹配。")
	}
	if mapping == nil || mapping.TargetURL == "" || !inSite(siteURL, mapping.TargetURL) {
		return unavailable("intent_fit", 20, "需要有证据支持的站内目标页。")
	}
	page, ok := candidates(e, siteURL, owned)[canonicalKey(mapping.TargetURL)]
	if !ok || page.typ == "" {
		return unavailable("intent_fit", 20, "目标页缺少可判定的页面类型。")
	}
	points := 20 * fitForType(intent, page.typ)
	return scored("intent_fit", 20, points, fmt.Sprintf("目标页类型为 %s，搜索意图建议 %s。", page.typ, intent.PageType))
}

func relevanceScore(e domain.KeywordEvidence, siteURL string, owned []domain.PageEvidence) domain.ScorePart {
	query := wordFeatures(e.Term)
	if len(query) == 0 {
		return unavailable("site_relevance", 15, "查询缺少可用于相关性比较的词项。")
	}
	var best float64
	var found bool
	for _, page := range ownedSamples(siteURL, append(append([]domain.PageEvidence(nil), owned...), e.Pages...)) {
		doc := wordFeatures(evidenceText(page))
		if len(doc) == 0 {
			continue
		}
		found = true
		best = math.Max(best, keywordCoverage(query, doc))
	}
	if !found {
		return unavailable("site_relevance", 15, "缺少本站标题、正文结构或主题样本。")
	}
	return scored("site_relevance", 15, 15*best, fmt.Sprintf("在本站已采集样本中，最佳查询词项覆盖比例为 %.2f。", best))
}

func rankingScore(entries []domain.SERPEntry, siteURL string) domain.ScorePart {
	if canonicalKey(siteURL) == "" {
		return unavailable("current_ranking", 15, "缺少有效的站点范围。")
	}
	results := organic(entries)
	for _, entry := range results {
		if inSite(siteURL, entry.URL) {
			return scored("current_ranking", 15, 15*float64(11-entry.Rank)/10,
				fmt.Sprintf("本次快照的最佳站内自然结果排名第 %d。", entry.Rank))
		}
	}
	if len(results) == 10 {
		return scored("current_ranking", 15, 0, "本次采集的前十条自然结果中没有本站页面。")
	}
	return unavailable("current_ranking", 15, "需要足够的自然排名快照以判断前十名中的本站表现。")
}

func businessScore(value *float64) domain.ScorePart {
	if value == nil || math.IsNaN(*value) || *value < 0 || *value > 10 {
		return unavailable("business_value", 10, "运营人员尚未提供 0 到 10 分的商业价值评估。")
	}
	return scored("business_value", 10, *value, fmt.Sprintf("运营人员填写的商业价值为 %.2f 分。", *value))
}
