package contentinventory

import (
	"bytes"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/net/html"
	"golang.org/x/net/html/charset"
	"golang.org/x/text/language"

	"github.com/Autumn-27/artex/seoos/internal/domain"
	"github.com/Autumn-27/artex/seoos/internal/researchengine"
)

const (
	maxPageText   = 96000
	maxDOMDepth   = 128
	maxTitleChars = 500
)

func extractDocument(raw []byte, contentType string, u *url.URL) (domain.SitePage, *time.Time, error) {
	page := domain.SitePage{State: "fetched", PageEvidence: domain.PageEvidence{URL: u.String(), Source: "site_http"}}
	switch pageMediaType(raw, contentType) {
	case "text/plain", "text/markdown":
		page.Body = paragraphText(string(raw))
		return page, nil, nil
	case "text/html", "application/xhtml+xml":
	default:
		page.State = "unsupported"
		return page, nil, nil
	}

	document, err := parseHTML(raw, contentType)
	if err != nil {
		return page, nil, err
	}
	var body, main, article *html.Node
	var published, modified *time.Time
	var typeSet = make(map[string]bool)
	var fallbackTitle, fallbackDescription string
	walkElements(document, 0, func(node *html.Node) {
		switch node.Data {
		case "html":
			page.Language = languageTag(attr(node, "lang"))
			if page.Language == "" {
				page.Language = languageTag(attr(node, "xml:lang"))
			}
		case "body":
			body = node
		case "main":
			if main == nil {
				main = node
			}
		case "article":
			if article == nil {
				article = node
			}
		case "title":
			if page.Title == "" && underHead(node) {
				page.Title = cleanLabel(documentText(node, false), maxTitleChars)
			}
		case "meta":
			key := firstNonEmpty(attr(node, "name"), attr(node, "property"), attr(node, "itemprop"))
			value := cleanLabel(attr(node, "content"), 1600)
			switch strings.ToLower(key) {
			case "description":
				if page.Description == "" {
					page.Description = value
				}
			case "og:description":
				fallbackDescription = value
			case "og:title":
				fallbackTitle = value
			case "robots":
				page.Robots = value
			case "article:published_time", "datepublished":
				if t := parseTimestamp(value); t != nil {
					published = t
				}
			case "article:modified_time", "datemodified":
				if t := parseTimestamp(value); t != nil {
					modified = t
				}
			}
		case "time":
			value := parseTimestamp(attr(node, "datetime"))
			if value != nil {
				switch strings.ToLower(attr(node, "itemprop")) {
				case "datepublished":
					published = firstTime(published, value)
				case "datemodified":
					modified = firstTime(modified, value)
				}
			}
		case "link":
			if page.CanonicalURL == "" && hasWord(attr(node, "rel"), "canonical") {
				page.CanonicalURL = metadataURL(u, attr(node, "href"))
			}
		case "script":
			if len(typeSet) < 64 && strings.EqualFold(strings.TrimSpace(attr(node, "type")), "application/ld+json") {
				var data any
				if code := node.FirstChild; code != nil && len(code.Data) <= 256<<10 && json.Unmarshal([]byte(code.Data), &data) == nil {
					var state schemaState
					state.walk(data, 0, typeSet)
					published = firstTime(published, state.published)
					modified = firstTime(modified, state.modified)
					page.FAQ = labelList(append(page.FAQ, state.questions...))
				}
			}
		}
		if len(typeSet) < 64 {
			for _, declared := range strings.Fields(attr(node, "itemtype")) {
				if label := schemaType(declared); label != "" {
					typeSet[label] = true
				}
				if len(typeSet) >= 64 {
					break
				}
			}
		}
	})

	for name := range typeSet {
		page.Schema = append(page.Schema, name)
	}
	sort.Strings(page.Schema)
	page.MicrodataObserved = len(page.Schema) > 0
	selected := main
	if selected == nil {
		selected = article
	}
	if selected == nil {
		selected = body
	}

	if selected != nil {
		page.Body = paragraphText(documentText(selected, true))
		extractSections(selected, u, &page, 0)
	}
	page.Title = firstNonEmpty(page.Title, cleanLabel(fallbackTitle, maxTitleChars))
	if page.Title == "" && len(page.Headings) > 0 && page.Headings[0].Level == 1 {
		page.Title = page.Headings[0].Text
	}
	page.Description = firstNonEmpty(page.Description, fallbackDescription)
	page.LastModified = modified
	page.PageType = researchengine.PageType(page.PageEvidence)
	return page, published, nil
}

