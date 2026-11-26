package contentinventory

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/Autumn-27/artex/seoos/internal/domain"
	"golang.org/x/net/html"
)

const (
	sitemapFileLimit = 24
	jsonPageLimit    = 20
)

// Discover reads the configured sitemap or CMS. It records link evidence only.
func (c *Crawler) Discover(ctx context.Context, site domain.Site, policy domain.CrawlPolicy) (domain.InventoryBatch, error) {
	if !policy.Enabled || policy.SiteID != site.ID {
		return domain.InventoryBatch{}, domain.ErrCrawlUnauthorized
	}
	policy, err := ValidatePolicy(site, policy)
	if err != nil {
		return domain.InventoryBatch{}, err
	}
	base, _ := publicURL(site.BaseURL)
	set := inventorySet{
		base: base, limit: policy.MaxPages, byURL: make(map[string]int),
		batch: domain.InventoryBatch{Entries: make([]domain.InventoryEntry, 0)},
	}
	if policy.Connector == "sitemap" || policy.SitemapURL != "" {
		if err := c.discoverSitemaps(ctx, &set, site.CMS, policy.SitemapURL); err != nil {
			var remote *domain.DataError
			if site.CMS != "generic" || policy.Connector != "sitemap" || policy.SitemapURL != "" ||
				!errors.As(err, &remote) || remote.Code != "inventory_sitemap_not_found" {
				return domain.InventoryBatch{}, err
			}
			if err := c.discoverLandingLinks(ctx, &set); err != nil {
				return domain.InventoryBatch{}, err
			}
		}
	}
	switch policy.Connector {
	case "wordpress":
		err = c.discoverWordPress(ctx, &set, policy.ConnectorURL)
	case "json":
		err = c.discoverJSON(ctx, &set, policy.ConnectorURL)
	}
	if err != nil {
		return domain.InventoryBatch{}, err
	}
	return set.batch, nil
}

// A registered generic site without a sitemap still exposes its landing page
// and links from that page. Those outbound references are discovery evidence,
// and do not authorize further traversal.
func (c *Crawler) discoverLandingLinks(ctx context.Context, set *inventorySet) error {
	base := set.base
	res, err := c.get(ctx, base, http.Header{"Accept": {"text/html, application/xhtml+xml"}})
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return responseError(res)
	}
	payload, err := readBounded(res.Body, maxPageBytes)
	if err != nil {
		return err
	}
	contentType := res.Header.Get("Content-Type")
	mediaType := pageMediaType(payload, contentType)
	if mediaType != "text/html" && mediaType != "application/xhtml+xml" {
		return &domain.DataError{Code: "inventory_unsupported_landing_page"}
	}
	document, err := parseHTML(payload, contentType)
	if err != nil {
		return err
	}
	set.batch.Truncated = true
	set.add(domain.InventoryEntry{URL: base.String(), Kind: "page"})
	walkElements(document, 0, func(node *html.Node) {
		if len(set.batch.Entries) < set.limit && node.Data == "a" && !hasWord(attr(node, "rel"), "nofollow") {
			set.add(domain.InventoryEntry{URL: metadataURL(base, attr(node, "href")), Kind: "page"})
		}
	})
	return nil
}

type inventorySet struct {
	base  *url.URL
	limit int
	byURL map[string]int
	batch domain.InventoryBatch
}

// add filters untrusted discovery results before storing or fetching them.
// Duplicate evidence can add a taxonomy or modification date to a known page.
func (s *inventorySet) add(entry domain.InventoryEntry) bool {
	u, err := scopedURL(s.base, entry.URL)
	if err != nil {
		return true
	}
	entry.URL = u.String()
	if existing, ok := s.byURL[entry.URL]; ok {
		old := &s.batch.Entries[existing]
		if old.Kind == "page" && entry.Kind != "page" {
			old.Kind = entry.Kind
		}
		if entry.ModifiedAt != nil && (old.ModifiedAt == nil || entry.ModifiedAt.After(*old.ModifiedAt)) {
			old.ModifiedAt = entry.ModifiedAt
		}
		old.Categories = labelList(append(old.Categories, entry.Categories...))
		old.Tags = labelList(append(old.Tags, entry.Tags...))
		return true
	}
	if len(s.batch.Entries) >= s.limit {
		s.batch.Truncated = true
		return false
	}
	entry.Categories = labelList(entry.Categories)
	entry.Tags = labelList(entry.Tags)
	s.byURL[entry.URL] = len(s.batch.Entries)
	s.batch.Entries = append(s.batch.Entries, entry)
	return true
}

