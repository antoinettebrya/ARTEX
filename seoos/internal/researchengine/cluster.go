package researchengine

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/Autumn-27/artex/seoos/internal/domain"
	"github.com/google/uuid"
)

type clusterTerm struct {
	evidence domain.KeywordEvidence
	intent   domain.SearchIntent
	identity string
	terms    features
	concepts features
	entities features
	serp     features
}

func concepts(value string) features {
	query := normalizedText(value)
	labels := features{}
	for _, profile := range intentProfiles {
		for _, cue := range profile.cues {
			if hasPhrase(query, cue) {
				labels[profile.label] = struct{}{}
				break
			}
		}
	}
	return labels
}

func entities(samples []domain.PageEvidence) features {
	labels := features{}
	for _, sample := range samples {
		for _, entity := range sample.Entities {
			if key := normalizedText(entity); key != "" {
				labels[key] = struct{}{}
			}
		}
	}
	return labels
}

func intentOf(e domain.KeywordEvidence) domain.SearchIntent {
	if e.Intent != nil && validIntent(e.Intent.Primary) && e.Intent.Confidence >= 0 && e.Intent.Confidence <= 1 {
		return *e.Intent
	}
	return Classify(e, nil)
}

func termFeatures(e domain.KeywordEvidence) clusterTerm {
	keys := features{}
	for _, entry := range organic(e.SERP) {
		keys[entry.URL] = struct{}{}
	}
	identity := e.ID
	if identity == "" {
		identity = domain.KeywordKey(e.Term)
	}
	return clusterTerm{
		evidence: e, intent: intentOf(e), identity: identity, terms: wordFeatures(e.Term),
		concepts: concepts(e.Term), entities: entities(e.Pages), serp: keys,
	}
}

type similarity struct {
	combined float64
	organic  float64
	semantic float64
	entity   float64
}

func clusterSimilarity(a, b clusterTerm) similarity {
	organicOverlap := jaccard(a.serp, b.serp)
	if intersection(a.serp, b.serp) < 2 || organicOverlap < 0.20 {
		return similarity{}
	}
	intentAgreement := 0.0
	switch {
	case a.intent.Primary == b.intent.Primary && a.intent.Confidence >= 0.35 && b.intent.Confidence >= 0.35:
		intentAgreement = 1
	case a.intent.Secondary == b.intent.Primary || b.intent.Secondary == a.intent.Primary:
		intentAgreement = 0.5
	case a.intent.Confidence >= 0.60 && b.intent.Confidence >= 0.60:
		return similarity{}
	}
	// Concept labels are transparent multilingual lexical rules. The organic URL
	// gate provides a distinct check before any lexical or intent match can group terms.
	semantic := 0.80*jaccard(a.terms, b.terms) + 0.20*jaccard(a.concepts, b.concepts)
	entity := jaccard(a.entities, b.entities)
	return similarity{
		combined: 0.60*organicOverlap + 0.23*semantic + 0.11*intentAgreement + 0.06*entity,
		organic:  organicOverlap, semantic: semantic, entity: entity,
	}
}

const minimumClusterSimilarity = 0.36

// Cluster groups the caller's one-market evidence. It uses complete linkage so
// one bridging query cannot join searches with incompatible results.
func Cluster(keywords []domain.KeywordEvidence, siteURL string) []domain.KeywordCluster {
	sorted := append([]domain.KeywordEvidence(nil), keywords...)
	sort.SliceStable(sorted, func(i, j int) bool {
		a, b := sorted[i], sorted[j]
		aVolume, bVolume := a.Metric.SearchVolume, b.Metric.SearchVolume
		if aVolume == nil && bVolume != nil {
			return false
		}
		if aVolume != nil && bVolume == nil {
			return true
		}
		if aVolume != nil && bVolume != nil && *aVolume != *bVolume {
			return *aVolume > *bVolume
		}
		if aKey, bKey := domain.KeywordKey(a.Term), domain.KeywordKey(b.Term); aKey != bKey {
			return aKey < bKey
		}
		return a.ID < b.ID
	})
	groups := [][]clusterTerm{}
	seen := features{}
	for _, keyword := range sorted {
		name := domain.KeywordKey(keyword.Term)
		if name == "" {
			continue
		}
		if _, duplicate := seen[name]; duplicate {
			continue
		}
		seen[name] = struct{}{}
		term := termFeatures(keyword)

		choice, best := -1, minimumClusterSimilarity
		for groupIndex, group := range groups {
			weakest := 1.0
			for _, other := range group {
				weakest = math.Min(weakest, clusterSimilarity(term, other).combined)
				if weakest < minimumClusterSimilarity {
					break
				}
			}
			if weakest >= best && (choice == -1 || weakest > best) {
				choice, best = groupIndex, weakest
			}
		}
		if choice >= 0 {
			groups[choice] = append(groups[choice], term)
			continue
		}
		groups = append(groups, []clusterTerm{term})
	}
	out := make([]domain.KeywordCluster, 0, len(groups))
	for _, group := range groups {
		out = append(out, outputCluster(group, siteURL))
	}
	return out
}

