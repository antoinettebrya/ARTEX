package researchengine

import (
	"fmt"
	"sort"

	"github.com/Autumn-27/artex/seoos/internal/domain"
)

type candidate struct {
	key    string
	page   domain.PageEvidence
	typ    string
	rank   int
	match  float64
	points float64
}

func keywordCoverage(query, page features) float64 {
	if len(query) == 0 || len(page) == 0 {
		return 0
	}
	return float64(intersection(query, page)) / float64(len(query))
}

func candidates(e domain.KeywordEvidence, siteURL string, owned []domain.PageEvidence) map[string]candidate {
	result := make(map[string]candidate)
	for _, page := range ownedSamples(siteURL, e.Pages) {
		key := canonicalKey(page.URL)
		result[key] = candidate{key: key, page: page}
	}
	for _, page := range ownedSamples(siteURL, owned) {
		key := canonicalKey(page.URL)
		if previous, exists := result[key]; !exists || evidenceText(previous.page) == "" {
			result[key] = candidate{key: key, page: page}
		}
	}
	for _, entry := range organic(e.SERP) {
		if !inSite(siteURL, entry.URL) {
			continue
		}
		page, exists := result[entry.URL]
		if !exists {
			page = candidate{key: entry.URL, page: observedPage(entry)}
		} else {
			if page.page.Title == "" {
				page.page.Title = entry.Title
			}
			if page.page.Description == "" {
				page.page.Description = entry.Description
			}
			if page.page.PageType == "" {
				page.page.PageType = entry.PageType
			}
		}
		page.rank = entry.Rank
		result[entry.URL] = page
	}

	query := wordFeatures(e.Term)
	intent := intentOf(e)
	for key, item := range result {
		item.typ = PageType(item.page)
		item.match = keywordCoverage(query, pageWords(item.page))
		item.points = 30*item.match + 12*fitForType(intent, item.typ)
		if item.rank > 0 {
			item.points += 50 + float64(11-item.rank)
		}
		result[key] = item
	}
	return result
}

func relevant(item candidate) bool {
	return item.rank > 0 || item.match >= 0.5
}

func orderedCandidates(pages map[string]candidate) []candidate {
	selected := []candidate{}
	for _, item := range pages {
		if relevant(item) {
			selected = append(selected, item)
		}
	}
	sort.Slice(selected, func(i, j int) bool {
		if selected[i].points == selected[j].points {
			return selected[i].key < selected[j].key
		}
		return selected[i].points > selected[j].points
	})
	return selected
}

// Map recommends a page action from observed organic results and the site's
// known inventory. A scoped result can establish a page even with no import.
func Map(e domain.KeywordEvidence, siteURL string, owned []domain.PageEvidence) domain.URLMapping {
	pages := candidates(e, siteURL, owned)
	out := domain.URLMapping{Reasons: []string{}, Evidence: []string{}}
	if len(pages) == 0 {
		out.Action = "inventory_missing"
		out.Reasons = append(out.Reasons, "当前缺少本站页面样本和已排名的本站 URL。导入页面清单后可确认是否需要新建。")
		out.Evidence = append(out.Evidence, fmt.Sprintf("本次采集了 %d 条有效自然结果。", len(organic(e.SERP))))
		return out
	}
	out.Evidence = append(out.Evidence, fmt.Sprintf("本次检查了站点范围内的 %d 个已知页面。", len(pages)))
	selected := orderedCandidates(pages)
	if len(selected) == 0 {
		out.Action = "create"
		out.Reasons = append(out.Reasons, "当前清单中未找到与查询关联的页面，可评估新建目标页。")
		return out
	}
	intent := intentOf(e)
	target := selected[0]
	aliases := map[string]string{}
	for _, item := range selected {
		if item.page.CanonicalURL == "" {
			continue
		}
		canonical := canonicalTarget(item.page)
		if canonical == item.key {
			continue
		}
		candidate, known := pages[canonical]
		if inSite(siteURL, canonical) && known && relevant(candidate) && (candidate.typ != "" && candidate.typ == item.typ || fitForType(intent, candidate.typ) == 1) {
			aliases[item.key] = canonical
			out.Evidence = append(out.Evidence, fmt.Sprintf("%s 的已观测 canonical 指向同范围候选页 %s。", item.key, canonical))
			continue
		}
		out.CanonicalIssue = true
		out.Reasons = append(out.Reasons, fmt.Sprintf("需核对 %s 的 canonical 是否指向有效且同意图的站内目标页。", item.key))
	}

	// A direct canonical alias can prefer an evidenced alternate page. Chains
	// and loops require review before the canonical can determine the target.
	for _, item := range selected {
		if next := aliases[item.key]; next != "" && aliases[next] != "" {
			out.CanonicalIssue = true
			out.Reasons = append(out.Reasons, "候选页面存在多级或循环 canonical，建议先核对最终目标。")
			break
		}
	}
	if preferred := aliases[target.key]; preferred != "" && aliases[preferred] == "" {
		target = pages[preferred]
	}
	out.TargetURL = target.key
	out.Action = "update"
	if target.rank > 0 {
		out.Evidence = append(out.Evidence, fmt.Sprintf("目标页 %s 在本次自然结果中排名第 %d。", target.key, target.rank))
	} else {
		out.Evidence = append(out.Evidence, fmt.Sprintf("目标页 %s 已在站点样本中登记，查询词项命中比例为 %.2f。", target.key, target.match))
	}
	out.Reasons = append(out.Reasons, "优先检查并更新现有目标页，使页面结构与已识别的搜索需求匹配。")

	ranked := map[string]candidate{}
	var potential int
	for _, item := range selected {
		if item.key != target.key && item.match >= 0.5 && fitForType(intent, item.typ) >= 0.5 && aliases[item.key] != target.key {
			potential++
		}
		if item.rank == 0 {
			continue
		}
		key := item.key
		if alias := aliases[key]; alias != "" && aliases[alias] == "" {
			key = alias
		}
		if existing, seen := ranked[key]; !seen || item.rank < existing.rank {
			ranked[key] = item
		}
	}
	if len(ranked) < 2 {
		if potential > 0 {
			out.Reasons = append(out.Reasons, fmt.Sprintf("另有 %d 个题材或页面类型相近的候选，需确认其目标查询。", potential))
		}
		return out
	}

	var sameIntent int
	for _, item := range ranked {
		out.Cannibalizing = append(out.Cannibalizing, item.key)
		if fitForType(intent, item.typ) == 1 {
			sameIntent++
		}
	}
	sort.Strings(out.Cannibalizing)
	out.Evidence = append(out.Evidence, fmt.Sprintf("同一次自然结果快照中出现 %d 个不同的本站排名页面。", len(out.Cannibalizing)))
	if sameIntent >= 2 && intent.Confidence >= 0.5 {
		out.Action = "merge"
		out.Reasons = append(out.Reasons, "多个同时排名的页面类型与同一需求一致，可人工评估合并或重定向。")
	} else {
		out.Reasons = append(out.Reasons, "多个页面同时出现于自然结果，需核对各自的搜索需求与独立价值。")
	}
	return out
}
