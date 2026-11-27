package researchengine

import (
	"net"
	"net/url"
	"path"
	"sort"
	"strings"
	"unicode"

	"github.com/Autumn-27/artex/seoos/internal/domain"
)

type features map[string]struct{}

func normalizedText(value string) string {
	value = strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			return r
		}
		return ' '
	}, domain.KeywordKey(value))
	return strings.Join(strings.Fields(value), " ")
}

func denseScript(r rune) bool {
	return unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul)
}

var stopWords = features{
	"a": {}, "an": {}, "the": {}, "and": {}, "or": {}, "for": {}, "to": {}, "of": {}, "in": {}, "on": {},
	"at": {}, "with": {}, "is": {}, "from": {}, "how": {}, "where": {}, "by": {}, "www": {}, "com": {},
	"de": {}, "la": {}, "le": {}, "les": {}, "et": {}, "der": {}, "die": {}, "das": {}, "und": {},
}

func wordFeatures(value string) features {
	out := features{}
	for _, field := range strings.Fields(normalizedText(value)) {
		letters := []rune(field)
		for start := 0; start < len(letters); {
			dense := denseScript(letters[start])
			end := start + 1
			for end < len(letters) && denseScript(letters[end]) == dense {
				end++
			}
			if dense && end-start > 1 {
				for cursor := start; cursor+1 < end; cursor++ {
					out[string(letters[cursor:cursor+2])] = struct{}{}
				}
			} else {
				word := string(letters[start:end])
				if _, skip := stopWords[word]; !skip {
					out[word] = struct{}{}
				}
			}
			start = end
		}
	}
	return out
}

func hasPhrase(text, cue string) bool {
	if strings.ContainsFunc(cue, denseScript) {
		return strings.Contains(text, cue)
	}
	return strings.Contains(" "+text+" ", " "+cue+" ")
}

func intersection(a, b features) int {
	if len(b) < len(a) {
		a, b = b, a
	}
	var overlap int
	for v := range a {
		if _, ok := b[v]; ok {
			overlap++
		}
	}
	return overlap
}

func jaccard(a, b features) float64 {
	overlap := intersection(a, b)
	if overlap == 0 {
		return 0
	}
	return float64(overlap) / float64(len(a)+len(b)-overlap)
}

func canonicalKey(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Opaque != "" || u.User != nil || u.Hostname() == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return ""
	}
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	port := u.Port()
	if port == "" || u.Scheme == "https" && port == "443" || u.Scheme == "http" && port == "80" {
		if strings.Contains(host, ":") {
			host = "[" + host + "]"
		}
		u.Host = host
	} else {
		u.Host = net.JoinHostPort(host, port)
	}
	u.Fragment = ""
	u.RawFragment = ""
	if u.Path == "" {
		u.Path = "/"
	}
	// Ignore known tracking parameters, preserve content parameters and the trailing slash.
	if query, err := url.ParseQuery(u.RawQuery); err == nil {
		for key := range query {
			lower := strings.ToLower(key)
			if strings.HasPrefix(lower, "utm_") || lower == "gclid" || lower == "fbclid" || lower == "msclkid" {
				delete(query, key)
			}
		}
		u.RawQuery = query.Encode()
	}
	u.ForceQuery = false
	return u.String()
}

func inSite(siteURL, pageURL string) bool {
	site, page := canonicalKey(siteURL), canonicalKey(pageURL)
	if site == "" || page == "" {
		return false
	}
	base, _ := url.Parse(site)
	got, _ := url.Parse(page)
	if base.Scheme != got.Scheme || base.Host != got.Host {
		return false
	}
	scope := path.Clean(base.Path)
	pagePath := path.Clean(got.Path)
	return scope == "/" || pagePath == scope || strings.HasPrefix(pagePath, scope+"/")
}

func canonicalTarget(page domain.PageEvidence) string {
	if strings.TrimSpace(page.CanonicalURL) == "" {
		return ""
	}
	base, err := url.Parse(page.URL)
	target, targetErr := url.Parse(strings.TrimSpace(page.CanonicalURL))
	if err != nil || targetErr != nil {
		return ""
	}
	return canonicalKey(base.ResolveReference(target).String())
}

func ownedSamples(siteURL string, pages []domain.PageEvidence) []domain.PageEvidence {
	result := make([]domain.PageEvidence, 0, len(pages))
	for _, page := range pages {
		if inSite(siteURL, page.URL) {
			result = append(result, page)
		}
	}
	return result
}

