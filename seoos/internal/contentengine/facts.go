package contentengine

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/Autumn-27/artex/seoos/internal/domain"
	"github.com/google/uuid"
)

const (
	factVerified = "verified"
	factPartial  = "partially_verified"
	factUnknown  = "unverified"
	factConflict = "conflicting"
	factStale    = "stale"
)

var (
	numerals    = regexp.MustCompile(`[+−-]?[\p{Nd}]+(?:[.,٫/:–−-][\p{Nd}]+)*(?:[ \t]*[%％])?`)
	listOrdinal = regexp.MustCompile(`^[\p{Nd}]{1,3}[.)、][ \t]+`)
	negative    = regexp.MustCompile(`\b(?:not|never|cannot)\b|禁止|不得|できない|ไม่อนุญาต|မပြုရ|មិនអនុញ្ញាត`)
)

var materialCues = []string{
	"according to", "was founded", "is headquartered", "was appointed", "was released", "published by", "certified by",
	"is required", "are required", "by law", "regulation", "policy states", "is prohibited", "warranty", "refund", "currently costs",
	"supports only", "is available in", "eligibility", "effective from", "research found", "study found", "statistically significant",
	"必须", "不得", "依法", "保修", "退款", "售价", "政策规定", "研究显示", "产地为", "由…创立", "出版于",
	"義務", "法律で", "返金", "保証期間", "調査によると", "発売された", "เป็นข้อบังคับ", "กฎหมาย", "การรับประกัน", "งานวิจัยพบ",
	"ဥပဒေ", "အာမခံ", "သုတေသန", "ច្បាប់", "ការធានា", "ការស្រាវជ្រាវ",
}

var freshnessCues = []string{
	"price", "cost", "law", "regulation", "policy", "eligibility", "refund", "effective", "expires", "currently", "availability", "available now",
	"价格", "售价", "政策", "有效期", "现行", "規制", "現在", "価格", "กฎหมาย", "ราคา", "ဈေးနှုန်း", "ဥပဒေ", "ច្បាប់", "តម្លៃ",
}

func hasCue(text string, cues []string) bool {
	key := domain.KeywordKey(text)
	normalizedWords := " " + strings.Join(tokens(text, 600), " ") + " "
	for _, cue := range cues {
		if unicode.Is(unicode.Latin, []rune(cue)[0]) {
			if strings.Contains(normalizedWords, " "+cue+" ") {
				return true
			}
		} else if strings.Contains(key, cue) {
			return true
		}
	}
	return false
}

func material(text string) bool {
	text = outboundURL.ReplaceAllString(text, "")
	return numerals.MatchString(domain.KeywordKey(text)) || hasCue(text, materialCues)
}

func sentences(text string) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	var out []string
	runes := []rune(text)
	start := 0
	for i, r := range runes {
		switch r {
		case '.':
			if i > 0 && i+1 < len(runes) && unicode.IsDigit(runes[i-1]) && unicode.IsDigit(runes[i+1]) {
				continue
			}
			if i+1 < len(runes) && !unicode.IsSpace(runes[i+1]) {
				continue
			}
		case '\n', '!', '?', '。', '！', '？', '။', '។':
		default:
			continue
		}
		if value := strings.TrimSpace(string(runes[start : i+1])); value != "" {
			out = append(out, value)
		}
		start = i + 1
	}
	if tail := strings.TrimSpace(string(runes[start:])); tail != "" {
		out = append(out, tail)
	}
	return out
}

// claimKey preserves numbers, comparators, and negation. Only surrounding
// punctuation and whitespace are normalized for the deterministic check.
func claimKey(text string) string {
	text = strings.TrimSpace(domain.KeywordKey(text))
	text = listOrdinal.ReplaceAllString(text, "")
	text = strings.ReplaceAll(strings.ReplaceAll(text, "−", "-"), "％", "%")
	return strings.Trim(text, " \t\n\r.\"'“”‘’!?。！？။។")
}

