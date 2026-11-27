package contentinventory

import (
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Autumn-27/artex/seoos/internal/domain"
)

const fixtureOrigin = "https://phase3.example.com"
const fixturePublicSource = "https://official.example.com"

func testSite(base string) domain.Site {
	return domain.Site{ID: "106dbd76-57ea-4a9f-9d58-6f2e167f2111", BaseURL: base, LanguageTag: "en", CMS: "generic", Status: "active"}
}

func testPolicy(t *testing.T, site domain.Site, policy domain.CrawlPolicy) domain.CrawlPolicy {
	t.Helper()
	policy.Enabled = true
	verified, err := ValidatePolicy(site, policy)
	if err != nil {
		t.Fatal(err)
	}
	return verified
}

type fixtureTransport struct {
	t        *testing.T
	local    *url.URL
	inner    *http.Transport
	requests atomic.Int32
}

func (f *fixtureTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	f.requests.Add(1)
	if req.URL.Host != "phase3.example.com" && req.URL.Host != "official.example.com" {
		f.t.Errorf("unexpected destination %s", req.URL)
		return nil, errors.New("out-of-scope fixture request")
	}
	if req.Method != http.MethodGet || req.Header.Get("User-Agent") != "ARTEXSEOOS/1.0" || req.Header.Get("Authorization") != "" {
		f.t.Error("fixture request did not use the crawler's public GET contract")
	}
	copy := req.Clone(req.Context())
	copy.URL.Scheme = f.local.Scheme
	copy.URL.Host = f.local.Host
	copy.Host = req.URL.Host
	return f.inner.RoundTrip(copy)
}

func httpFixture(t *testing.T, h http.HandlerFunc) (*Crawler, *fixtureTransport) {
	t.Helper()
	server := httptest.NewServer(h)
	t.Cleanup(server.Close)
	local, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	transport := &fixtureTransport{t: t, local: local, inner: &http.Transport{}}
	t.Cleanup(transport.inner.CloseIdleConnections)
	return New(&http.Client{Transport: transport}), transport
}

func TestPublicPolicyValidationAndPinnedDNS(t *testing.T) {
	site := testSite(fixtureOrigin + "/guides/")
	policy, err := ValidatePolicy(site, domain.CrawlPolicy{
		Enabled: true, SitemapURL: fixtureOrigin + "/guides/sitemap.xml",
		SourceOrigins: []string{fixturePublicSource + "/", fixturePublicSource + ":443"},
	})
	if err != nil || policy.SiteID != site.ID || policy.Connector != "sitemap" || policy.MaxPages != 100 ||
		!slices.Equal(policy.SourceOrigins, []string{fixturePublicSource}) {
		t.Fatalf("unexpected normalized policy %#v: %v", policy, err)
	}
	for _, target := range []string{
		"https://phase3.example.com.evil.com/guides/", fixtureOrigin + "/guides-more",
		fixtureOrigin + "/guides/%2e%2e/private", fixtureOrigin + "/guides/%252e%252e%252fprivate",
		fixtureOrigin + "/guides/..;/private", fixtureOrigin + "/guides/%5cx",
		fixtureOrigin + "/guides%2Fx", fixtureOrigin + "/guides/x?password=fake",
		"http://phase3.example.com/guides/x", "https://phase3.example.com:8080/guides/x",
		"https://guest:fake@phase3.example.com/guides/x", fixtureOrigin + "/guides/x#ignored",
	} {
		if _, err := ValidatePageURL(site, policy, target); !errors.Is(err, domain.ErrCrawlUnauthorized) {
			t.Errorf("allowed out-of-scope URL %s: %v", target, err)
		}
	}
	if _, err := ValidatePageURL(site, policy, fixtureOrigin+"/guides/%E6%99%82%E9%96%93?lang=ja"); err != nil {
		t.Fatalf("valid in-scope URL rejected: %v", err)
	}
	if _, err := ValidatePageURL(site, domain.CrawlPolicy{Enabled: true}, fixtureOrigin+"/guides"); !errors.Is(err, domain.ErrCrawlUnauthorized) {
		t.Fatal("site-independent policy authorized a page")
	}
	for _, address := range []string{
		"127.0.0.1", "10.20.30.40", "100.64.1.1", "169.254.169.254", "192.0.2.1",
		"198.18.0.1", "198.51.100.1", "240.1.2.3", "::1", "::ffff:127.0.0.1",
		"fc00::1", "fe80::1", "2001:db8::1", "2002:7f00:1::",
	} {
		if publicAddress(netip.MustParseAddr(address)) {
			t.Errorf("nonpublic DNS address was accepted: %s", address)
		}
	}
	v4, v6 := netip.MustParseAddr("1.1.1.1"), netip.MustParseAddr("2606:4700:4700::1111")
	all, err := publicAddresses(t.Context(), staticLookup{v4, v6}, "phase3.example.com")
	if err != nil || !slices.Equal(all, []netip.Addr{v4, v6}) {
		t.Fatalf("expected both public addresses to stay pinned: %v %v", all, err)
	}
	if _, err := publicAddresses(t.Context(), staticLookup{v4, netip.MustParseAddr("127.0.0.1")}, "phase3.example.com"); !errors.Is(err, domain.ErrCrawlUnauthorized) {
		t.Fatalf("mixed public and private DNS answers not rejected: %v", err)
	}
}

