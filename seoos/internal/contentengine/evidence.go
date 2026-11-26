// Package contentengine interprets saved SEO research and authoring evidence.
package contentengine

import (
	"net/url"
	"path"
	"sort"
	"strings"
	"unicode"

	"github.com/Autumn-27/artex/seoos/internal/domain"
)

func languageCode(value string) string {
	code, _, _ := strings.Cut(strings.ReplaceAll(strings.ToLower(strings.TrimSpace(value)), "_", "-"), "-")
	return code
}

func bounded(text string, limit int) string {
	text = strings.TrimSpace(strings.ToValidUTF8(text, ""))
	for pos := range text {
		if limit == 0 {
			return strings.TrimSpace(text[:pos])
		}
		limit--
	}
	return text
}

func appendUnique(list []string, limit int, values ...string) []string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		for _, prev := range list {
			if domain.KeywordKey(prev) == domain.KeywordKey(value) {
				value = ""
				break
			}
		}
		if value != "" && len(list) < limit {
			list = append(list, value)
		}
	}
	return list
}

func joinText(values ...string) string {
	return strings.Join(values, " ")
}

func sitePage(site, address string) bool {
	u, err := domain.EvidenceURL(address)
	return site != "" && err == nil && domain.BelongsToSite(site, u)
}

func canonicalKey(raw string) string {
	u, err := domain.EvidenceURL(raw)
	if err != nil {
		return ""
	}
	u.Host = strings.ToLower(u.Host)
	if (u.Scheme == "https" && u.Port() == "443") || (u.Scheme == "http" && u.Port() == "80") {
		u.Host = strings.TrimSuffix(strings.TrimSuffix(u.Host, ":443"), ":80")
	}
	u.Path = path.Clean("/" + strings.TrimPrefix(u.Path, "/"))
	u.RawPath = ""
	q := u.Query()
	for key := range q {
		k := strings.ToLower(key)
		if strings.HasPrefix(k, "utm_") || k == "gclid" || k == "msclkid" || k == "fbclid" {
			q.Del(key)
		}
	}
	u.RawQuery = q.Encode()
	u.ForceQuery = false
	return u.String()
}

func assetReady(page domain.SitePage) bool {
	return page.HTTPStatus >= 200 && page.HTTPStatus < 300 &&
		(strings.TrimSpace(page.Body) != "" || strings.TrimSpace(page.Title) != "")
}

func pageText(page domain.SitePage) string {
	values := []string{page.Title, page.Description}
	values = append(values, page.Topics...)
	for _, heading := range page.Headings {
		values = append(values, heading.Text)
	}
	return joinText(values...)
}

func ownsPageLanguage(page domain.SitePage, code string) bool {
	return strings.TrimSpace(page.Language) == "" || languageCode(page.Language) == languageCode(code)
}

func continuousScript(r rune) bool {
	return unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Thai, unicode.Myanmar, unicode.Khmer)
}

// tokens uses whole words in space-delimited text and grapheme-like units in
// scripts where most orthographic words are not separated by whitespace.
func tokens(text string, limit int) []string {
	var out []string
	var word strings.Builder
	var continuous bool
	flush := func() {
		if word.Len() > 0 && len(out) < limit {
			out = append(out, word.String())
		}
		word.Reset()
	}
	for _, r := range domain.KeywordKey(text) {
		switch {
		case unicode.IsLetter(r) || unicode.IsNumber(r):
			c := continuousScript(r)
			if c || continuous {
				flush()
			}
			continuous = c
			word.WriteRune(r)
		case unicode.IsMark(r) && word.Len() > 0:
			word.WriteRune(r)
		default:
			flush()
			continuous = false
		}
		if len(out) >= limit {
			return out
		}
	}
	flush()
	return out
}

var stopWords = map[string]bool{
	"a": true, "an": true, "and": true, "are": true, "about": true, "at": true, "by": true, "for": true, "from": true,
	"how": true, "in": true, "is": true, "it": true, "of": true, "on": true, "or": true, "that": true, "the": true,
	"their": true, "this": true, "to": true, "what": true, "when": true, "where": true, "which": true, "with": true, "you": true,
}

type stringSet map[string]struct{}

func wordSet(words []string, ignoreCommon bool) stringSet {
	out := make(stringSet, len(words))
	for _, word := range words {
		if !ignoreCommon || !stopWords[word] {
			out[word] = struct{}{}
		}
	}
	return out
}

func common(a, b stringSet) int {
	if len(a) > len(b) {
		a, b = b, a
	}
	count := 0
	for key := range a {
		if _, ok := b[key]; ok {
			count++
		}
	}
	return count
}

func coverage(target string, observed stringSet) float64 {
	terms := wordSet(tokens(target, 120), true)
	if len(terms) == 0 {
		return 0
	}
	return float64(common(terms, observed)) / float64(len(terms))
}

func ratio(count, total int) float64 {
	if total == 0 {
		return 0
	}
	return float64(count) / float64(total)
}

func fingerprint(words []string) stringSet {
	out := make(stringSet, len(words))
	for i := 0; i+2 < len(words); i++ {
		out[joinText(words[i], words[i+1], words[i+2])] = struct{}{}
	}
	return out
}

func duplicateSimilarity(base, comparison []string) float64 {
	if len(base) < 12 || len(comparison) < 12 {
		return 0
	}
	x, y := fingerprint(base), fingerprint(comparison)
	shared := common(x, y)
	jaccard := ratio(shared, len(x)+len(y)-shared)
	containment := ratio(shared, min(len(x), len(y)))
	wordsA, wordsB := wordSet(base, true), wordSet(comparison, true)
	vocab := common(wordsA, wordsB)
	topicSimilarity := ratio(vocab, len(wordsA)+len(wordsB)-vocab)
	return .55*jaccard + .35*containment + .10*topicSimilarity
}

func nearestDuplicate(text, skipURL string, assets []domain.SitePage) (float64, string) {
	var best float64
	var address string
	base, skip := tokens(text, 3800), canonicalKey(skipURL)
	for _, asset := range assets {
		if !assetReady(asset) || canonicalKey(asset.URL) == skip {
			continue
		}
		if score := duplicateSimilarity(base, tokens(asset.Body, 3800)); score > best ||
			(score == best && score > 0 && asset.URL < address) {
			best, address = score, asset.URL
		}
	}
	return best, address
}

func matchingLinks(site, exclude, topic, locale string, assets []domain.SitePage) []string {
	type candidate struct {
		url   string
		score float64
	}
	var matches []candidate
	for _, page := range assets {
		if !assetReady(page) || !sitePage(site, page.URL) || canonicalKey(page.URL) == canonicalKey(exclude) ||
			!ownsPageLanguage(page, locale) {
			continue
		}
		related := coverage(topic, wordSet(tokens(pageText(page), 600), true))
		if related >= .4 {
			matches = append(matches, candidate{url: page.URL, score: related})
		}
	}
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].score == matches[j].score {
			return matches[i].url < matches[j].url
		}
		return matches[i].score > matches[j].score
	})
	out := []string{}
	for _, match := range matches[:min(len(matches), 6)] {
		out = appendUnique(out, 6, match.url)
	}
	return out
}

func slug(value string) string {
	words := strings.FieldsFunc(domain.KeywordKey(value), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r) && !unicode.IsMark(r)
	})
	return strings.Trim(bounded(strings.Join(words, "-"), 72), "-")
}

func suggestedURL(base, keyword string) string {
	if value := slug(keyword); value != "" {
		if u, err := domain.EvidenceURL(base); err == nil {
			return u.JoinPath(url.PathEscape(value)).String()
		}
	}
	return ""
}
