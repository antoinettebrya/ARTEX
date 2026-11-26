package researchengine

import (
	"fmt"

	"github.com/Autumn-27/artex/seoos/internal/domain"
)

var contentTopics = []struct {
	name string
	cues []string
}{
	{"价格或费用", []string{"pricing", "price", "cost", "subscription", "价格", "價格", "费用", "費用", "料金", "preis", "precio", "coût"}},
	{"设置或安装", []string{"install", "installation", "setup", "getting started", "安装", "安裝", "设置", "設定", "導入", "instalación", "einrichtung"}},
	{"兼容性和要求", []string{"compatibility", "requirements", "supported versions", "兼容性", "相容性", "兼容", "動作環境", "kompatibilität", "requisitos"}},
	{"选择或比较", []string{"comparison", "compare", "alternatives", "pros and cons", "比较", "比較", "替代", "优缺点", "vergleich", "comparativa"}},
	{"使用示例", []string{"example", "examples", "case study", "use cases", "示例", "例子", "案例", "事例", "beispiel", "ejemplo"}},
	{"支持或排障", []string{"support", "troubleshooting", "error resolution", "故障", "排查", "支持", "サポート", "fehlerbehebung", "solución de problemas"}},
}

func pageTopics(page domain.PageEvidence) features {
	text := normalizedText(evidenceText(page))
	out := features{}
	for _, topic := range contentTopics {
		for _, cue := range topic.cues {
			if hasPhrase(text, cue) {
				out[topic.name] = struct{}{}
				break
			}
		}
	}
	return out
}

func sampleCoverage(pages []domain.PageEvidence) (features, features, features, bool) {
	topics, schema := features{}, features{}
	hasFAQ := false
	for _, page := range pages {
		for topic := range pageTopics(page) {
			topics[topic] = struct{}{}
		}
		hasFAQ = hasFAQ || len(page.FAQ) > 0
		for _, kind := range page.Schema {
			if key := normalizedText(kind); key != "" {
				schema[key] = struct{}{}
				hasFAQ = hasFAQ || key == "faqpage" || key == "faq page"
			}
		}
	}
	return topics, entities(pages), schema, hasFAQ
}

// ContentGaps compares captured topics and structure to the site's known pages.
// Recommendations describe categories and counts without reproducing competitor copy.
func ContentGaps(page domain.PageEvidence, owned []domain.PageEvidence) []string {
	out := []string{}
	if len(owned) == 0 {
		return out
	}
	hasOwnEvidence := false
	for _, sample := range owned {
		if normalizedText(evidenceText(sample)) != "" || len(sample.FAQ) > 0 || len(sample.Schema) > 0 {
			hasOwnEvidence = true
			break
		}
	}
	if !hasOwnEvidence {
		return out
	}
	ownTopics, ownNames, ownSchema, ownFAQ := sampleCoverage(owned)
	otherTopics, otherNames, otherSchema, otherFAQ := sampleCoverage([]domain.PageEvidence{page})
	for _, topic := range contentTopics {
		_, own := ownTopics[topic.name]
		if _, observed := otherTopics[topic.name]; observed && !own {
			out = append(out, fmt.Sprintf("已采集竞品样本包含%s，本次本站样本未发现对应主题。", topic.name))
		}
	}
	if otherFAQ && !ownFAQ {
		out = append(out, "竞品样本具有 FAQ 结构，可根据本站用户的实际问题评估是否补充。")
	}
	if missing := len(otherNames) - intersection(otherNames, ownNames); missing > 0 {
		out = append(out, fmt.Sprintf("本次竞品样本中的 %d 个已采集实体未在本站样本出现。可核对其主题相关性。", missing))
	}
	if missing := len(otherSchema) - intersection(otherSchema, ownSchema); missing > 0 {
		out = append(out, fmt.Sprintf("竞品样本使用了 %d 种本站样本中未见的 Schema 类型，可核对其适用条件。", missing))
	}
	return out
}
