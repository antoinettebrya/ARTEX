package executioncms

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/Autumn-27/artex/seoos/internal/domain"
	"golang.org/x/net/html"
)

// ObservePublic checks the approved public page without sending a CMS
// credential. HTTP status codes describe observations, not index coverage.
func (c *client) ObservePublic(ctx context.Context, raw string) (domain.CMSPage, error) {
	u, err := c.approved(raw)
	if err != nil {
		return domain.CMSPage{}, err
	}
	result, err := c.request(ctx, http.MethodGet, u, false, nil, http.Header{"Accept": {"text/html,application/xhtml+xml"}})
	page := domain.CMSPage{URL: u.String(), HTTPStatus: result.status}
	if err != nil {
		if result.status >= 300 && result.status < 500 && result.status != 408 && result.status != 425 && result.status != 429 {
			return page, nil
		}
		return page, err
	}
	if !strings.Contains(strings.ToLower(result.header.Get("Content-Type")), "html") {
		return page, &domain.DataError{Code: "cms_public_not_html"}
	}
	document, err := html.Parse(bytes.NewReader(result.body))
	if err != nil {
		return page, &domain.DataError{Code: "cms_public_invalid_html"}
	}
	if head := firstElement(document, "head"); head != nil {
		readHead(&page, u, head)
	}
	body := firstElement(document, "article")
	if body == nil {
		body = firstElement(document, "main")
	}
	if body == nil {
		body = firstElement(document, "body")
	}
	if body != nil {
		if heading := firstElement(body, "h1"); heading != nil {
			page.Title = visibleText(heading)
		}
		page.Body = visibleText(body)
	}
	if page.Title == "" {
		page.Title = page.SEOTitle
	}
	return page, nil
}

func readHead(page *domain.CMSPage, base *url.URL, head *html.Node) {
	for node := head.FirstChild; node != nil; node = node.NextSibling {
		switch node.Data {
		case "title":
			if page.SEOTitle == "" {
				page.SEOTitle = visibleText(node)
			}
		case "meta":
			if strings.EqualFold(nodeAttribute(node, "name"), "description") && page.Description == "" {
				page.Description = strings.TrimSpace(nodeAttribute(node, "content"))
			}
		case "link":
			if hasRel(nodeAttribute(node, "rel"), "canonical") && page.Canonical == "" {
				if ref, err := url.Parse(nodeAttribute(node, "href")); err == nil && ref.String() != "" {
					resolved := base.ResolveReference(ref)
					if _, err := domain.EvidenceURL(resolved.String()); err == nil {
						page.Canonical = resolved.String()
					}
				}
			}
		}
	}
}

func hasRel(value, rel string) bool {
	for _, item := range strings.Fields(value) {
		if strings.EqualFold(item, rel) {
			return true
		}
	}
	return false
}

func nodeAttribute(node *html.Node, key string) string {
	for _, attr := range node.Attr {
		if attr.Key == key {
			return attr.Val
		}
	}
	return ""
}

func firstElement(root *html.Node, tag string) *html.Node {
	if root.Type == html.ElementNode && root.Data == tag {
		return root
	}
	for child := root.FirstChild; child != nil; child = child.NextSibling {
		if match := firstElement(child, tag); match != nil {
			return match
		}
	}
	return nil
}

func visibleText(root *html.Node) string {
	var text strings.Builder
	writeText(&text, root)
	return strings.Join(strings.Fields(text.String()), " ")
}

func writeText(dest io.StringWriter, node *html.Node) {
	switch node.Type {
	case html.TextNode:
		_, _ = dest.WriteString(node.Data)
		return
	case html.ElementNode:
		switch node.Data {
		case "script", "style", "template", "svg", "noscript", "nav", "aside", "footer":
			return
		}
	}
	block := false
	if node.Type == html.ElementNode {
		switch node.Data {
		case "article", "main", "div", "section", "p", "h1", "h2", "h3", "h4", "h5", "h6", "ul", "ol", "li", "blockquote", "br", "tr":
			block = true
		}
	}
	if block {
		_, _ = dest.WriteString(" ")
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		writeText(dest, child)
	}
	if block {
		_, _ = dest.WriteString(" ")
	}
}