func labelList(input []string) []string {
	var items []string
	seen := make(map[string]bool)
	for _, value := range input {
		value = cleanLabel(value, 120)
		if value != "" && !seen[value] {
			items = append(items, value)
			seen[value] = true
		}
		if len(items) >= 50 {
			break
		}
	}
	return items
}

func siteEndpoint(base *url.URL, suffix string) *url.URL {
	u := *base
	u.Path = path.Join("/", u.Path, suffix)
	u.RawPath = ""
	u.RawQuery = ""
	return &u
}

type sitemapRecord struct {
	Location string `xml:"loc"`
	Modified string `xml:"lastmod"`
}

type sitemapData struct {
	XMLName xml.Name
	URLs    []sitemapRecord `xml:"url"`
	Maps    []sitemapRecord `xml:"sitemap"`
}

type queuedSitemap struct {
	url *url.URL
	doc *sitemapData
}

func (c *Crawler) discoverSitemaps(ctx context.Context, set *inventorySet, cms, configured string) error {
	seeds, err := c.initialSitemaps(ctx, set.base, cms, configured)
	if err != nil {
		return err
	}
	seen := make(map[string]bool)
	visited := 0
	for len(seeds) != 0 {
		item := seeds[0]
		seeds = seeds[1:]
		if seen[item.url.String()] {
			continue
		}
		seen[item.url.String()] = true
		if visited == sitemapFileLimit {
			set.batch.Truncated = true
			break
		}
		visited++
		doc := item.doc
		if doc == nil {
			doc, err = c.readSitemap(ctx, item.url)
			if err != nil {
				return err
			}
		}
		if doc.XMLName.Local == "sitemapindex" {
			for _, child := range doc.Maps {
				if u, err := scopedURL(set.base, child.Location); err == nil && !seen[u.String()] {
					if len(seeds) >= sitemapFileLimit {
						set.batch.Truncated = true
						break
					}
					seeds = append(seeds, queuedSitemap{url: u})
				}
			}
			continue
		}
		for _, record := range doc.URLs {
			if !set.add(domain.InventoryEntry{
				URL: record.Location, Kind: "page", ModifiedAt: parseTimestamp(record.Modified),
			}) {
				return nil
			}
		}
	}
	return nil
}

func (c *Crawler) initialSitemaps(ctx context.Context, base *url.URL, cms, configured string) ([]queuedSitemap, error) {
	if configured != "" {
		u, err := scopedURL(base, configured)
		if err != nil {
			return nil, err
		}
		return []queuedSitemap{{url: u}}, nil
	}
	names := []string{"sitemap.xml"}
	if cms == "wordpress" {
		names = append(names, "wp-sitemap.xml")
	}
	for _, name := range names {
		u := siteEndpoint(base, name)
		doc, err := c.readSitemap(ctx, u)
		if err == nil {
			return []queuedSitemap{{url: u, doc: doc}}, nil
		}
		var failure *domain.DataError
		if !errors.As(err, &failure) || failure.Code != "inventory_http_404" && failure.Code != "inventory_http_410" {
			return nil, err
		}
	}
	// A robots file can publish a nonstandard sitemap URL. The destination still
	// has to be within the exact site scope before any request is sent.
	res, err := c.get(ctx, siteEndpoint(base, "robots.txt"), nil)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		if transientStatus(res.StatusCode) {
			return nil, responseError(res)
		}
		return nil, &domain.DataError{Code: "inventory_sitemap_not_found"}
	}
	data, err := readBounded(res.Body, 128<<10)
	if err != nil {
		return nil, err
	}
	sitemaps := make([]queuedSitemap, 0)
	for _, line := range strings.Split(string(data), "\n") {
		field, value, ok := strings.Cut(line, ":")
		if !ok || !strings.EqualFold(strings.TrimSpace(field), "sitemap") {
			continue
		}
		value, _, _ = strings.Cut(value, "#")
		if u, err := scopedURL(base, strings.TrimSpace(value)); err == nil {
			sitemaps = append(sitemaps, queuedSitemap{url: u})
			if len(sitemaps) > sitemapFileLimit {
				break
			}
		}
	}
	if len(sitemaps) == 0 {
		return nil, &domain.DataError{Code: "inventory_sitemap_not_found"}
	}
	return sitemaps, nil
}