func pageMediaType(raw []byte, header string) string {
	mediaType, _, _ := mime.ParseMediaType(header)
	if mediaType == "" {
		mediaType, _, _ = mime.ParseMediaType(http.DetectContentType(raw))
	}
	return mediaType
}

func parseHTML(raw []byte, contentType string) (*html.Node, error) {
	document, err := html.Parse(charsetReader(bytes.NewReader(raw), raw, contentType))
	if err != nil {
		return nil, &domain.DataError{Code: "inventory_invalid_html"}
	}
	return document, nil
}

func charsetReader(raw *bytes.Reader, data []byte, header string) io.Reader {
	encoding, name, certain := charset.DetermineEncoding(data, header)
	if name == "utf-8" || !certain && utf8.Valid(data) {
		return raw
	}
	return encoding.NewDecoder().Reader(raw)
}

func walkElements(root *html.Node, depth int, visit func(*html.Node)) {
	if depth >= maxDOMDepth {
		return
	}
	if root.Type == html.ElementNode {
		visit(root)
	}
	for child := root.FirstChild; child != nil; child = child.NextSibling {
		walkElements(child, depth+1, visit)
	}
}

func extractSections(node *html.Node, u *url.URL, page *domain.SitePage, depth int) {
	if depth >= maxDOMDepth || excluded(node) {
		return
	}
	if node.Type == html.ElementNode {
		name := node.Data
		if len(name) == 2 && name[0] == 'h' && name[1] >= '1' && name[1] <= '6' && len(page.Headings) < 128 {
			if heading := cleanLabel(documentText(node, true), maxTitleChars); heading != "" {
				page.Headings = append(page.Headings, domain.HeadingEvidence{Level: int(name[1] - '0'), Text: heading})
			}
		}
		if name == "img" && len(page.Images) < 128 {
			src := metadataURL(u, attr(node, "src"))
			if src == "" {
				src = metadataURL(u, attr(node, "data-src"))
			}
			if src == "" {
				if values := strings.Fields(attr(node, "srcset")); len(values) > 0 {
					src = metadataURL(u, strings.TrimSuffix(values[0], ","))
				}
			}
			if src != "" {
				page.Images = append(page.Images, domain.PageImage{URL: src, Alt: cleanLabel(attr(node, "alt"), maxTitleChars)})
			}
		}
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		extractSections(child, u, page, depth+1)
	}
}

func documentText(node *html.Node, visible bool) string {
	var text strings.Builder
	appendText(&text, node, visible, 0)
	return text.String()
}

func appendText(out *strings.Builder, n *html.Node, visible bool, depth int) {
	if depth >= maxDOMDepth || out.Len() >= maxPageText || visible && excluded(n) {
		return
	}
	if n.Type == html.TextNode {
		out.WriteString(trimBytes(n.Data, maxPageText-out.Len()))
		return
	}
	breaks := n.Type == html.ElementNode && blockElement(n.Data)
	if breaks {
		out.WriteByte('\n')
	}
	for child := n.FirstChild; child != nil && out.Len() < maxPageText; child = child.NextSibling {
		appendText(out, child, visible, depth+1)
	}
	if breaks && out.Len() < maxPageText {
		out.WriteByte('\n')
	}
}

func blockElement(tag string) bool {
	switch tag {
	case "address", "article", "blockquote", "br", "dd", "details", "div", "dl", "dt", "figcaption", "figure",
		"footer", "h1", "h2", "h3", "h4", "h5", "h6", "header", "li", "main", "ol", "p", "pre", "section",
		"summary", "table", "td", "th", "tr", "ul":
		return true
	}
	return false
}

func excluded(node *html.Node) bool {
	if node.Type != html.ElementNode {
		return false
	}
	switch node.Data {
	case "aside", "button", "canvas", "footer", "form", "iframe", "nav", "noscript", "script", "style", "svg", "template":
		return true
	}
	for _, value := range node.Attr {
		if value.Key == "hidden" || value.Key == "aria-hidden" && strings.EqualFold(value.Val, "true") {
			return true
		}
	}
	return false
}

