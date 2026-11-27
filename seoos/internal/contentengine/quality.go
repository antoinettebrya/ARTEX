package contentengine

import (
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Autumn-27/artex/seoos/internal/domain"
)

const qualityAlgorithm = "content-quality/v1"

var (
	outboundURL = regexp.MustCompile(`(?i)https?://[^\s<>"'()\]]+`)
	activeHTML  = regexp.MustCompile(`(?i)<\s*(?:script|iframe|object|embed|form|img|svg|a)(?:\s|/?>)|javascript\s*:`)
)

func textCoverage(checks []string, observed stringSet) float64 {
	qualified := 0
	for _, check := range checks {
		if coverage(check, observed) >= .55 {
			qualified++
		}
	}
	if len(checks) == 0 {
		return 1
	}
	return ratio(qualified, len(checks))
}

func structure(article domain.ContentArticle) (bool, int) {
	prev, nonempty := 1, 0
	seenHeadings := stringSet{}
	if len(article.Sections) < 2 {
		return false, 0
	}
	for _, section := range article.Sections {
		key := domain.KeywordKey(section.Heading)
		if _, repeat := seenHeadings[key]; repeat || key == "" ||
			section.Level < 2 || section.Level > 6 || section.Level > prev+1 {
			return false, nonempty
		}
		seenHeadings[key] = struct{}{}
		prev = section.Level
		if len(tokens(section.Body, 7)) >= 7 {
			nonempty++
		}
	}
	return nonempty == len(article.Sections), nonempty
}

func scriptRatio(text, code string) float64 {
	var count, match, kana int
	for _, character := range text {
		if !unicode.IsLetter(character) {
			continue
		}
		count++
		switch code {
		case "en":
			if unicode.Is(unicode.Latin, character) {
				match++
			}
		case "zh":
			if unicode.Is(unicode.Han, character) {
				match++
			}
		case "ja":
			if unicode.In(character, unicode.Han, unicode.Katakana, unicode.Hiragana) {
				match++
			}
			if unicode.In(character, unicode.Katakana, unicode.Hiragana) {
				kana++
			}
		case "th":
			if unicode.Is(unicode.Thai, character) {
				match++
			}
		case "my":
			if unicode.Is(unicode.Myanmar, character) {
				match++
			}
		case "km":
			if unicode.Is(unicode.Khmer, character) {
				match++
			}
		}
	}
	value := ratio(match, count)
	if code == "ja" && ratio(kana, match) < .08 {
		return min(value, .35)
	}
	return value
}

func conciseRatio(body, code string) float64 {
	maximum := 45
	if code != "en" {
		maximum = 125
	}
	var count, concise int
	for _, sentence := range sentences(body) {
		if n := len(tokens(sentence, maximum+1)); n > 0 {
			count++
			if n <= maximum {
				concise++
			}
		}
	}
	return ratio(concise, count)
}

func trustCoverage(w domain.ContentWorkspace, now time.Time, text string) (int, bool) {
	known := stringSet{}
	for _, source := range w.Sources {
		if credibleSource(source, now) && freshSource(source, "", now) {
			known[canonicalKey(source.URL)] = struct{}{}
		}
	}
	for _, link := range outboundURL.FindAllString(text, -1) {
		address := strings.TrimRight(link, ".,!?。！？។")
		if _, approved := known[canonicalKey(address)]; !approved && !sitePage(w.SiteURL, address) {
			return len(known), false
		}
	}
	return len(known), true
}

func currentFacts(w domain.ContentWorkspace, now time.Time) []domain.ContentFact {
	sources := make(map[string]domain.ContentSource, len(w.Sources))
	for _, source := range w.Sources {
		if credibleSource(source, now) {
			sources[source.ID] = source
		}
	}
	var out []domain.ContentFact
	for _, fact := range w.Facts {
		if fact.Market != w.Project.Market {
			continue
		}
		if fact.Status == factVerified {
			var trusted bool
			liveHosts := stringSet{}
			for _, evidence := range fact.Evidence {
				if source, ok := sources[evidence.SourceID]; ok && !evidence.Conflict && freshSource(source, evidence.Excerpt, now) {
					if source.Kind != "authorized" {
						trusted = true
						break
					}
					address, _ := domain.EvidenceURL(source.URL)
					liveHosts[strings.ToLower(address.Hostname())] = struct{}{}
				}
			}
			if !trusted && len(liveHosts) < 2 {
				fact.Status = factStale
			}
		}
		out = append(out, fact)
	}
	return out
}

