// Package researchengine derives versioned research results from collected evidence.
package researchengine

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"unicode"

	"github.com/Autumn-27/artex/seoos/internal/domain"
)

const (
	informational = "informational"
	commercial    = "commercial"
	transactional = "transactional"
	navigational  = "navigational"
	local         = "local"

	pageGuide      = "guide"
	pageComparison = "comparison"
	pageProduct    = "product"
	pageCategory   = "category"
	pageHome       = "homepage"
	pageLocal      = "local_landing"
)

type intentProfile struct {
	label  string
	weight float64
	cues   []string
	need   string
	page   string
}

var intentProfiles = []intentProfile{
	{
		label: local, weight: 6,
		cues: []string{"near me", "nearby", "in my area", "directions", "opening hours", "附近", "本地", "营业时间", "營業時間", "近く", "周辺", "cerca de mí", "près de moi", "in der nähe", "perto de mim", "가까운"},
		need: "查找可到访的地点、服务范围或营业信息。", page: pageLocal,
	},
	{
		label: transactional, weight: 4.6,
		cues: []string{"buy", "purchase", "order", "checkout", "coupon", "book appointment", "book tickets", "reserve", "购买", "購買", "下单", "预订", "シート予約", "購入", "予約", "acheter", "commander", "comprar", "reservar", "kaufen", "bestellen", "구매"},
		need: "准备购买、预订或完成其他具体操作。", page: pageProduct,
	},
	{
		label: commercial, weight: 4.3,
		cues: []string{"best", "vs", "compare", "comparison", "reviews", "review", "alternatives", "pricing", "price comparison", "推荐", "推薦", "测评", "測評", "評測", "比较", "比較", "评测", "排行榜", "口コミ", "avis", "comparatif", "mejor", "vergleich", "bewertung", "melhor", "추천"},
		need: "比较候选项、价格、评价和使用条件。", page: pageComparison,
	},
	{
		label: navigational, weight: 5,
		cues: []string{"official site", "official website", "login", "sign in", "homepage", "官网", "官網", "登录", "登入", "官方网站", "公式サイト", "ログイン", "sitio oficial", "site officiel", "offizielle website", "공식"},
		need: "进入指定品牌、网站或服务的入口。", page: pageHome,
	},
	{
		label: informational, weight: 4.4,
		cues: []string{"how to", "what is", "why", "guide", "tutorial", "definition", "如何", "怎么", "怎麼", "为什么", "是什么", "指南", "教程", "とは", "使い方", "cómo", "qué es", "comment", "anleitung", "was ist", "como fazer", "사용법"},
		need: "理解一个概念、问题或具体做法。", page: pageGuide,
	},
}

func preferredPage(intent string) string {
	for _, profile := range intentProfiles {
		if profile.label == intent {
			return profile.page
		}
	}
	return ""
}

func validIntent(intent string) bool {
	return preferredPage(intent) != ""
}

func normalizeIntent(value string) string {
	kind := normalizedText(value)
	switch kind {
	case "informational", "information", "informative":
		return informational
	case "commercial", "commercial investigation":
		return commercial
	case "transactional", "transaction":
		return transactional
	case "navigational", "navigation":
		return navigational
	case "local", "local intent":
		return local
	default:
		return ""
	}
}

func intentForPage(kind string) string {
	switch kind {
	case pageGuide:
		return informational
	case pageComparison, pageCategory:
		return commercial
	case pageProduct:
		return transactional
	case pageHome:
		return navigational
	case pageLocal:
		return local
	default:
		return ""
	}
}

type intentSignal struct {
	label string
	score float64
}

