package contentengine

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Autumn-27/artex/seoos/internal/domain"
	"github.com/Autumn-27/artex/seoos/internal/researchengine"
)

func searchIntent(e domain.KeywordEvidence) domain.SearchIntent {
	if e.Intent != nil && e.Intent.Primary != "" && e.Intent.PageType != "" {
		return *e.Intent
	}
	return researchengine.Classify(e, nil)
}

func hasResearch(w domain.ContentWorkspace) bool {
	e := w.Evidence
	if w.Project.Keyword == "" || e.Keyword.ID == "" ||
		domain.KeywordKey(e.Keyword.Term) != domain.KeywordKey(w.Project.Keyword) || e.SERPSnapshotID == "" {
		return false
	}
	for _, row := range e.Keyword.SERP {
		if row.Rank >= 1 && row.Rank <= 10 && canonicalKey(row.URL) != "" {
			return true
		}
	}
	return false
}

func ownedAssets(w domain.ContentWorkspace) []domain.SitePage {
	assets := make([]domain.SitePage, 0, len(w.Assets))
	for _, asset := range w.Assets {
		if sitePage(w.SiteURL, asset.URL) && assetReady(asset) && ownsPageLanguage(asset, w.Project.Market.LanguageCode) {
			assets = append(assets, asset)
		}
	}
	return assets
}

func exactAsset(assets []domain.SitePage, raw string) *domain.SitePage {
	key := canonicalKey(raw)
	if key == "" {
		return nil
	}
	for _, page := range assets {
		if canonicalKey(page.URL) == key {
			return &page
		}
	}
	return nil
}

func clusterContains(cluster domain.KeywordCluster, evidence domain.KeywordEvidence) bool {
	for _, id := range cluster.MemberIDs {
		if id != "" && id == evidence.ID {
			return true
		}
	}
	for _, group := range [][]string{{cluster.PrimaryKeyword}, cluster.Secondary, cluster.LongTail} {
		for _, keyword := range group {
			if domain.KeywordKey(keyword) == domain.KeywordKey(evidence.Term) {
				return true
			}
		}
	}
	return false
}