type staticLookup []netip.Addr

func (l staticLookup) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	return l, nil
}

func TestSitemapDiscoveryIndexGzipScopeAndCap(t *testing.T) {
	site := testSite(fixtureOrigin + "/docs")
	policy := testPolicy(t, site, domain.CrawlPolicy{MaxPages: 3})
	c, transport := httpFixture(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/docs/sitemap.xml":
			io.WriteString(w, `<sitemapindex xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
				<sitemap><loc>https://phase3.example.com/docs/sitemap-a.xml</loc></sitemap>
				<sitemap><loc>https://phase3.example.com/docs/sitemap-b.xml.gz</loc></sitemap>
				<sitemap><loc>https://official.example.com/external.xml</loc></sitemap>
				<sitemap><loc>https://phase3.example.com/docs/sitemap.xml</loc></sitemap>
			</sitemapindex>`)
		case "/docs/sitemap-a.xml":
			io.WriteString(w, `<urlset>
				<url><loc>https://phase3.example.com/docs/existing</loc><lastmod>2025-03-01</lastmod></url>
				<url><loc>https://phase3.example.com/docs-other/rejected</loc></url>
				<url><loc>https://phase3.example.com/docs/%2e%2e/rejected</loc></url>
			</urlset>`)
		case "/docs/sitemap-b.xml.gz":
			w.Header().Set("Content-Type", "application/gzip")
			g := gzip.NewWriter(w)
			io.WriteString(g, `<urlset>
				<url><loc>https://phase3.example.com/docs/existing</loc><lastmod>2025-03-02T13:30:00Z</lastmod></url>
				<url><loc>https://phase3.example.com/docs/second</loc></url>
				<url><loc>https://phase3.example.com/docs/third</loc></url>
				<url><loc>https://phase3.example.com/docs/fourth</loc></url>
			</urlset>`)
			g.Close()
		default:
			t.Error("unexpected sitemap request:", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
	got, err := c.Discover(t.Context(), site, policy)
	if err != nil || len(got.Entries) != 3 || !got.Truncated || transport.requests.Load() != 3 {
		t.Fatalf("unexpected sitemap result %#v: %v", got, err)
	}
	if got.Entries[0].URL != fixtureOrigin+"/docs/existing" || got.Entries[0].ModifiedAt.Format(time.RFC3339) != "2025-03-02T13:30:00Z" {
		t.Fatalf("missing or stale discovery metadata: %#v", got.Entries[0])
	}
}

func TestSitemapDiscoveryUsesRobotsDeclaration(t *testing.T) {
	site := testSite(fixtureOrigin)
	policy := testPolicy(t, site, domain.CrawlPolicy{})
	c, requests := httpFixture(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sitemap.xml":
			w.WriteHeader(404)
		case "/robots.txt":
			io.WriteString(w, "User-agent: *\nSitemap: https://official.example.com/ignored.xml\nSitemap: "+fixtureOrigin+"/places.xml # declared\n")
		case "/places.xml":
			io.WriteString(w, `<urlset><url><loc>https://phase3.example.com/existing</loc></url></urlset>`)
		default:
			t.Error("unexpected discovery path:", r.URL.Path)
			w.WriteHeader(404)
		}
	})
	result, err := c.Discover(t.Context(), site, policy)
	if err != nil || len(result.Entries) != 1 || requests.requests.Load() != 3 || result.Entries[0].URL != fixtureOrigin+"/existing" {
		t.Fatalf("sitemap fallback: %#v %v", result, err)
	}
}

