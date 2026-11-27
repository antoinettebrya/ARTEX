package researchprovider

import (
	"encoding/json"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Autumn-27/artex/seoos/internal/domain"
)

const (
	maxSections = 48
	maxLabels   = 24
)

type contentResult struct {
	Progress string `json:"crawl_progress"`
	Items    []struct {
		Type       string       `json:"type"`
		StatusCode int          `json:"status_code"`
		FetchTime  string       `json:"fetch_time"`
		Content    *pageContent `json:"page_content"`
	} `json:"items"`
}

type pageContent struct {
	Main      []contentSection `json:"main_topic"`
	Secondary []contentSection `json:"secondary_topic"`
}

type contentSection struct {
	Heading   string      `json:"h_title"`
	Level     json.Number `json:"level"`
	Author    string      `json:"author"`
	Primary   []struct{}  `json:"primary_content"`
	Secondary []struct{}  `json:"secondary_content"`
	Tables    []struct{}  `json:"table_content"`
}

func readPageContent(response *domain.DataResponse, task apiTask, target string) error {
	if !matchesPageEcho(task, target) {
		return errPageMissing
	}
	pending := false
	for _, row := range task.Result {
		var result contentResult
		if err := json.Unmarshal(row, &result); err != nil {
			return errInvalidResult
		}
		if result.Progress == "in_progress" {
			pending = true
			continue
		}
		for _, item := range result.Items {
			if item.Type != "content_parsing_element" || item.StatusCode < 200 || item.StatusCode >= 300 || item.Content == nil {
				continue
			}
			if collected := sourceTime(item.FetchTime, response.CollectedAt); collected != nil {
				response.CollectedAt = *collected
			}
			page := &domain.PageEvidence{URL: target, Source: response.Source}
			pageLabels := evidenceLabels{}
			seenSections := make(map[string]struct{})
			addSections(page, &pageLabels, seenSections, item.Content.Main, true)
			addSections(page, &pageLabels, seenSections, item.Content.Secondary, false)
			response.Page = page
			return nil
		}
	}
	if pending {
		return errCrawlPending
	}
	return errPageMissing
}

func addSections(page *domain.PageEvidence, labels *evidenceLabels, seen map[string]struct{}, sections []contentSection, main bool) {
	faqLevel := 0
	for _, section := range sections {
		addEvidenceLabel(&page.Entities, &labels.entities, declaredName(section.Author))
		height, err := strconv.Atoi(section.Level.String())
		if err != nil || height < 1 || height > 6 {
			continue
		}
		heading := evidenceLabel(section.Heading, 160)
		if heading == "" {
			continue
		}
		if faqLevel > 0 && height <= faqLevel {
			faqLevel = 0
		}
		isFAQHeading := clearlyFAQ(heading)
		if isFAQHeading {
			faqLevel = height
		} else if faqLevel > 0 && (strings.HasSuffix(heading, "?") || strings.HasSuffix(heading, "？")) {
			addEvidenceLabel(&page.FAQ, &labels.faq, heading)
		}
		if !isFAQHeading && (main || len(section.Primary) > 0) {
			addEvidenceLabel(&page.Topics, &labels.topics, heading)
		}

		key := strconv.Itoa(height) + ":" + domain.KeywordKey(heading)
		if _, ok := seen[key]; ok || len(page.Sections) >= maxSections {
			continue
		}
		seen[key] = struct{}{}
		page.Headings = append(page.Headings, domain.HeadingEvidence{Level: height, Text: heading})
		page.Sections = append(page.Sections, domain.PageSectionEvidence{
			Level: height, Heading: heading,
			PrimaryBlocks: len(section.Primary), SecondaryBlocks: len(section.Secondary), Tables: len(section.Tables),
		})
	}
}

func clearlyFAQ(title string) bool {
	title = strings.Trim(domain.KeywordKey(title), " :?.!。")
	switch title {
	case "faq", "faqs", "frequently asked questions", "questions fréquentes", "foire aux questions",
		"preguntas frecuentes", "perguntas frequentes", "häufig gestellte fragen", "domande frequenti",
		"よくある質問", "よくあるご質問", "常见问题", "常見問題", "常见问题解答", "常見問題解答", "자주 묻는 질문":
		return true
	default:
		return false
	}
}

type microdataResult struct {
	Progress string `json:"crawl_progress"`
	Items    []struct {
		Inspection markupNode `json:"inspection_info"`
	} `json:"items"`
}

type markupNode struct {
	Types  []string      `json:"types"`
	Fields []markupField `json:"fields"`
}

