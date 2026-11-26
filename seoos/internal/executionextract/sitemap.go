package executionextract

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Autumn-27/artex/seoos/internal/contentinventory"
	"github.com/Autumn-27/artex/seoos/internal/domain"
	"golang.org/x/text/language"
)

const (
	sitemapBytes      = 8 << 20
	sitemapDocuments  = 20
	maxSitemapEntries = 10000
	sitemapPartSize   = 1000
	sitemapNS         = "http://www.sitemaps.org/schemas/sitemap/0.9"
)

type sitemapSource struct {
	XMLName xml.Name
	Entries []struct {
		Location string `xml:"loc"`
		Modified string `xml:"lastmod"`
		Related  []struct {
			Rel      string `xml:"rel,attr"`
			Language string `xml:"hreflang,attr"`
			URL      string `xml:"href,attr"`
		} `xml:"link"`
	} `xml:"url"`
	Children []struct {
		Location string `xml:"loc"`
	} `xml:"sitemap"`
}

func Sitemap(ctx context.Context, client *http.Client, site domain.Site, target, initial string) (domain.SitemapDocument, error) {
	policy := domain.CrawlPolicy{Enabled: true, SiteID: site.ID, Connector: "sitemap"}
	var discovered []string
	if strings.TrimSpace(target) == "" {
		target = strings.TrimSuffix(site.BaseURL, "/") + "/sitemap.xml"
		if initial == "" {
			var err error
			discovered, err = sitemapsFromRobots(ctx, client, site, policy)
			if err != nil {
				return domain.SitemapDocument{}, err
			}
			if len(discovered) > 0 {
				target, discovered = discovered[0], discovered[1:]
			}
		}
	}
	target, err := contentinventory.ValidatePageURL(site, policy, target)
	if err != nil {
		return domain.SitemapDocument{}, err
	}
	out := domain.SitemapDocument{URL: target, Entries: []domain.SitemapEntry{}, Children: append([]string(nil), discovered...)}
	queue := append([]string{target}, discovered...)
	seen, urls := map[string]bool{}, map[string]bool{}
	for len(queue) > 0 && len(seen) < sitemapDocuments && len(out.Entries) < maxSitemapEntries {
		current := queue[0]
		queue = queue[1:]
		if seen[current] {
			continue
		}
		seen[current] = true
		var raw []byte
		if current == target && initial != "" {
			raw = []byte(initial)
		} else {
			raw, err = fetchSitemapBytes(ctx, client, current, sitemapBytes)
			if err != nil {
				if current == target {
					return out, err
				}
				var problem *domain.DataError
				if errors.As(err, &problem) && problem.Retry {
					return out, err
				}
				out.Truncated = true
				continue
			}
		}
		if len(raw) > sitemapBytes {
			return out, &domain.DataError{Code: "sitemap_too_large"}
		}
		var source sitemapSource
		if err := xml.Unmarshal(raw, &source); err != nil {
			return out, &domain.DataError{Code: "sitemap_invalid_xml"}
		}
		switch source.XMLName.Local {
		case "sitemapindex":
			for _, child := range source.Children {
				u, err := contentinventory.ValidatePageURL(site, policy, strings.TrimSpace(child.Location))
				if err != nil || seen[u] {
					continue
				}
				if len(queue) >= sitemapDocuments*2 {
					out.Truncated = true
					break
				}
				queue = append(queue, u)
				out.Children = append(out.Children, u)
			}
		case "urlset":
			for _, entry := range source.Entries {
				u, err := contentinventory.ValidatePageURL(site, policy, strings.TrimSpace(entry.Location))
				if err != nil || urls[u] {
					continue
				}
				if len(out.Entries) == maxSitemapEntries {
					out.Truncated = true
					break
				}
				row := domain.SitemapEntry{URL: u, LastMod: sitemapLastMod(entry.Modified)}
				for _, alt := range entry.Related {
					if alt.Rel != "alternate" || !validHreflang(alt.Language) || len(row.Alternate) >= 40 {
						continue
					}
					ref, err := domain.EvidenceURL(alt.URL)
					if err != nil || ref.Scheme != "https" {
						continue
					}
					row.Alternate = append(row.Alternate, domain.SitemapAlternate{Language: alt.Language, URL: ref.String()})
				}
				urls[u] = true
				out.Entries = append(out.Entries, row)
			}
		default:
			return out, &domain.DataError{Code: "sitemap_unsupported_root"}
		}
	}
	if len(queue) > 0 {
		out.Truncated = true
	}
	out.Files, err = GenerateSitemaps(site.BaseURL, out.Entries, sitemapPartSize)
	return out, err
}

func validHreflang(value string) bool {
	if value == "x-default" {
		return true
	}
	if value == "" || len(value) > 30 {
		return false
	}
	_, err := language.Parse(value)
	return err == nil
}
func sitemapLastMod(raw string) string {
	raw = strings.TrimSpace(raw)
	for _, format := range []string{time.DateOnly, time.RFC3339} {
		if _, err := time.Parse(format, raw); err == nil {
			return raw
		}
	}
	return ""
}