func TestGenericLandingDiscoveryWithoutSitemap(t *testing.T) {
	site := testSite(fixtureOrigin)
	policy := testPolicy(t, site, domain.CrawlPolicy{})
	c, requests := httpFixture(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sitemap.xml", "/robots.txt":
			w.WriteHeader(http.StatusNotFound)
		case "/":
			w.Header().Set("Content-Type", "text/html")
			io.WriteString(w, `<html><body><main>
				<a href="/manual#sec">Public manual</a>
				<a rel="nofollow" href="/skip">Excluded</a>
				<a href="https://official.example.com/out">External</a>
				<form action="/submission"></form>
			</main></body></html>`)
		default:
			t.Errorf("followed a landing-page reference directly %s", r.URL.Path)
		}
	})
	result, err := c.Discover(t.Context(), site, policy)
	if err != nil || !result.Truncated || len(result.Entries) != 2 || requests.requests.Load() != 3 ||
		result.Entries[0].URL != fixtureOrigin || result.Entries[1].URL != fixtureOrigin+"/manual" {
		t.Fatalf("landing links result %#v: %v", result, err)
	}
}

func TestWordPressPublicRESTInventory(t *testing.T) {
	site := testSite(fixtureOrigin + "/wp")
	policy := testPolicy(t, site, domain.CrawlPolicy{Connector: "wordpress"})
	c, requests := httpFixture(t, func(w http.ResponseWriter, r *http.Request) {
		collection, pathOK := strings.CutPrefix(r.URL.Path, "/wp/wp-json/wp/v2/")
		if !pathOK {
			t.Error("expected subdirectory WordPress endpoint")
			w.WriteHeader(http.StatusNotFound)
			return
		}
		query := r.URL.Query()
		if query.Get("_fields") == "" || query.Get("per_page") != "100" || (collection == "posts" || collection == "pages") != (query.Get("status") == "publish") {
			t.Error("invalid WordPress pagination or visibility query", r.URL.RawQuery)
		}
		switch collection {
		case "categories":
			io.WriteString(w, `[{"id":4,"name":"Guides &amp; tips","link":"https://phase3.example.com/wp/category/guides"}]`)
		case "tags":
			io.WriteString(w, `[{"id":5,"name":"掃除機","link":"https://phase3.example.com/wp/tag/sweep"}]`)
		case "pages":
			io.WriteString(w, `[{"link":"https://phase3.example.com/wp/contact","modified_gmt":"2025-12-07T16:35:40"}]`)
		case "posts":
			w.Header().Set("X-WP-TotalPages", "2")
			if query.Get("page") == "1" {
				io.WriteString(w, `[{"link":"https://phase3.example.com/wp/one","modified_gmt":"2025-12-07T16:35:40","categories":[4],"tags":[5]}]`)
			} else {
				io.WriteString(w, `[{"link":"https://phase3.example.com/wp/two","categories":[4]},{"link":"https://official.example.com/out"}]`)
			}
		default:
			t.Error("unexpected WordPress collection", collection)
		}
	})
	got, err := c.Discover(t.Context(), site, policy)
	if err != nil || got.Truncated || len(got.Entries) != 5 || requests.requests.Load() != 5 {
		t.Fatalf("WordPress entries %#v: %v", got, err)
	}
	firstPost := got.Entries[1]
	if firstPost.Kind != "post" || firstPost.ModifiedAt == nil || firstPost.ModifiedAt.Format(time.RFC3339) != "2025-12-07T16:35:40Z" ||
		!slices.Equal(firstPost.Categories, []string{"Guides & tips"}) || !slices.Equal(firstPost.Tags, []string{"掃除機"}) {
		t.Fatalf("post taxonomy or modification: %#v", firstPost)
	}
	if got.Entries[0].Kind != "page" || got.Entries[3].Kind != "category" || got.Entries[4].Kind != "tag" {
		t.Fatalf("missing page or taxonomy archives: %#v", got.Entries)
	}
}