func factFrame(key string) string {
	key = numerals.ReplaceAllString(key, " {number} ")
	key = negative.ReplaceAllString(key, "")
	return strings.Join(strings.Fields(key), " ")
}

type observedFact struct {
	excerpt    string
	confidence float64
	evidence   []domain.FactEvidence
	liveHosts  stringSet
	fresh      bool
}

func credibleSource(source domain.ContentSource, now time.Time) bool {
	return firstPartyKind(source.Kind) && source.ID != "" && canonicalKey(source.URL) != "" &&
		source.CollectedAt != nil && !source.CollectedAt.IsZero() && !source.CollectedAt.After(now.Add(time.Hour))
}

func freshSource(source domain.ContentSource, excerpt string, now time.Time) bool {
	if source.CollectedAt == nil || source.CollectedAt.Before(now.AddDate(0, -6, 0)) {
		return false
	}
	if !hasCue(excerpt, freshnessCues) || source.PublishedAt == nil {
		return true
	}
	return !source.PublishedAt.Before(now.AddDate(-2, 0, 0)) && !source.PublishedAt.After(now.Add(24*time.Hour))
}

// Facts extracts bounded candidate assertions from captured first-party pages.
// Comparisons between different observed values retain all source references.
func Facts(sources []domain.SourceDocument, market domain.Market) []domain.ContentFact {
	now := time.Now().UTC()
	return collectedFacts(sources, market, now)
}