func sitemapsFromRobots(ctx context.Context, client *http.Client, site domain.Site, policy domain.CrawlPolicy) ([]string, error) {
	base, err := domain.EvidenceURL(site.BaseURL)
	if err != nil {
		return nil, err
	}
	base.Path, base.RawPath, base.RawQuery = "/robots.txt", "", ""
	raw, err := fetchSitemapBytes(ctx, client, base.String(), 64<<10)
	var problem *domain.DataError
	if err != nil {
		if errors.As(err, &problem) && problem.Retry {
			return nil, err
		}
		return nil, nil
	}
	var targets []string
	seen := map[string]bool{}
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	for len(targets) < sitemapDocuments && scanner.Scan() {
		k, v, ok := strings.Cut(scanner.Text(), ":")
		if ok && strings.EqualFold(strings.TrimSpace(k), "sitemap") {
			target, err := contentinventory.ValidatePageURL(site, policy, strings.TrimSpace(v))
			if err == nil && !seen[target] {
				targets = append(targets, target)
				seen[target] = true
			}
		}
	}
	return targets, nil
}

func fetchSitemapBytes(ctx context.Context, client *http.Client, target string, limit int) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	res, err := client.Do(req)
	if err != nil {
		var deferred *domain.DataError
		if errors.As(err, &deferred) {
			return nil, deferred
		}
		return nil, &domain.DataError{Code: "sitemap_transport", Retry: true}
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, &domain.DataError{Code: "sitemap_http_rejected", Retry: res.StatusCode >= 500 || res.StatusCode == 429}
	}
	reader := bufio.NewReader(res.Body)
	var from io.Reader = reader
	if header, _ := reader.Peek(2); len(header) == 2 && header[0] == 0x1f && header[1] == 0x8b {
		z, err := gzip.NewReader(reader)
		if err != nil {
			return nil, &domain.DataError{Code: "sitemap_invalid_gzip"}
		}
		defer z.Close()
		from = z
	}
	body, err := io.ReadAll(io.LimitReader(from, int64(limit+1)))
	if err != nil {
		return nil, &domain.DataError{Code: "sitemap_read_failed", Retry: true}
	}
	if len(body) > limit {
		return nil, &domain.DataError{Code: "sitemap_too_large"}
	}
	return body, nil
}

type exportSitemap struct {
	XMLName   xml.Name    `xml:"urlset"`
	Namespace string      `xml:"xmlns,attr"`
	XHTML     string      `xml:"xmlns:xhtml,attr"`
	URLs      []exportURL `xml:"url"`
}
type exportURL struct {
	Location   string      `xml:"loc"`
	Modified   string      `xml:"lastmod,omitempty"`
	Alternates []exportAlt `xml:"xhtml:link,omitempty"`
}
type exportAlt struct {
	Rel      string `xml:"rel,attr"`
	Language string `xml:"hreflang,attr"`
	URL      string `xml:"href,attr"`
}
type exportIndex struct {
	XMLName   xml.Name            `xml:"sitemapindex"`
	Namespace string              `xml:"xmlns,attr"`
	Locations []exportSitemapLink `xml:"sitemap"`
}

type exportSitemapLink struct {
	Location string `xml:"loc"`
}

func GenerateSitemaps(base string, entries []domain.SitemapEntry, limit int) (map[string]string, error) {
	if _, err := url.ParseRequestURI(base); err != nil {
		return nil, err
	}
	if limit < 1 || limit > 50000 {
		return nil, &domain.DataError{Code: "sitemap_part_limit"}
	}
	files := map[string]string{}
	if len(entries) == 0 {
		return files, nil
	}
	index := exportIndex{Namespace: sitemapNS}
	for start := 0; start < len(entries); start += limit {
		out := exportSitemap{Namespace: sitemapNS, XHTML: "http://www.w3.org/1999/xhtml"}
		for _, entry := range entries[start:min(start+limit, len(entries))] {
			row := exportURL{Location: entry.URL, Modified: entry.LastMod}
			for _, alt := range entry.Alternate {
				row.Alternates = append(row.Alternates, exportAlt{Rel: "alternate", Language: alt.Language, URL: alt.URL})
			}
			out.URLs = append(out.URLs, row)
		}
		name := fmt.Sprintf("artex-sitemap-%04d.xml", start/limit+1)
		body, err := xml.MarshalIndent(out, "", "  ")
		if err != nil {
			return nil, err
		}
		if len(body) > 50<<20 {
			return nil, &domain.DataError{Code: "sitemap_part_too_large"}
		}
		files[name] = xml.Header + string(body)
		index.Locations = append(index.Locations, exportSitemapLink{Location: strings.TrimSuffix(base, "/") + "/" + name})
	}
	if len(index.Locations) > 1 {
		body, err := xml.MarshalIndent(index, "", "  ")
		if err != nil {
			return nil, err
		}
		files["artex-sitemap-index.xml"] = xml.Header + string(body)
	}
	return files, nil
}