func TestGenericCMSReadAndForeignPagination(t *testing.T) {
	site := testSite(fixtureOrigin + "/site")
	policy := testPolicy(t, site, domain.CrawlPolicy{Connector: "json", ConnectorURL: fixtureOrigin + "/site/content"})
	tests := []struct {
		name    string
		next    string
		failure error
	}{
		{"bounded data", "", nil},
		{"foreign cursor", "https://official.example.com/other", domain.ErrCrawlUnauthorized},
		{"cursor loop", "?cursor=2", &domain.DataError{Code: "inventory_pagination_loop"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c, requests := httpFixture(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/site/content" {
					t.Error("incorrect CMS destination")
				}
				switch r.URL.Query().Get("cursor") {
				case "":
					io.WriteString(w, `{"items":[{"url":"https://phase3.example.com/site/one","kind":"article","modified_at":"2025-12-07T16:35:40Z"}],"next":"?cursor=2"}`)
				case "2":
					fmt.Fprintf(w, `{"items":[{"url":"https://phase3.example.com/site/two","tags":["តម្រង"]}],"next":%q}`, test.next)
				default:
					t.Error("unexpected cursor")
				}
			})
			got, err := c.Discover(t.Context(), site, policy)
			if test.failure != nil {
				if err == nil || err.Error() != test.failure.Error() {
					t.Fatalf("pagination failure %#v: %v", got, err)
				}
			} else if err != nil || len(got.Entries) != 2 || got.Entries[0].Kind != "article" ||
				!slices.Equal(got.Entries[1].Tags, []string{"តម្រង"}) {
				t.Fatalf("generic CMS result %#v: %v", got, err)
			}
			if requests.requests.Load() != 2 {
				t.Fatal("unexpected number of requests:", requests.requests.Load())
			}
		})
	}
}