func (c *Crawler) readSitemap(ctx context.Context, target *url.URL) (*sitemapData, error) {
	res, err := c.get(ctx, target, http.Header{"Accept": {"application/xml, text/xml, application/gzip"}})
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, responseError(res)
	}
	payload, err := readBounded(res.Body, maxListingBytes)
	if err != nil {
		return nil, err
	}
	if bytes.HasPrefix(payload, []byte{0x1f, 0x8b}) {
		zip, err := gzip.NewReader(bytes.NewReader(payload))
		if err != nil {
			return nil, &domain.DataError{Code: "inventory_invalid_sitemap"}
		}
		defer zip.Close()
		payload, err = readBounded(zip, maxListingBytes)
		if err != nil {
			return nil, err
		}
	}
	var doc sitemapData
	decoder := xml.NewDecoder(bytes.NewReader(payload))
	if err := decoder.Decode(&doc); err != nil || (doc.XMLName.Local != "urlset" && doc.XMLName.Local != "sitemapindex") {
		return nil, &domain.DataError{Code: "inventory_invalid_sitemap"}
	}
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		switch value := token.(type) {
		case xml.CharData:
			if err == nil && strings.TrimSpace(string(value)) == "" {
				continue
			}
		case xml.Comment:
			if err == nil {
				continue
			}
		}
		return nil, &domain.DataError{Code: "inventory_invalid_sitemap"}
	}
	return &doc, nil
}

type jsonListing struct {
	Items *[]domain.InventoryEntry `json:"items"`
	Next  string                   `json:"next"`
}

func (c *Crawler) discoverJSON(ctx context.Context, set *inventorySet, configured string) error {
	target, err := scopedURL(set.base, configured)
	if err != nil {
		return err
	}
	seen := make(map[string]bool)
	for i := 0; i < jsonPageLimit; i++ {
		if seen[target.String()] {
			return &domain.DataError{Code: "inventory_pagination_loop"}
		}
		seen[target.String()] = true
		var listing jsonListing
		if _, err := c.readJSON(ctx, target, &listing); err != nil {
			return err
		}
		if listing.Items == nil {
			return &domain.DataError{Code: "inventory_invalid_cms_json"}
		}
		for _, entry := range *listing.Items {
			switch entry.Kind {
			case "post", "article", "category", "tag":
			default:
				entry.Kind = "page"
			}
			if !set.add(entry) {
				return nil
			}
		}
		if listing.Next == "" {
			return nil
		}
		next, err := target.Parse(listing.Next)
		if err != nil {
			return &domain.DataError{Code: "inventory_invalid_pagination"}
		}
		target, err = scopedURL(set.base, next.String())
		if err != nil {
			return err
		}
	}
	set.batch.Truncated = true
	return nil
}

func (c *Crawler) readJSON(ctx context.Context, u *url.URL, into any) (http.Header, error) {
	res, err := c.get(ctx, u, http.Header{"Accept": {"application/json"}})
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, responseError(res)
	}
	body, err := readBounded(res.Body, maxListingBytes)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(body, into); err != nil {
		return nil, &domain.DataError{Code: "inventory_invalid_cms_json"}
	}
	return res.Header, nil
}

func parseTimestamp(raw string) *time.Time {
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05", time.DateOnly} {
		if t, err := time.Parse(layout, strings.TrimSpace(raw)); err == nil {
			t = t.UTC()
			return &t
		}
	}
	return nil
}