func competitorStructure(w domain.ContentWorkspace, outline []domain.ArticleSection) float64 {
	headings := stringSet{}
	for _, part := range outline {
		if key := domain.KeywordKey(part.Heading); key != "" {
			headings[key] = struct{}{}
		}
	}
	var best float64
	for _, record := range w.Evidence.Competitors {
		observed := stringSet{}
		for _, heading := range record.Page.Headings {
			if key := domain.KeywordKey(heading.Text); key != "" {
				observed[key] = struct{}{}
			}
		}
		if len(observed) > 1 {
			best = max(best, ratio(common(observed, headings), len(headings)))
		}
	}
	return best
}

func decimal(value float64) float64 {
	return math.Round(value*10) / 10
}

// Quality applies the same evidence-bound rules regardless of the writer or
// reviewer. Independent judgment is necessary for passing and cannot override
// material fact, duplicate content, or scope failures.
func Quality(w domain.ContentWorkspace, review *domain.EditorialReview) domain.ContentQuality {
	result := domain.ContentQuality{
		Algorithm: qualityAlgorithm, ReviewedAt: time.Now().UTC(), Dimensions: []domain.QualityDimension{},
		Issues: []string{}, Suggestions: []string{},
	}
	if w.Article == nil || w.Brief == nil {
		result.Issues = append(result.Issues, "The current brief and complete draft are required.")
		return result
	}
	a, brief := *w.Article, *w.Brief
	body := articleText(a)
	code := languageCode(brief.Language)
	allText := joinText(a.Title, a.Description, a.H1, body)
	words := tokens(body, 3800)
	observed := wordSet(tokens(allText, 4000), true)
	intro := wordSet(tokens(joinText(a.H1, a.Title, bounded(body, 1200)), 3800), true)
	keywordFit := coverage(brief.PrimaryKeyword, intro)
	pageMatch := strings.TrimSpace(brief.PageType) != "" && brief.PageType == searchIntent(w.Evidence.Keyword).PageType
	topicFit := textCoverage(brief.Topics, observed)
	questionFit := textCoverage(brief.Questions, observed)
	entityFit := textCoverage(brief.Entities, observed)
	maxDuplicate, relatedURL := nearestDuplicate(body, brief.URL, ownedAssets(w))
	sharedStructure := competitorStructure(w, a.Sections)
	facts := FactCheck(a, currentFacts(w, result.ReviewedAt))
	verified := 0
	for _, assertion := range facts.Claims {
		if assertion.Status == factVerified {
			verified++
		}
	}
	sourceCount, trustedLinks := trustCoverage(w, result.ReviewedAt, allText)
	sequence, substantial := structure(a)
	scriptFit := scriptRatio(body, code)
	concise := conciseRatio(body, code)
	independent := review != nil && strings.TrimSpace(review.Model) != ""
	authorNamed := strings.TrimSpace(a.Author) != ""
	readerFit := 0.0
	if independent && review.OriginalValue {
		readerFit = 1
	}
	intentReview := 0.0
	if independent && review.Intent {
		intentReview = 1
	}
	wantLength := 85
	if code != "en" {
		wantLength = 150
	}
	depth := min(1, float64(len(words))/float64(wantLength))
	canonicalFit := sitePage(w.SiteURL, a.Canonical) && canonicalKey(a.Canonical) == canonicalKey(brief.URL)
	var titles int
	for _, field := range []string{a.Title, a.Description, a.H1} {
		if strings.TrimSpace(field) != "" {
			titles++
		}
	}
	seoFit := ratio(titles, 4)
	if canonicalFit {
		seoFit += .25
	}
	factFit := 1.0
	if len(facts.Claims) > 0 {
		factFit = ratio(verified, len(facts.Claims))
	}
	sourceFit := .0
	if sourceCount > 0 && trustedLinks {
		sourceFit = .75
	}
	if authorNamed {
		sourceFit += .25
	}

	add := func(name string, weight int, measured float64, detail string, metrics map[string]float64) {
		points := decimal(float64(weight) * min(1, max(0, measured)))
		result.Dimensions = append(result.Dimensions, domain.QualityDimension{
			Name: name, Score: points, Weight: weight, Explanation: detail, Metrics: metrics,
		})
		result.Score += points
	}
	add("search_intent", 13, .55*keywordFit+.25*intentReview+.20*flag(pageMatch),
		"Combines local keyword coverage, the saved search intent and independent intent review.",
		map[string]float64{"keyword_coverage": keywordFit, "brief_matches_intent": flag(pageMatch), "independent_pass": intentReview})
	add("completeness", 12, .6*topicFit+.4*questionFit,
		"Measures exact and token coverage of the brief's topics and reader questions.",
		map[string]float64{"topic_coverage": topicFit, "question_coverage": questionFit})
	add("original_value", 12, .30*(1-maxDuplicate)+.25*(1-sharedStructure)+.45*readerFit,
		"Combines independently assessed usefulness, similarity to existing pages and competitor-outline overlap.",
		map[string]float64{"existing_body_similarity": maxDuplicate, "competitor_heading_overlap": sharedStructure, "independent_pass": readerFit})
	add("fact_accuracy", 16, factFit,
		"Counts verified material claims in the actual draft and metadata.",
		map[string]float64{"verified_claims": float64(verified), "material_claims": float64(len(facts.Claims))})
	add("credibility", 9, sourceFit,
		"Checks captured first-party sources, cited URLs and a named author.",
		map[string]float64{"captured_sources": float64(sourceCount), "registered_citations": flag(trustedLinks), "named_author": flag(authorNamed)})
	add("topic_depth", 6, .65*depth+.35*topicFit,
		"Measures substantive body coverage alongside the brief's topics.",
		map[string]float64{"body_units": float64(len(words)), "topic_coverage": topicFit})
	add("entity_coverage", 5, entityFit,
		"Measures coverage of entities named in scoped research when supplied.",
		map[string]float64{"entity_coverage": entityFit, "expected_entities": float64(len(brief.Entities))})
	add("structure", 6, .6*flag(sequence)+.4*ratio(substantial, len(a.Sections)),
		"Checks distinct headings, valid nesting and substantive body text per section.",
		map[string]float64{"heading_sequence_ok": flag(sequence), "substantial_sections": float64(substantial)})
	add("readability", 5, .45*scriptFit+.25*concise+.30*flag(independent && review.Language),
		"Combines locale script, sentence length and separate language review.",
		map[string]float64{"script_ratio": scriptFit, "concise_sentence_ratio": concise})
	add("duplicate_content", 6, 1-maxDuplicate,
		"Compares body shingles and topic vocabulary with other captured pages in the same language and site.",
		map[string]float64{"maximum_similarity": maxDuplicate})
	add("seo_metadata", 6, seoFit,
		"Checks title, description, H1 and consistency with the in-scope canonical target.",
		map[string]float64{"present_fields": float64(titles), "canonical_matches_target": flag(canonicalFit)})
	add("reader_value", 4, .65*readerFit+.35*depth,
		"Uses independent editorial judgment and substantive coverage, without using keyword density as a target.",
		map[string]float64{"independent_pass": readerFit, "body_depth": depth})
	result.Score = decimal(result.Score)

	block := func(issue, suggestion string) {
		result.Issues = append(result.Issues, issue)
		result.Suggestions = append(result.Suggestions, suggestion)
	}
	if !hasResearch(w) || brief.Country != w.Project.Market.CountryCode || languageCode(w.Project.Market.LanguageCode) != code ||
		domain.KeywordKey(brief.PrimaryKeyword) != domain.KeywordKey(w.Project.Keyword) {
		block("The current brief lacks verified linkage to the project's market and SERP.", "Regenerate the brief from the intended market's saved evidence.")
	}
	if !facts.Passed {
		block("The draft contains material assertions without verified evidence.", "Correct or remove those assertions and cite the matching current fact IDs.")
	}
	if sourceCount == 0 || !trustedLinks {
		block("Source provenance is incomplete.", "Collect a first-party source and use only recorded source citations.")
	}
	if maxDuplicate >= .84 {
		block(fmt.Sprintf("Possible duplicate of %s (%.0f%% similarity).", relatedURL, maxDuplicate*100), "Consolidate related pages or add original analysis and concrete reader value.")
	}
	if scriptFit < .52 || code != languageCode(a.Language) {
		block("The draft language does not match the locale required by the brief.", "Write in the target locale and re-run independent language review.")
	}
	if !sequence || len(words) < 34 {
		block("The article needs a valid heading hierarchy and substantial text.", "Add specific examples and steps under distinct headings.")
	}
	if titles != 3 || !canonicalFit || strings.TrimSpace(a.Slug) == "" || keywordFit < .60 {
		block("One or more essential metadata or topic checks failed.", "Review the title, H1, description, slug, target topic and canonical.")
	}
	if utf8.RuneCountInString(a.Title) > 90 || utf8.RuneCountInString(a.Description) > 210 {
		block("The title or description exceeds the editor's metadata limit.", "Use concise metadata that preserves the reader's intent.")
	}
	if activeHTML.MatchString(allText) {
		block("An unsafe markup string appears in the draft.", "Enter plain text in article fields.")
	}
	if !independent || !review.Passed || !review.Intent || !review.Language || !review.OriginalValue {
		block("Independent editorial review has not passed all required checks.", "Use a separate configured reviewer and address their issues before preparing any release.")
	}
	if review != nil {
		result.Suggestions = appendUnique(result.Suggestions, 25, review.Issues...)
	}
	if result.Score < 75 {
		block("The weighted quality score is below the review threshold.", "Improve the lowest-scoring dimensions and re-run the gate.")
	}
	result.Passed = len(result.Issues) == 0
	return result
}

func flag(value bool) float64 {
	if value {
		return 1
	}
	return 0
}
