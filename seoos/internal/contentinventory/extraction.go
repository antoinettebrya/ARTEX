package contentinventory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"

	"github.com/Autumn-27/artex/seoos/internal/domain"
)

func (c *Crawler) Extract(ctx context.Context, site domain.Site, policy domain.CrawlPolicy, target string, prior *domain.SitePage) (domain.SitePage, error) {
	authorized, err := ValidatePageURL(site, policy, target)
	if err != nil {
		return domain.SitePage{}, err
	}
	u, _ := url.Parse(authorized)
	headers := http.Header{"Accept": {"text/html, application/xhtml+xml, text/plain;q=0.5"}}
	if site.LanguageTag != "" {
		headers.Set("Accept-Language", site.LanguageTag)
	}
	hasPrior := prior != nil && prior.URL == u.String() && prior.State == "fetched"
	if hasPrior {
		if usableHeader(prior.ETag) {
			headers.Set("If-None-Match", prior.ETag)
		}
		if _, err := http.ParseTime(prior.HTTPModified); err == nil {
			headers.Set("If-Modified-Since", prior.HTTPModified)
		}
	}
	res, err := c.get(ctx, u, headers)
	if err != nil {
		return domain.SitePage{}, err
	}
	defer res.Body.Close()

	now := time.Now().UTC()
	if res.StatusCode == http.StatusNotModified {
		if !hasPrior {
			return domain.SitePage{}, &domain.DataError{Code: "inventory_unexpected_not_modified"}
		}
		page := *prior
		page.CheckedAt, page.Unchanged = &now, true
		setValidators(&page, res.Header)
		return page, nil
	}

	page := observedPage(site.ID, u, res, now)
	if res.StatusCode != http.StatusOK {
		if res.StatusCode >= 300 && res.StatusCode < 400 {
			page.State = "redirected"
		}
		page.Fingerprint = fingerprint(page)
		if transientStatus(res.StatusCode) {
			return page, responseError(res)
		}
		return page, nil
	}
	payload, err := readBounded(res.Body, maxPageBytes)
	if err != nil {
		return page, err
	}
	metadata, _, err := extractDocument(payload, res.Header.Get("Content-Type"), u)
	if err != nil {
		return page, err
	}
	if metadata.State == "unsupported" {
		page.State = metadata.State
		page.Fingerprint = fingerprint(page)
		return page, nil
	}
	metadata.SiteID, metadata.HTTPStatus, metadata.IndexStatus, metadata.CheckedAt = page.SiteID, page.HTTPStatus, page.IndexStatus, page.CheckedAt
	metadata.ETag, metadata.HTTPModified = page.ETag, page.HTTPModified
	if metadata.LastModified == nil {
		metadata.LastModified = page.LastModified
	}
	if metadata.Language == "" {
		metadata.Language = languageTag(res.Header.Get("Content-Language"))
	}
	metadata.Robots = robotsValue(metadata.Robots, res.Header.Values("X-Robots-Tag"))
	metadata.Fingerprint = fingerprint(metadata)
	metadata.Unchanged = hasPrior && prior.Fingerprint == metadata.Fingerprint
	return metadata, nil
}

func observedPage(siteID string, u *url.URL, res *http.Response, now time.Time) domain.SitePage {
	page := domain.SitePage{
		SiteID: siteID, HTTPStatus: res.StatusCode, State: "unavailable",
		IndexStatus: "unverified", CheckedAt: &now, PageEvidence: domain.PageEvidence{URL: u.String(), Source: "site_http"},
		Robots: robotsValue("", res.Header.Values("X-Robots-Tag")),
	}
	setValidators(&page, res.Header)
	if modified, err := http.ParseTime(page.HTTPModified); err == nil {
		modified = modified.UTC()
		page.LastModified = &modified
	}
	return page
}

func setValidators(page *domain.SitePage, headers http.Header) {
	if tag := headers.Get("ETag"); usableHeader(tag) {
		page.ETag = tag
	}
	if raw := headers.Get("Last-Modified"); raw != "" {
		if _, err := http.ParseTime(raw); err == nil {
			page.HTTPModified = raw
		}
	}
}

func usableHeader(raw string) bool {
	return raw != "" && len(raw) <= 1024 && strings.IndexFunc(raw, unicode.IsControl) < 0
}

func robotsValue(body string, headers []string) string {
	values := append([]string{body}, headers...)
	var kept []string
	for _, value := range values {
		if value = cleanLabel(value, 300); value != "" {
			kept = append(kept, value)
		}
	}
	return cleanLabel(strings.Join(kept, ", "), 900)
}

func fingerprint(page domain.SitePage) string {
	// Stable fields only. Changes to a check time, cache validator or sitemap
	// hint alone do not create a new content version.
	page.ID, page.SiteID, page.SnapshotID = "", "", ""
	page.IndexStatus, page.Fingerprint, page.ETag, page.HTTPModified = "", "", "", ""
	page.CheckedAt, page.DiscoveredAt, page.LastModified = nil, nil, nil
	page.Mapped, page.Unchanged = false, false
	payload, _ := json.Marshal(page)
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

// Research ingests a document only from an approved first-party origin. HTTP
// response contents supply the captured title, evidence text and dating.
func (c *Crawler) Research(ctx context.Context, policy domain.CrawlPolicy, source domain.ContentSource) (domain.SourceDocument, error) {
	authorized, err := ValidateSource(policy, source)
	if err != nil {
		return domain.SourceDocument{}, err
	}
	u, _ := url.Parse(authorized.URL)
	res, err := c.get(ctx, u, http.Header{"Accept": {"text/html, application/xhtml+xml, text/plain, text/markdown"}})
	if err != nil {
		return domain.SourceDocument{}, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return domain.SourceDocument{}, responseError(res)
	}
	payload, err := readBounded(res.Body, maxPageBytes)
	if err != nil {
		return domain.SourceDocument{}, err
	}
	page, published, err := extractDocument(payload, res.Header.Get("Content-Type"), u)
	if err != nil {
		return domain.SourceDocument{}, err
	}
	if page.State == "unsupported" {
		return domain.SourceDocument{}, &domain.DataError{Code: "inventory_unsupported_source"}
	}
	if page.Body == "" {
		return domain.SourceDocument{}, &domain.DataError{Code: "inventory_empty_source"}
	}
	now := time.Now().UTC()
	authorized.Title, authorized.PublishedAt, authorized.CollectedAt = page.Title, published, &now
	sum := sha256.Sum256([]byte(page.Title + "\x00" + page.Body))
	authorized.Fingerprint, authorized.Status = hex.EncodeToString(sum[:]), "fetched"
	return domain.SourceDocument{ContentSource: authorized, Text: page.Body}, nil
}