func attr(node *html.Node, name string) string {
	for _, value := range node.Attr {
		if value.Key == name || value.Namespace+":"+value.Key == name {
			return value.Val
		}
	}
	return ""
}

func underHead(node *html.Node) bool {
	for parent := node.Parent; parent != nil; parent = parent.Parent {
		if parent.Type == html.ElementNode && parent.Data == "head" {
			return true
		}
	}
	return false
}

func metadataURL(base *url.URL, raw string) string {
	if raw = strings.TrimSpace(raw); raw == "" || strings.HasPrefix(raw, "#") {
		return ""
	}
	u, err := base.Parse(raw)
	if err != nil {
		return ""
	}
	u.RawFragment, u.Fragment = "", ""
	value, err := publicURL(u.String())
	if err != nil {
		return ""
	}
	return value.String()
}

func languageTag(raw string) string {
	tag, err := language.Parse(strings.TrimSpace(raw))
	if err != nil || tag == language.Und {
		return ""
	}
	return tag.String()
}

func cleanLabel(raw string, maxBytes int) string {
	raw = strings.ToValidUTF8(raw, "")
	raw = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && !unicode.IsSpace(r) {
			return -1
		}
		return r
	}, raw)
	return strings.TrimSpace(trimBytes(strings.Join(strings.Fields(raw), " "), maxBytes))
}

func paragraphText(raw string) string {
	var parts []string
	for _, line := range strings.Split(raw, "\n") {
		if clean := cleanLabel(line, maxPageText); clean != "" {
			parts = append(parts, clean)
		}
	}
	return trimBytes(strings.Join(parts, "\n"), maxPageText)
}

func trimBytes(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	for limit > 0 && !utf8.RuneStart(s[limit]) {
		limit--
	}
	return s[:limit]
}

func hasWord(list, word string) bool {
	for _, item := range strings.Fields(list) {
		if strings.EqualFold(item, word) {
			return true
		}
	}
	return false
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func firstTime(first, second *time.Time) *time.Time {
	if first != nil {
		return first
	}
	return second
}

type schemaState struct {
	scanned   int
	published *time.Time
	modified  *time.Time
	questions []string
}

func (s *schemaState) walk(value any, depth int, types map[string]bool) {
	if depth > 32 || s.scanned >= 1024 {
		return
	}
	s.scanned++
	switch v := value.(type) {
	case []any:
		for _, child := range v {
			s.walk(child, depth+1, types)
		}
	case map[string]any:
		for _, label := range schemaLabels(v["@type"]) {
			if len(types) < 64 {
				types[label] = true
			}
			if label == "Question" && len(s.questions) < 50 {
				if name, ok := v["name"].(string); ok {
					s.questions = append(s.questions, cleanLabel(name, maxTitleChars))
				}
			}
			switch label {
			case "Article", "BlogPosting", "NewsArticle", "Report", "ScholarlyArticle", "TechArticle", "WebPage":
				published, _ := v["datePublished"].(string)
				modified, _ := v["dateModified"].(string)
				s.published = firstTime(s.published, parseTimestamp(published))
				s.modified = firstTime(s.modified, parseTimestamp(modified))
			}
		}
		// Traverse map values deterministically so repeated extraction has
		// stable dates and content fingerprints when several objects exist.
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			s.walk(v[key], depth+1, types)
		}
	}
}

func schemaLabels(raw any) []string {
	if value, ok := raw.(string); ok {
		if label := schemaType(value); label != "" {
			return []string{label}
		}
		return nil
	}
	var labels []string
	if values, ok := raw.([]any); ok {
		for _, item := range values {
			if label, ok := item.(string); ok {
				if name := schemaType(label); name != "" {
					labels = append(labels, name)
				}
			}
			if len(labels) >= 64 {
				break
			}
		}
	}
	return labels
}

func schemaType(raw string) string {
	value := strings.TrimPrefix(strings.TrimSpace(raw), "schema:")
	if strings.Contains(value, "/") {
		u, err := url.Parse(value)
		if err != nil || !strings.EqualFold(u.Hostname(), "schema.org") || (u.Scheme != "https" && u.Scheme != "http") {
			return ""
		}
		value = strings.Trim(u.Path, "/")
	}
	if value == "" || len(value) > 100 || strings.IndexFunc(value, func(r rune) bool {
		return !(r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_')
	}) >= 0 {
		return ""
	}
	return value
}