func TestExtractHTMLStateFingerprintAndConditionalGET(t *testing.T) {
	site := testSite(fixtureOrigin)
	p := testPolicy(t, site, domain.CrawlPolicy{})
	var calls atomic.Int32
	c, _ := httpFixture(t, func(w http.ResponseWriter, r *http.Request) {
		step := calls.Add(1)
		if r.URL.Path != "/existing" || r.Header.Get("Accept-Language") != "en" {
			t.Error("unexpected extraction request")
		}
		if step > 1 && (r.Header.Get("If-None-Match") != `"v1"` || r.Header.Get("If-Modified-Since") != "Mon, 01 Dec 2025 08:30:00 GMT") {
			t.Error("not using saved ETag and HTTP modification date for conditional GET")
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("X-Robots-Tag", "nofollow")
		w.Header().Set("ETag", `"v1"`)
		w.Header().Set("Last-Modified", "Mon, 01 Dec 2025 08:30:00 GMT")
		if step == 2 {
			w.WriteHeader(304)
			return
		}
		interval := 90
		if step == 4 {
			interval = 75
		}
		fmt.Fprintf(w, `<!doctype html><html lang="ja-JP"><head><title>現行フィルター</title>
			<meta name="description" content="A practical filter guide."><meta name="robots" content="noindex">
			<link rel="alternate canonical" href="/canonical">
			<script type="application/ld+json">{"@graph":[{"@type":"Article","dateModified":"2025-04-01T12:05:00Z"},
				{"@type":"FAQPage","mainEntity":{"@type":"Question","name":"When should it be replaced?"}}]}</script>
			</head><body><nav>Boilerplate %d</nav><main><h1>Filter guidance</h1>
			<p>Replace the robot vacuum filter every %d days.</p><h6>Care details</h6>
			<p aria-hidden="true">Internal decorative phrase</p><img src="/filter.jpg" alt="フィルター">
			<img src="https://cdn.example.com/image.jpg?token=fake-private"><div itemtype="https://schema.org/Product"></div>
			</main></body></html>`, step, interval)
	})
	target := fixtureOrigin + "/existing"
	first, err := c.Extract(t.Context(), site, p, target, nil)
	if err != nil || first.IndexStatus != "unverified" || first.State != "fetched" || first.HTTPStatus != 200 ||
		first.Title != "現行フィルター" || first.Language != "ja-JP" || first.Description != "A practical filter guide." ||
		first.CanonicalURL != fixtureOrigin+"/canonical" || first.Robots != "noindex, nofollow" ||
		first.LastModified == nil || first.LastModified.Format(time.RFC3339) != "2025-04-01T12:05:00Z" || first.CheckedAt == nil || first.Fingerprint == "" {
		t.Fatalf("incorrect initial snapshot %#v: %v", first, err)
	}
	if !strings.Contains(first.Body, "Replace the robot vacuum filter every 90 days.") ||
		strings.Contains(first.Body, "Boilerplate") || strings.Contains(first.Body, "decorative phrase") ||
		!slices.Equal(first.Schema, []string{"Article", "FAQPage", "Product", "Question"}) ||
		!reflect.DeepEqual(first.Headings, []domain.HeadingEvidence{{Level: 1, Text: "Filter guidance"}, {Level: 6, Text: "Care details"}}) ||
		!slices.Equal(first.Images, []domain.PageImage{{URL: fixtureOrigin + "/filter.jpg", Alt: "フィルター"}}) {
		t.Fatalf("bad structural extraction: %#v", first)
	}
	for i := 0; i < 2; i++ {
		unchanged, err := c.Extract(t.Context(), site, p, target, &first)
		if err != nil || !unchanged.Unchanged || unchanged.Fingerprint != first.Fingerprint ||
			!unchanged.CheckedAt.After(*first.CheckedAt) || unchanged.Body != first.Body || unchanged.HTTPStatus != 200 {
			t.Fatalf("failed incremental check %#v: %v", unchanged, err)
		}
	}
	changed, err := c.Extract(t.Context(), site, p, target, &first)
	if err != nil || changed.Unchanged || changed.Fingerprint == first.Fingerprint || !strings.Contains(changed.Body, "75 days.") {
		t.Fatalf("failed change detection %#v: %v", changed, err)
	}
}

func TestHTTPAndPolicyBoundaries(t *testing.T) {
	site := testSite(fixtureOrigin)
	p := testPolicy(t, site, domain.CrawlPolicy{})
	c, requests := httpFixture(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/gone":
			w.WriteHeader(404)
		case "/redirect":
			w.Header().Set("Location", fixturePublicSource+"/private")
			w.WriteHeader(301)
		case "/file.pdf":
			w.Header().Set("Content-Type", "application/pdf")
			io.WriteString(w, "%PDF-1.7")
		case "/busy":
			w.Header().Set("Retry-After", "7")
			w.WriteHeader(429)
		case "/large":
			w.Header().Set("Content-Type", "text/html")
			io.WriteString(w, strings.Repeat("x", maxPageBytes+1))
		default:
			t.Error("unexpected HTTP request:", r.URL.Path)
		}
	})
	for _, entry := range []struct {
		path   string
		status int
		state  string
	}{
		{"/gone", 404, "unavailable"},
		{"/redirect", 301, "redirected"},
		{"/file.pdf", 200, "unsupported"},
	} {
		page, err := c.Extract(t.Context(), site, p, fixtureOrigin+entry.path, nil)
		if err != nil || page.HTTPStatus != entry.status || page.State != entry.state || page.IndexStatus != "unverified" {
			t.Fatalf("HTTP observation %#v %v", page, err)
		}
	}
	busy, err := c.Extract(t.Context(), site, p, fixtureOrigin+"/busy", nil)
	var failure *domain.DataError
	if busy.HTTPStatus != 429 || !errors.As(err, &failure) || !failure.Retry || failure.RetryAfter != 7*time.Second {
		t.Fatalf("retry hint %#v %v", busy, err)
	}
	if _, err := c.Extract(t.Context(), site, p, fixtureOrigin+"/large", nil); !errors.As(err, &failure) || failure.Code != "inventory_body_limit" {
		t.Fatalf("oversized payload was not rejected: %v", err)
	}
	p.Enabled = false
	if _, err := c.Discover(t.Context(), site, p); !errors.Is(err, domain.ErrCrawlUnauthorized) {
		t.Fatalf("disabled crawl policy discovery %v", err)
	}
	if _, err := c.Extract(t.Context(), site, p, fixtureOrigin+"/gone", nil); !errors.Is(err, domain.ErrCrawlUnauthorized) {
		t.Fatalf("disabled crawl policy extraction %v", err)
	}
	if requests.requests.Load() != 5 {
		t.Fatalf("an unauthorized or redirect request was sent: %d", requests.requests.Load())
	}
}