// Classify combines transparent query rules, categorized organic results and
// optional corroboration from the data provider and language model.
func Classify(e domain.KeywordEvidence, llm *domain.SearchIntent) domain.SearchIntent {
	scores := map[string]float64{}
	reasons := []string{}
	sources := []string{}
	query := normalizedText(e.Term)
	var keywordEvidence bool
	for _, profile := range intentProfiles {
		hits := []string{}
		for _, cue := range profile.cues {
			if hasPhrase(query, cue) {
				hits = append(hits, cue)
			}
		}
		if len(hits) > 0 {
			keywordEvidence = true
			scores[profile.label] += profile.weight + float64(min(2, len(hits)-1))*0.5
			reasons = append(reasons, fmt.Sprintf("查询词中的 %s 线索包括 %s。", profile.label, strings.Join(hits[:min(3, len(hits))], "、")))
		}
	}
	if queryNamesHost(e.Term) {
		keywordEvidence = true
		scores[navigational] += 4
		reasons = append(reasons, "查询词本身符合站点地址格式。")
	}
	if keywordEvidence {
		sources = append(sources, "keyword_rules")
	}

	pageByURL := make(map[string]domain.PageEvidence, len(e.Pages))
	for _, page := range e.Pages {
		if key := canonicalKey(page.URL); key != "" {
			pageByURL[key] = page
		}
	}
	types := map[string]int{}
	var categorized, observedDetails int
	for _, result := range organic(e.SERP) {
		kind := PageType(observedPage(result))
		if detail, ok := pageByURL[result.URL]; ok {
			if fromDetail := PageType(detail); fromDetail != "" {
				kind = fromDetail
				observedDetails++
			}
		}
		if intent := intentForPage(kind); intent != "" {
			categorized++
			types[kind]++
			scores[intent] += 0.65 + float64(10-result.Rank)*0.085
		}
	}
	if categorized > 0 {
		sources = append(sources, "organic_serp_page_types")
		if observedDetails > 0 {
			sources = append(sources, "captured_page_evidence")
		}
		for _, kind := range []string{pageGuide, pageComparison, pageCategory, pageProduct, pageHome, pageLocal} {
			if types[kind] > 0 {
				reasons = append(reasons, fmt.Sprintf("已采集自然结果中，%d 条呈现 %s 页面特征。", types[kind], kind))
			}
		}
	}
	if supplied := normalizeIntent(e.Metric.ProviderIntent); supplied != "" {
		scores[supplied] += 2.25
		sources = append(sources, "data_provider_intent")
		reasons = append(reasons, fmt.Sprintf("数据提供方记录的意图为 %s。", supplied))
	}

	// The model may corroborate one class already supported by independent evidence.
	independentSources := len(sources)
	if llm != nil && llm.Confidence > 0 && llm.Confidence <= 1 {
		if suggested := normalizeIntent(llm.Primary); scores[suggested] > 0 {
			scores[suggested] += math.Min(1.05, 1.05*llm.Confidence)
			sources = append(sources, "llm_corroboration")
			reasons = append(reasons, fmt.Sprintf("语言模型分析也支持 %s。", suggested))
		}
	}

	out := domain.SearchIntent{
		Primary: informational, Need: "通过对应市场的自然搜索结果确认用户需求。",
		PageType: pageGuide, Confidence: 0.15, Reasons: reasons, Sources: sources,
	}
	signals := make([]intentSignal, 0, len(intentProfiles))
	for _, profile := range intentProfiles {
		if score := scores[profile.label]; score > 0 {
			signals = append(signals, intentSignal{label: profile.label, score: score})
		}
	}
	if len(signals) == 0 {
		out.Reasons = append(out.Reasons, "当前查询缺少明确词义线索和可判读的自然结果，暂按信息需求标记。")
		return out
	}
	sort.SliceStable(signals, func(i, j int) bool { return signals[i].score > signals[j].score })
	main := signals[0]
	var competingScore float64
	if len(signals) > 1 {
		competingScore = signals[1].score
		if competingScore >= main.score*0.4 {
			out.Secondary = signals[1].label
		}
	}
	out.Primary = main.label
	for _, profile := range intentProfiles {
		if profile.label == main.label {
			out.Need = profile.need
			out.PageType = profile.page
			break
		}
	}
	if out.Primary == commercial && types[pageCategory] > types[pageComparison] {
		out.PageType = pageCategory
	}

	margin := (main.score - competingScore) / main.score
	confidence := 0.23 + math.Min(0.38, main.score*0.04) + 0.19*margin + float64(min(independentSources, 3))*0.045
	if categorized == 0 {
		confidence = math.Min(confidence, 0.62)
	}
	if categorized == 0 && !keywordEvidence {
		confidence = math.Min(confidence, 0.43)
	}
	if categorized == 1 && !keywordEvidence {
		confidence = math.Min(confidence, 0.56)
	}
	out.Confidence = math.Round(math.Min(confidence, 0.95)*100) / 100
	return out
}

func queryNamesHost(term string) bool {
	value := domain.KeywordKey(term)
	value = strings.TrimPrefix(strings.TrimPrefix(value, "https://"), "http://")
	value = strings.TrimSuffix(value, "/")
	if strings.ContainsAny(value, " /?:#@") {
		return false
	}
	before, suffix, found := strings.Cut(value, ".")
	if !found || len(before) == 0 {
		return false
	}
	labels := strings.Split(suffix, ".")
	tld := labels[len(labels)-1]
	return len(tld) >= 2 && strings.IndexFunc(tld, func(r rune) bool { return !unicode.IsLetter(r) }) == -1
}