func collectedFacts(sources []domain.SourceDocument, market domain.Market, now time.Time) []domain.ContentFact {
	byStatement := map[string]*observedFact{}
	byFrame := map[string][]string{}
	for _, source := range sources {
		if !credibleSource(source.ContentSource, now) {
			continue
		}
		u, _ := domain.EvidenceURL(source.URL)
		host := strings.ToLower(u.Hostname())
		observed := 0
		for _, entry := range sentences(source.Text) {
			// Whole sentences only. A truncated statement might omit a condition.
			if bounded(entry, 280) != entry || !material(entry) || strings.HasSuffix(entry, "?") ||
				strings.HasSuffix(entry, "？") || len(tokens(entry, 4)) < 4 {
				continue
			}
			key := claimKey(entry)
			fact := byStatement[key]
			if fact == nil {
				if len(byStatement) >= 96 {
					continue
				}
				fact = &observedFact{excerpt: entry, liveHosts: stringSet{}}
				byStatement[key] = fact
				frame := factFrame(key)
				byFrame[frame] = append(byFrame[frame], key)
			}
			known := false
			for _, evidence := range fact.evidence {
				known = known || evidence.SourceID == source.ID
			}
			if !known && len(fact.evidence) < 8 {
				fact.evidence = append(fact.evidence, domain.FactEvidence{SourceID: source.ID, Excerpt: entry})
			}
			if freshSource(source.ContentSource, entry, now) {
				fact.fresh = true
				fact.liveHosts[host] = struct{}{}
				c := .6
				if source.Kind != "authorized" {
					c = .90
				}
				fact.confidence = max(fact.confidence, c)
			}
			observed++
			if observed >= 24 {
				break
			}
		}
	}
	keys := make([]string, 0, len(byStatement))
	for key := range byStatement {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	results := make([]domain.ContentFact, 0, len(keys))
	marketKey := fmt.Sprintf("%s/%d/%s/%s/%s/", market.CountryCode, market.LocationCode, market.LanguageCode, market.SearchEngine, market.Device)
	for _, key := range keys {
		entry := byStatement[key]
		status := factPartial
		confidence := entry.confidence
		switch {
		case !entry.fresh:
			status, confidence = factStale, 0
		case confidence >= .90 || len(entry.liveHosts) > 1:
			status, confidence = factVerified, max(.85, confidence)
		}
		evidence := append([]domain.FactEvidence(nil), entry.evidence...)
		for _, alternative := range byFrame[factFrame(key)] {
			other := byStatement[alternative]
			if key == alternative || !other.fresh {
				continue
			}
			status, confidence = factConflict, 0
			for _, conflict := range other.evidence {
				if len(evidence) >= 12 {
					break
				}
				conflict.Conflict = true
				evidence = append(evidence, conflict)
			}
		}
		results = append(results, domain.ContentFact{
			ID:        uuid.NewSHA1(uuid.NameSpaceURL, []byte(marketKey+key)).String(),
			Statement: entry.excerpt, Status: status, Confidence: confidence, Market: market, Evidence: evidence,
		})
	}
	return results
}

func verdict(text string, rawIDs []string, facts map[string]domain.ContentFact, locale string) domain.ClaimResult {
	result := domain.ClaimResult{Text: text, Material: true, Status: factUnknown, FactIDs: []string{},
		Reason: "The rendered claim needs a matching, recorded first-party excerpt and its fact ID."}
	key := claimKey(text)
	for _, id := range rawIDs {
		fact, ok := facts[id]
		if !ok || len(fact.Evidence) == 0 || languageCode(fact.Market.LanguageCode) != languageCode(locale) {
			result.Reason = "A cited fact ID is missing, invalid, or from a different language."
			return result
		}
		matched := false
		for _, citation := range fact.Evidence {
			if citation.SourceID != "" && !citation.Conflict && claimKey(citation.Excerpt) == key {
				matched = true
				break
			}
		}
		if !matched || claimKey(fact.Statement) != key {
			result.Reason = "The citation does not support the complete rendered assertion as written."
			return result
		}
		result.FactIDs = appendUnique(result.FactIDs, 12, id)
		if fact.Status != factVerified {
			result.Status = fact.Status
			result.Reason = "The recorded evidence is partial, conflicting, stale, or unverified."
			return result
		}
	}
	if len(result.FactIDs) > 0 {
		result.Status, result.Reason = factVerified, "A current first-party excerpt supports the complete rendered assertion."
	}
	return result
}

func articleText(article domain.ContentArticle) string {
	var body strings.Builder
	for _, section := range article.Sections {
		body.WriteString(section.Body)
		body.WriteByte('\n')
	}
	return body.String()
}

// FactCheck audits both declared claims and material assertions found in the
// actual metadata and prose. A model's citation alone is never verification.
func FactCheck(article domain.ContentArticle, facts []domain.ContentFact) domain.FactReport {
	report := domain.FactReport{Passed: true, Claims: []domain.ClaimResult{}}
	factsByID := make(map[string]domain.ContentFact, len(facts))
	for _, fact := range facts {
		factsByID[fact.ID] = fact
	}
	type declaration struct {
		text string
		ids  []string
	}
	declared := map[string]declaration{}
	keys := []string{}
	for _, section := range article.Sections {
		for _, claim := range section.Claims {
			key := claimKey(claim.Text)
			if key == "" {
				continue
			}
			d, seen := declared[key]
			if !seen {
				keys = append(keys, key)
			}
			d.text = claim.Text
			d.ids = append(d.ids, claim.FactIDs...)
			declared[key] = d
		}
	}
	observed := stringSet{}
	text := []string{article.Title, article.Description, article.H1}
	for _, section := range article.Sections {
		text = append(text, section.Heading, section.Body)
	}
	add := func(result domain.ClaimResult) {
		report.Claims = append(report.Claims, result)
		report.Passed = report.Passed && result.Status == factVerified
	}
	for _, field := range text {
		for _, sentence := range sentences(field) {
			key := claimKey(sentence)
			if key == "" {
				continue
			}
			if _, seen := observed[key]; seen {
				continue
			}
			observed[key] = struct{}{}
			d, explicit := declared[key]
			// Numbered structure labels and neutral instructions are not
			// verifiable assertions after their presentation marker is removed.
			if explicit || material(key) {
				add(verdict(sentence, d.ids, factsByID, article.Language))
			}
		}
	}
	for _, key := range keys {
		if _, rendered := observed[key]; !rendered {
			d := declared[key]
			add(domain.ClaimResult{Text: d.text, Status: factUnknown, Material: true, FactIDs: []string{},
				Reason: "The declared citation is not attached to a complete rendered assertion."})
		}
	}
	return report
}