func TestApprovedPublicResearchSource(t *testing.T) {
	site := testSite(fixtureOrigin)
	p := testPolicy(t, site, domain.CrawlPolicy{SourceOrigins: []string{fixturePublicSource}})
	c, requests := httpFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Host != "official.example.com" || r.URL.Path != "/standards/filter" {
			t.Error("unexpected source request", r.Host, r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/html")
		io.WriteString(w, `<html lang="th"><head><title>วิธีดูแลตัวกรอง</title>
			<meta property="article:published_time" content="2026-01-03T14:15:00+07:00"></head>
			<body><main><p>Replace the robot vacuum filter every 90 days.</p><p>ข้อมูลจากคู่มือทางการ</p></main>
			<footer>Noisy sidebar</footer></body></html>`)
	})
	source := domain.ContentSource{ID: "source-browser-fixture", URL: fixturePublicSource + "/standards/filter", Kind: "official", Fingerprint: "unobserved"}
	result, err := c.Research(t.Context(), p, source)
	if err != nil || result.ID != source.ID || result.URL != source.URL || result.Title != "วิธีดูแลตัวกรอง" ||
		result.PublishedAt == nil || result.PublishedAt.Format(time.RFC3339) != "2026-01-03T07:15:00Z" || result.CollectedAt == nil ||
		result.Fingerprint == source.Fingerprint || result.Status != "fetched" ||
		!strings.Contains(result.Text, "Replace the robot vacuum filter every 90 days.\nข้อมูลจากคู่มือทางการ") ||
		strings.Contains(result.Text, "Noisy sidebar") {
		t.Fatalf("source result %#v: %v", result, err)
	}
	source.Kind = "competitor"
	if _, err := c.Research(t.Context(), p, source); err == nil {
		t.Fatal("competitor text ingested")
	}
	source.Kind, source.URL = "primary", "https://official.example.com.other.com/source"
	if _, err := c.Research(t.Context(), p, source); !errors.Is(err, domain.ErrCrawlUnauthorized) {
		t.Fatal("foreign origin was not denied")
	}
	if requests.requests.Load() != 1 {
		t.Fatal("disallowed sources issued HTTP requests")
	}
}