type markupField struct {
	Name   string          `json:"name"`
	Value  json.RawMessage `json:"value"`
	Fields []markupField   `json:"fields"`
}

func readPageMicrodata(response *domain.DataResponse, task apiTask, target string) error {
	if !matchesPageEcho(task, target) {
		return errPageMissing
	}
	page := &domain.PageEvidence{URL: target, Source: response.Source}
	labels := evidenceLabels{}
	pending, completed := false, false
	for _, row := range task.Result {
		var result microdataResult
		if err := json.Unmarshal(row, &result); err != nil {
			return errInvalidResult
		}
		if result.Progress == "in_progress" {
			pending = true
			continue
		}
		completed = true
		for _, item := range result.Items {
			addMarkup(page, &labels, item.Inspection, "", false, 0)
		}
	}
	if pending && !completed {
		return errCrawlPending
	}
	page.MicrodataObserved = len(page.Schema) > 0
	response.Page = page
	return nil
}

func matchesPageEcho(task apiTask, target string) bool {
	requested, _ := publicPageURL(target)
	observed, ok := publicPageURL(task.Data.URL)
	return ok && samePageURL(observed, requested)
}

func addMarkup(page *domain.PageEvidence, labels *evidenceLabels, node markupNode, property string, faqQuestion bool, depth int) {
	if depth > 32 {
		return
	}
	var types []string
	addType := func(raw string) {
		label := schemaType(raw)
		if label != "" && !slices.Contains(types, label) {
			types = append(types, label)
			addEvidenceLabel(&page.Schema, &labels.schema, label)
		}
	}
	for _, value := range node.Types {
		addType(value)
	}
	var name string
	for _, field := range node.Fields {
		switch field.Name {
		case "@type":
			for _, value := range markupStrings(field.Value) {
				addType(value)
			}
		case "name":
			if values := markupStrings(field.Value); len(values) > 0 {
				name = values[0]
			}
		}
	}
	if faqQuestion && slices.Contains(types, "Question") {
		addEvidenceLabel(&page.FAQ, &labels.faq, evidenceLabel(name, 160))
	}
	if describesEntity(types, property) {
		addEvidenceLabel(&page.Entities, &labels.entities, declaredName(name))
	}

	isFAQPage := slices.Contains(types, "FAQPage")
	for _, field := range node.Fields {
		if len(field.Fields) > 0 {
			addMarkup(page, labels, markupNode{Fields: field.Fields}, field.Name, isFAQPage && field.Name == "mainEntity", depth+1)
		}
	}
}

func describesEntity(types []string, property string) bool {
	switch property {
	case "about", "mentions", "author", "brand", "publisher", "manufacturer":
		return true
	}
	for _, label := range types {
		switch label {
		case "Person", "Organization", "LocalBusiness", "Brand", "Product", "Place", "Event", "SoftwareApplication",
			"Book", "Movie", "Restaurant", "Thing", "Service", "MedicalCondition":
			return true
		}
	}
	return false
}

func schemaType(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "https://schema.org/")
	value = strings.TrimPrefix(value, "http://schema.org/")
	if len(value) == 0 || len(value) > 80 || value[0] < 'A' || value[0] > 'Z' {
		return ""
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			return ""
		}
	}
	return value
}

func markupStrings(raw json.RawMessage) []string {
	var single string
	if err := json.Unmarshal(raw, &single); err == nil && single != "" {
		return []string{single}
	}
	var multiple []string
	_ = json.Unmarshal(raw, &multiple)
	return multiple
}

type evidenceLabels struct {
	topics   map[string]struct{}
	entities map[string]struct{}
	faq      map[string]struct{}
	schema   map[string]struct{}
}

func addEvidenceLabel(labels *[]string, seen *map[string]struct{}, value string) {
	if value == "" || len(*labels) >= maxLabels {
		return
	}
	key := domain.KeywordKey(value)
	if *seen == nil {
		*seen = make(map[string]struct{})
	}
	if _, ok := (*seen)[key]; ok {
		return
	}
	(*seen)[key] = struct{}{}
	*labels = append(*labels, value)
}

func declaredName(value string) string {
	if strings.ContainsAny(value, "@/") {
		return ""
	}
	return evidenceLabel(value, 80)
}

func evidenceLabel(value string, limit int) string {
	if !utf8.ValidString(value) || strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return ""
	}
	value = strings.Join(strings.Fields(value), " ")
	if utf8.RuneCountInString(value) > limit {
		return ""
	}
	return value
}
