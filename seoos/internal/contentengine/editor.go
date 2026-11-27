package contentengine

import (
	"math"
	"path"
	"strings"
	"time"

	"github.com/Autumn-27/artex/seoos/internal/domain"
)

func conciseMetadata(value, fallback string, length int) string {
	if strings.TrimSpace(value) == "" {
		value = fallback
	}
	return bounded(strings.Join(strings.Fields(value), " "), length)
}

func validSlug(value string) bool {
	return value != "" && value == slug(value)
}

// Optimize edits metadata, outline structure and recommendations without
// replacing authored body text or changing its associated citations.
func Optimize(article domain.ContentArticle, brief domain.ContentBrief, assets []domain.SitePage) domain.ContentArticle {
	article.Title = conciseMetadata(article.Title, brief.Title, 76)
	article.Description = conciseMetadata(article.Description, brief.Description, 175)
	article.H1 = conciseMetadata(article.H1, brief.H1, 130)
	if article.Language == "" {
		article.Language = brief.Language
	}
	u, urlError := domain.EvidenceURL(brief.URL)
	if urlError == nil {
		article.Canonical = brief.URL
	}
	if !validSlug(article.Slug) {
		if urlError == nil {
			article.Slug = slug(path.Base(u.Path))
		}
		if article.Slug == "" {
			article.Slug = slug(brief.PrimaryKeyword)
		}
	}
	article.Sections = append([]domain.ArticleSection(nil), article.Sections...)
	prev := 1
	for i := range article.Sections {
		part := &article.Sections[i]
		if i < len(brief.Headings) && strings.TrimSpace(part.Heading) == "" {
			part.Heading = brief.Headings[i].Heading
		}
		part.Level = min(max(part.Level, 2), min(prev+1, 6))
		prev = part.Level
	}
	// URLs discovered in scope can be editorially useful. Leave the intended
	// target out of its own link list and do not synthesize URL destinations.
	article.InternalLinks = []string{}
	if urlError == nil {
		origin := u.Scheme + "://" + u.Host
		all := matchingLinks(origin, article.Canonical, brief.PrimaryKeyword, brief.Language, assets)
		article.InternalLinks = appendUnique(article.InternalLinks, 6, all...)
		for _, candidate := range brief.InternalLinks {
			if known := exactAsset(assets, candidate); known != nil && assetReady(*known) &&
				ownsPageLanguage(*known, article.Language) && sitePage(origin, known.URL) &&
				canonicalKey(known.URL) != canonicalKey(article.Canonical) {
				article.InternalLinks = appendUnique(article.InternalLinks, 6, known.URL)
			}
		}
	}
	if len(article.ImageAlts) == 0 {
		article.ImageAlts = []string{}
		for _, suggestion := range brief.Images {
			article.ImageAlts = appendUnique(article.ImageAlts, 6, bounded(suggestion, 125))
		}
	}
	if len(article.Schema) == 0 {
		article.Schema = []string{"Article"}
	}
	return article
}

func direction(value *float64, defaultValue float64) float64 {
	if value == nil || math.IsNaN(*value) || math.IsInf(*value, 0) {
		return defaultValue
	}
	return *value
}

func agedPage(page domain.SitePage, now time.Time) bool {
	return page.LastModified != nil && !page.LastModified.IsZero() && page.LastModified.Before(now.AddDate(-1, 0, 0))
}

func requestsNoIndex(robots string) bool {
	for _, value := range strings.FieldsFunc(strings.ToLower(robots), func(r rune) bool { return r == ',' || r == ' ' || r == '\n' }) {
		if value == "noindex" || value == "none" {
			return true
		}
	}
	return false
}

// Refresh evaluates a captured version of the existing page. It returns the
// saved snapshot to anchor any update. Recommendations never mutate the CMS.
func Refresh(w domain.ContentWorkspace) (domain.ContentRefreshResult, error) {
	if !hasResearch(w) {
		return domain.ContentRefreshResult{}, domain.ErrContentPrerequisite
	}
	strategy := w.Strategy
	if strategy == nil {
		derived, err := Strategy(w)
		if err != nil {
			return domain.ContentRefreshResult{}, err
		}
		strategy = &derived
	}
	assets := ownedAssets(w)
	page := exactAsset(assets, strategy.TargetURL)
	if page == nil || page.SnapshotID == "" || strings.TrimSpace(page.Body) == "" {
		return domain.ContentRefreshResult{}, domain.ErrContentPrerequisite
	}
	out := domain.ContentRefreshResult{Action: "keep", TargetURL: page.URL, SnapshotID: page.SnapshotID, Reasons: []string{}}
	signal := w.RefreshSignals
	declining, sourcesChanged, intentChanged, serpChanged := false, false, false, false
	if canonicalKey(signal.EvidenceURL) != "" {
		// Negative traffic/CTR values and an increase in rank number are declines.
		declining = direction(signal.TrafficChange, 0) <= -15 || direction(signal.RankChange, 0) >= 3 ||
			direction(signal.CTRChange, 0) <= -1
		sourcesChanged, intentChanged, serpChanged = signal.SourceChanged, signal.IntentChanged, signal.SERPChanged
	}
	if declining {
		out.Reasons = append(out.Reasons, "The recorded performance signal shows a decline. Evidence: "+signal.EvidenceURL)
	}
	if sourcesChanged {
		out.Reasons = append(out.Reasons, "The recorded first-party information changed. Evidence: "+signal.EvidenceURL)
	}
	if intentChanged || serpChanged {
		out.Reasons = append(out.Reasons, "The recorded search result or intent changed. Evidence: "+signal.EvidenceURL)
	}

	now := time.Now().UTC()
	currentEvidenceIncomplete := false
	if w.Brief != nil {
		observed := wordSet(tokens(joinText(pageText(*page), page.Body), 3800), true)
		currentEvidenceIncomplete = textCoverage(w.Brief.Topics, observed) < .60
	}
	switch {
	case strategy.Action == "merge":
		out.Action = "merge"
		out.Reasons = appendUnique(out.Reasons, 16, strategy.Reasons...)
	case requestsNoIndex(page.Robots):
		out.Action = "review_noindex"
		out.Reasons = append(out.Reasons, "The captured page declares noindex. Confirm that the intent and business value still justify this directive.")
	case intentChanged || strategy.Action == "adjust_type" || (sourcesChanged && currentEvidenceIncomplete):
		out.Action = "rewrite"
		out.Reasons = append(out.Reasons, "The target requires substantial changes to match its current search purpose or source material.")
	case declining || sourcesChanged || serpChanged || currentEvidenceIncomplete || agedPage(*page, now):
		out.Action = "minor_update"
		out.Reasons = append(out.Reasons, "Keep the URL and update stale, missing or changed sections against the captured baseline.")
	default:
		out.Reasons = append(out.Reasons, "The saved evidence contains no material change requiring a content rewrite.")
	}
	return out, nil
}