func organic(entries []domain.SERPEntry) []domain.SERPEntry {
	selected := make([]domain.SERPEntry, 0, min(len(entries), 10))
	seen := features{}
	ranks := map[int]bool{}
	sorted := append([]domain.SERPEntry(nil), entries...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Rank < sorted[j].Rank })
	for _, entry := range sorted {
		key := canonicalKey(entry.URL)
		if entry.Rank < 1 || entry.Rank > 10 || key == "" || ranks[entry.Rank] {
			continue
		}
		ranks[entry.Rank] = true
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		entry.URL = key
		selected = append(selected, entry)
	}
	return selected
}

func evidenceText(page domain.PageEvidence) string {
	text := []string{page.Title, page.Description}
	text = append(text, page.Topics...)
	text = append(text, page.Entities...)
	for _, heading := range page.Headings {
		if heading.Level >= 1 && heading.Level <= 6 {
			text = append(text, heading.Text)
		}
	}
	return strings.TrimSpace(strings.Join(text, " "))
}

func pageWords(page domain.PageEvidence) features {
	value := evidenceText(page)
	if parsed, err := url.Parse(page.URL); err == nil {
		value += " " + parsed.Path
	}
	return wordFeatures(value)
}

func observedPage(entry domain.SERPEntry) domain.PageEvidence {
	return domain.PageEvidence{
		URL: entry.URL, Title: entry.Title, Description: entry.Description, PageType: entry.PageType,
	}
}

func observedType(raw string) string {
	switch normalizedText(raw) {
	case "guide", "article", "blog", "blog post", "article page", "blogposting", "tutorial", "faq page", "faqpage", "educational":
		return pageGuide
	case "comparison", "review", "reviews", "roundup", "comparison page":
		return pageComparison
	case "product", "product page", "product detail", "product listing", "purchase", "checkout", "booking":
		return pageProduct
	case "category", "category page", "collection", "list", "listings", "catalog":
		return pageCategory
	case "homepage", "home page", "official site", "brand", "login", "login page", "loginpage":
		return pageHome
	case "local landing", "local landing page", "local business", "localbusiness", "store", "location page", "directory", "restaurant":
		return pageLocal
	default:
		return ""
	}
}

// PageType infers a usable page category from declared markup, title and URL.
func PageType(page domain.PageEvidence) string {
	if kind := observedType(page.PageType); kind != "" {
		return kind
	}
	for _, schema := range page.Schema {
		if key := normalizedText(schema); key == "faqpage" || key == "faq page" {
			// A page can include FAQ markup as one section of a different page type.
			continue
		}
		if kind := observedType(schema); kind != "" {
			return kind
		}
	}
	parsed, err := url.Parse(page.URL)
	if err != nil || parsed.Hostname() == "" {
		return ""
	}
	tokens := normalizedText(parsed.Path + " " + page.Title)
	patterns := []struct {
		kind string
		cues []string
	}{
		{pageLocal, []string{"directions", "near me", "hours", "locations", "store locator", "附近", "营业时间", "アクセス"}},
		{pageComparison, []string{"compare", "comparison", "review", "reviews", "versus", "vs", "alternatives", "测评", "評價", "比较", "比較"}},
		{pageProduct, []string{"product", "products", "checkout", "book appointment", "商品詳細", "购买", "購入"}},
		{pageCategory, []string{"catalog", "category", "collection", "shop", "商品一覧", "目录", "目錄"}},
		{pageGuide, []string{"guide", "how to", "tutorial", "what is", "blog", "learn", "教程", "指南", "とは", "使い方"}},
		{pageHome, []string{"login", "sign in", "official site", "官网", "官網", "公式サイト"}},
	}
	for _, signal := range patterns {
		for _, cue := range signal.cues {
			if hasPhrase(tokens, cue) {
				return signal.kind
			}
		}
	}
	if parsed.Path == "" || parsed.Path == "/" {
		return pageHome
	}
	return ""
}

func fitForType(intent domain.SearchIntent, typ string) float64 {
	if typ == "" || !validIntent(intent.Primary) || intent.Confidence == 0 {
		return 0
	}
	if typ == intent.PageType || compatibleType(intent.Primary, typ) {
		return 1
	}
	if compatibleType(intent.Secondary, typ) {
		return 0.5
	}
	return 0
}

func compatibleType(intent, typ string) bool {
	return typ == preferredPage(intent) || intent == commercial && typ == pageCategory
}