func outputCluster(group []clusterTerm, siteURL string) domain.KeywordCluster {
	primary := group[0]
	cluster := domain.KeywordCluster{
		PrimaryKeyword: primary.evidence.Term, PageType: primary.intent.PageType,
		Secondary: []string{}, LongTail: []string{}, MemberIDs: make([]string, 0, len(group)),
	}
	identities := make([]string, 0, len(group))
	for _, member := range group {
		identities = append(identities, member.identity)
		cluster.MemberIDs = append(cluster.MemberIDs, member.evidence.ID)
	}
	sort.Strings(identities)
	sort.Strings(cluster.MemberIDs)
	cluster.ID = uuid.NewSHA1(uuid.NameSpaceOID, []byte(strings.Join(identities, "\x00"))).String()
	for _, member := range group[1:] {
		if longPhrase(member.evidence.Term, primary.evidence.Term) {
			cluster.LongTail = append(cluster.LongTail, member.evidence.Term)
		} else {
			cluster.Secondary = append(cluster.Secondary, member.evidence.Term)
		}
	}
	if len(group) == 1 {
		cluster.Reasons = []string{"当前关键词独立保留，继续采集自然结果以验证可合并的搜索需求。"}
	} else {
		cluster.Confidence, cluster.Reasons = groupEvidence(group)
	}
	cluster.RecommendedURL = recommendedURL(group, siteURL)
	if cluster.RecommendedURL != "" {
		cluster.Reasons = append(cluster.Reasons, "推荐已有自然排名且位于站点范围内的页面。")
	}
	return cluster
}

func longPhrase(term, primary string) bool {
	if strings.ContainsFunc(term, denseScript) {
		length := utf8.RuneCountInString(normalizedText(term))
		return length >= 8 && length >= utf8.RuneCountInString(normalizedText(primary))+2
	}
	length := len(strings.Fields(normalizedText(term)))
	return length >= 4 && length > len(strings.Fields(normalizedText(primary)))
}

func groupEvidence(group []clusterTerm) (float64, []string) {
	var pairCount int
	var scores, urls, words, names float64
	for index, a := range group {
		for _, b := range group[index+1:] {
			pair := clusterSimilarity(a, b)
			pairCount++
			scores += pair.combined
			urls += pair.organic
			words += pair.semantic
			names += pair.entity
		}
	}
	n := float64(pairCount)
	confidence := math.Round(scores/n*100) / 100
	reasons := []string{
		fmt.Sprintf("所有关键词对至少共享两个自然结果 URL，平均 Jaccard 相似度为 %.2f。", urls/n),
		fmt.Sprintf("词项和概念相似度均值为 %.2f，并结合搜索意图比较。", words/n),
	}
	if names > 0 {
		reasons = append(reasons, fmt.Sprintf("已采集页面的实体重合度均值为 %.2f。", names/n))
	}
	return confidence, reasons
}

func recommendedURL(group []clusterTerm, siteURL string) string {
	votes := map[string]int{}
	for _, member := range group {
		for _, result := range organic(member.evidence.SERP) {
			if inSite(siteURL, result.URL) {
				votes[result.URL] += 11 - result.Rank
			}
		}
	}
	var key string
	var best int
	for candidate, points := range votes {
		if points > best || points == best && candidate < key {
			key, best = candidate, points
		}
	}
	return key
}