// Strategy combines a persisted, market-specific SERP with Phase 2 signals and
// page evidence from the authorized site inventory.
func Strategy(w domain.ContentWorkspace) (domain.ContentStrategy, error) {
	if !hasResearch(w) {
		return domain.ContentStrategy{}, domain.ErrContentPrerequisite
	}
	e := w.Evidence
	intent := searchIntent(e.Keyword)
	assets := ownedAssets(w)
	pages := make([]domain.PageEvidence, 0, len(assets))
	for _, asset := range assets {
		pages = append(pages, asset.PageEvidence)
	}
	localMapping := researchengine.Map(e.Keyword, w.SiteURL, pages)
	mapping := localMapping
	if saved := e.Mapping; saved != nil && (saved.TargetURL == "" || sitePage(w.SiteURL, saved.TargetURL)) {
		// A saved target is an editorial decision for this scope. Compare the
		// new inventory independently for conflicts and newly visible pages.
		if saved.TargetURL != "" || mapping.Action == "inventory_missing" {
			mapping = *saved
		}
	}
	s := domain.ContentStrategy{
		TargetURL: mapping.TargetURL, PageType: intent.PageType, Reasons: []string{},
		Evidence: []string{"serp_snapshot:" + e.SERPSnapshotID, "keyword:" + e.Keyword.ID},
	}
	s.Evidence = appendUnique(s.Evidence, 40, "market:"+w.Project.Market.CountryCode+"/"+w.Project.Market.LanguageCode+"/"+w.Project.Market.SearchEngine+"/"+w.Project.Market.Device)
	if len(intent.Sources) > 0 {
		s.Evidence = appendUnique(s.Evidence, 40, "intent:"+intent.Primary+" ("+strings.Join(intent.Sources, ", ")+")")
	}
	metricNames := make([]string, 0, len(e.Keyword.MetricSources))
	for name := range e.Keyword.MetricSources {
		metricNames = append(metricNames, name)
	}
	sort.Strings(metricNames)
	for _, name := range metricNames {
		if source := e.Keyword.MetricSources[name]; source.SnapshotID != "" {
			s.Evidence = appendUnique(s.Evidence, 40, "metric:"+name+":"+source.SnapshotID)
		}
	}
	if opportunity := e.Opportunity; opportunity != nil {
		s.Evidence = appendUnique(s.Evidence, 40, fmt.Sprintf("opportunity:%s (%.1f known points, %d%% measured)", opportunity.Version, opportunity.KnownPoints, opportunity.Coverage))
	}
	for _, competitor := range e.Competitors {
		if competitor.SnapshotID != "" {
			s.Evidence = appendUnique(s.Evidence, 40, "competitor_snapshot:"+competitor.SnapshotID)
		}
	}
	for _, cluster := range e.Clusters {
		if clusterContains(cluster, e.Keyword) && len(cluster.Secondary)+len(cluster.LongTail) > 0 {
			s.TopicCluster = true
			s.Evidence = appendUnique(s.Evidence, 40, "keyword_cluster:"+cluster.ID)
		}
	}
	if e.Mapping != nil {
		s.Evidence = appendUnique(s.Evidence, 40, "phase2_mapping:"+e.Mapping.Action, e.Mapping.TargetURL)
	}
	for _, evidence := range [][]string{mapping.Evidence, localMapping.Evidence} {
		s.Evidence = appendUnique(s.Evidence, 40, evidence...)
	}
	competingURLs := []string{}
	for _, candidates := range [][]string{mapping.Cannibalizing, localMapping.Cannibalizing} {
		for _, address := range candidates {
			if sitePage(w.SiteURL, address) {
				competingURLs = appendUnique(competingURLs, 10, canonicalKey(address))
				s.Evidence = appendUnique(s.Evidence, 40, "competing_url:"+address)
			}
		}
	}

	target := exactAsset(assets, s.TargetURL)
	switch {
	case mapping.CanonicalIssue || localMapping.CanonicalIssue:
		s.Action = "hold"
		s.Reasons = append(s.Reasons, "Resolve the observed canonical conflict before committing to a target URL.")
	case len(competingURLs) >= 2 && (mapping.Action == "merge" || localMapping.Action == "merge"):
		s.Action = "merge"
		s.Reasons = append(s.Reasons, "Multiple URLs satisfy this market's intent in the observed SERP. Compare their unique sections and select a consolidation target.")
	case len(competingURLs) >= 2:
		s.Action = "hold"
		s.Reasons = append(s.Reasons, "Several site URLs appear for the same query. Confirm their distinct intents before adding or rewriting a page.")
	case target != nil:
		s.Action = "update"
		s.Evidence = appendUnique(s.Evidence, 40, target.URL)
		if target.SnapshotID != "" {
			s.Evidence = appendUnique(s.Evidence, 40, "site_snapshot:"+target.SnapshotID)
		}
		if score, duplicate := nearestDuplicate(target.Body, target.URL, assets); score >= .84 {
			s.Action = "merge"
			s.Evidence = appendUnique(s.Evidence, 40, duplicate)
			s.Reasons = append(s.Reasons, fmt.Sprintf("Another captured page has %.0f%% similarity to the target body.", score*100))
		} else if current := researchengine.PageType(target.PageEvidence); current != "" && intent.PageType != "" && current != intent.PageType &&
			!(intent.Primary == "commercial" && current == "category") {
			s.Action = "adjust_type"
			s.Reasons = append(s.Reasons, fmt.Sprintf("Current page type %s differs from the observed %s intent's %s page type.", current, intent.Primary, intent.PageType))
		} else {
			s.Reasons = append(s.Reasons, "Use the captured target and retain its URL while improving coverage and current evidence.")
		}
	case s.TargetURL != "":
		s.Action = "hold"
		s.Reasons = append(s.Reasons, "The mapping identifies a target. Capture that page before deciding how to update it.")
	case len(assets) == 0:
		s.Action = "hold"
		s.Reasons = append(s.Reasons, "Capture existing pages in this site's scope to establish whether a new URL is warranted.")
	default:
		s.Action = "create"
		s.TargetURL = suggestedURL(w.SiteURL, w.Project.Keyword)
		if s.TargetURL == "" || exactAsset(assets, s.TargetURL) != nil {
			s.Action = "hold"
			s.Reasons = append(s.Reasons, "Choose a unique URL in the site's scope before creating a page.")
		} else {
			s.Reasons = append(s.Reasons, "The site inventory and saved mapping show no target that addresses this market's observed query.")
		}
	}
	if s.TopicCluster {
		s.Reasons = append(s.Reasons, "Use the saved keyword cluster to assign related terms to the canonical target or distinct supporting pages.")
	}
	return s, nil
}
