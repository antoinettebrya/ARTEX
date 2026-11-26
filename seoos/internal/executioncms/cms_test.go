package executioncms

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Autumn-27/artex/seoos/internal/domain"
)

const wpFixtureSite = "https://wordpress.example.test/blog"
const wpFixtureUser = "local-editor"
const wpFixturePassword = "0000 1111 2222 3333 4444 5555"
const genericFixtureSite = "https://generic.example.test"
const genericFixtureToken = "local-generic-fixture-only-token"

type mapTransport func(*http.Request) (*http.Response, error)

func (f mapTransport) RoundTrip(p *http.Request) (*http.Response, error) { return f(p) }

func mapFixture(t *testing.T, s *httptest.Server, logicalHost string) *http.Client {
	t.Helper()
	physical, err := url.Parse(s.URL)
	if err != nil {
		t.Fatal(err)
	}
	tlsClient := s.Client()
	return &http.Client{Transport: mapTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Scheme != "https" || r.URL.Host != logicalHost {
			t.Errorf("unexpected fixture destination %s", r.URL)
			return nil, domain.ErrExecutionAuthority
		}
		p := r.Clone(r.Context())
		p.URL.Scheme, p.URL.Host = physical.Scheme, physical.Host
		p.Host = logicalHost
		return tlsClient.Transport.RoundTrip(p)
	})}
}

func fixtureConnector(t *testing.T, s *httptest.Server, kind, site, login, password string, write bool) domain.CMSConnector {
	t.Helper()
	u, _ := url.Parse(site)
	c, err := New(domain.ExecutionConnection{ExecutionConnectionInput: domain.ExecutionConnectionInput{
		SiteID: "site", Kind: kind, BaseURL: site, Enabled: true, AllowWrites: write, TimeoutSeconds: 3,
	}}, domain.ExecutionCredential{Username: login, Secret: password}, mapFixture(t, s, u.Host))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func testPNG(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 3, 3))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

type wpFixture struct {
	t          *testing.T
	page       map[string]any
	plugin     string
	metaByPost string
}

func (f *wpFixture) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/blog/story/" {
		if r.Header.Get("Authorization") != "" {
			f.t.Error("CMS credential on anonymous read-back")
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, `<html><head><title>Public SEO title</title>
		<meta name="description" content="Observed description"><link href="/blog/story/" rel="canonical">
		</head><body><nav>Menu</nav><article><h1>Actual headline</h1><p>A <strong>verified</strong> article.</p>
		<script>ignored script</script></article></body></html>`)
		return
	}
	if r.URL.Path == "/blog/wp-sitemap.xml" {
		if r.Header.Get("Authorization") != "" {
			f.t.Error("CMS credential on anonymous sitemap read")
		}
		_, _ = io.WriteString(w, `<sitemapindex xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"></sitemapindex>`)
		return
	}
	user, secret, ok := r.BasicAuth()
	if !ok || user != wpFixtureUser || secret != wpFixturePassword {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	route := strings.TrimPrefix(r.URL.Path, "/blog/wp-json/")
	switch route {
	case "":
		root := fmt.Sprintf(`{"name":"WordPress fixture","home":%q,"namespaces":["wp/v2",%q],
				"routes":{"/wp/v2/posts":{"methods":["GET","POST"]},"/wp/v2/pages":{"methods":["GET","POST"]},
				"/wp/v2/media":{"methods":["GET","POST"]},"/wp/v2/categories":{"methods":["GET"]},
				"/wp/v2/tags":{"methods":["GET"]}}}`, wpFixtureSite, f.plugin)
		_, _ = io.WriteString(w, root)
	case "wp/v2/users/me":
		if r.URL.Query().Get("context") != "edit" {
			f.t.Error("missing edit context on authentication probe")
		}
		_, _ = io.WriteString(w, `{"id":2,"capabilities":{"edit_posts":true,"edit_pages":true,"publish_posts":true,"publish_pages":true,"upload_files":true}}`)
	case "wp/v2/categories":
		_, _ = io.WriteString(w, `[{"id":9,"slug":"tea","name":"Tea"}]`)
	case "wp/v2/tags":
		_, _ = io.WriteString(w, `[{"id":10,"slug":"bergamot","name":"Bergamot"}]`)
	case "wp/v2/media":
		if r.Method == http.MethodPost {
			if r.Header.Get("Content-Disposition") != "attachment; filename=cover.png" || r.Header.Get("Content-Type") != "image/png" {
				f.t.Errorf("incorrect media upload headers")
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"id":31,"source_url":"https://wordpress.example.test/blog/uploads/cover.png","mime_type":"image/png","title":{"raw":"Cover"}}`)
		} else {
			_, _ = io.WriteString(w, `[{"id":31,"source_url":"https://wordpress.example.test/blog/uploads/cover.png","mime_type":"image/png","title":{"rendered":"Cover"}}]`)
		}
	case "wp/v2/pages":
		if r.Method == http.MethodOptions {
			_, _ = io.WriteString(w, `{"schema":{"properties":{"meta":{"type":"object","properties":{}}}}}`)
			return
		}
		http.NotFound(w, r)
	case "wp/v2/posts", "wp/v2/posts/23":
		f.posts(w, r, route)
	default:
		http.NotFound(w, r)
	}
}

func (f *wpFixture) posts(w http.ResponseWriter, r *http.Request, route string) {
	if r.Method == http.MethodOptions {
		_, _ = io.WriteString(w, f.metaByPost)
		return
	}
	if r.URL.Query().Get("context") != "edit" {
		f.t.Error("rollback snapshot needs raw edit context")
	}
	if r.Method == http.MethodGet {
		if route == "wp/v2/posts/23" {
			writeJSON(w, f.page)
			return
		}
		w.Header().Set("X-WP-TotalPages", "1")
		if r.URL.Query().Get("slug") != "story" ||
			!strings.Contains(strings.Join(r.URL.Query()["status[]"], " "), "draft") {
			f.t.Error("draft recovery query must include exact slug and draft status")
		}
		writeJSON(w, []map[string]any{f.page, {"id": 24, "slug": "story-2", "status": "draft",
			"title": map[string]string{"raw": "unrelated"}, "content": map[string]string{"raw": "other"},
			"excerpt": map[string]string{"raw": ""}}})
		return
	}
	if r.Method != http.MethodPost {
		f.t.Errorf("unexpected article method %s", r.Method)
		http.Error(w, "unexpected method", 405)
		return
	}
	var input map[string]any
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		f.t.Error(err)
		return
	}
	if route == "wp/v2/posts" {
		if input["status"] != "draft" {
			f.t.Error("connection tried to create a live post")
		}
		f.page = map[string]any{"id": 23, "link": wpFixtureSite + "/story/",
			"yoast_head_json": map[string]string{"title": "computed title"}}
	}
	for _, k := range []string{"slug", "status", "meta", "date_gmt"} {
		if value, ok := input[k]; ok {
			f.page[k] = value
		}
	}
	for _, k := range []string{"title", "content", "excerpt"} {
		if value, ok := input[k]; ok {
			f.page[k] = map[string]any{"raw": value, "rendered": "different rendered value"}
		}
	}
	if m, ok := input["meta"].(map[string]any); ok {
		for k := range m {
			if !strings.Contains(f.metaByPost, `"`+k+`"`) || strings.Contains(k, "canonical") {
				f.t.Errorf("unadvertised or read-only SEO write field %s", k)
			}
		}
	}
	writeJSON(w, f.page)
}

func TestWordPressContract(t *testing.T) {
	fixture := &wpFixture{t: t, plugin: "yoast/v1", metaByPost: `{"schema":{"properties":{"meta":{"type":"object","properties":{
		"_yoast_wpseo_title":{"type":"string"},"_yoast_wpseo_canonical":{"type":"string","readonly":true}}}}}}`}
	server := httptest.NewTLSServer(fixture)
	defer server.Close()
	c := fixtureConnector(t, server, domain.ExecutionWordPress, wpFixtureSite, wpFixtureUser, wpFixturePassword, true)
	ctx := t.Context()

	info, err := c.GetSiteInfo(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if info.URL != wpFixtureSite || !info.Capabilities.Publish || !info.Capabilities.WriteMedia ||
		info.Capabilities.SEOPlugin != "yoast" || len(info.Capabilities.SEOFields) != 1 ||
		info.Capabilities.SEOFields[0] != "_yoast_wpseo_title" ||
		len(info.Categories) != 1 || len(info.Tags) != 1 {
		t.Fatalf("unexpected capabilities: %+v", info)
	}
	doc := domain.CMSPageInput{Resource: "post", Slug: "story", Title: "Actual headline",
		Body: "<p>Original text</p><!-- artex-publication:known -->", Excerpt: "Original excerpt",
		SEOTitle: "Draft SEO title", Description: "Not writable", Canonical: wpFixtureSite + "/story/"}
	page, err := c.CreatePage(ctx, doc)
	if err != nil || page.Status != "draft" || page.Body != doc.Body || page.SEOTitle != doc.SEOTitle ||
		page.Description != "" || !json.Valid(page.Raw) {
		t.Fatalf("draft %v, err %v", page, err)
	}
	list, err := c.ListPages(ctx, "post", "story", 3)
	if err != nil || len(list) != 1 || list[0].ID != page.ID || list[0].Body != doc.Body {
		t.Fatalf("unexpected recovery result %#v %v", list, err)
	}
	snapshot, err := c.GetPage(ctx, "post", page.ID)
	if err != nil || snapshot.Body != doc.Body || snapshot.Excerpt != doc.Excerpt {
		t.Fatalf("GET lost edit data: %#v %v", snapshot, err)
	}
	after, err := c.UpdatePage(ctx, page.ID, domain.CMSPageInput{
		Resource: "post", Title: doc.Title, Body: doc.Body + "<p>Revised.</p>", Excerpt: doc.Excerpt,
	})
	if err != nil || after.Status != "draft" || !strings.Contains(after.Body, "Revised.") || after.SEOTitle != "" {
		t.Fatalf("update must preserve status and explicit empty metadata: %#v %v", after, err)
	}
	at := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	scheduled, err := c.PublishPage(ctx, page.ID, domain.CMSPageInput{Resource: "post", ScheduledAt: &at})
	if err != nil || scheduled.Status != "future" || scheduled.ScheduledAt == nil || !scheduled.ScheduledAt.Equal(at) ||
		scheduled.Body != after.Body {
		t.Fatalf("scheduled result %#v %v", scheduled, err)
	}
	live, err := c.PublishPage(ctx, page.ID, doc)
	if err != nil || live.Status != "publish" || live.Body != doc.Body {
		t.Fatalf("publish %#v %v", live, err)
	}
	observed, err := c.ObservePublic(ctx, live.URL)
	if err != nil || observed.HTTPStatus != 200 || observed.Title != "Actual headline" ||
		observed.Body != "Actual headline A verified article." || observed.SEOTitle != "Public SEO title" ||
		observed.Description != "Observed description" || observed.Canonical != live.URL {
		t.Fatalf("public comparison %#v %v", observed, err)
	}
	if sitemap, err := c.GetSitemap(ctx); err != nil || !strings.Contains(sitemap.XML, "sitemapindex") || !strings.HasSuffix(sitemap.URL, "/wp-sitemap.xml") {
		t.Fatalf("sitemap %v %v", sitemap, err)
	}
	if uploaded, err := c.UploadMedia(ctx, "cover.png", "image/png", testPNG(t)); err != nil || uploaded.ID != "31" {
		t.Fatalf("upload %#v %v", uploaded, err)
	}
	if items, err := c.GetMedia(ctx, 2); err != nil || len(items) != 1 || items[0].ID != "31" {
		t.Fatalf("media list %#v %v", items, err)
	}
}

func TestWordPressDiscoversActualSEOFields(t *testing.T) {
	for _, tc := range []struct {
		name, namespace, schema, plugin, field string
	}{
		{"core without SEO plugin", "example/v1", "", "", ""},
		{"Yoast read only", "yoast/v1", `{"schema":{"properties":{"yoast_head_json":{"type":"object","readonly":true}}}}`, "yoast", ""},
		{"Rank Math field", "rankmath/v1", `{"schema":{"properties":{"meta":{"type":"object","properties":{
			"rank_math_description":{"type":"string","context":["edit"]},"rank_math_title":{"type":"string","context":["view"]}}}}}}`, "rank_math", "rank_math_description"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewTLSServer(&wpFixture{t: t, plugin: tc.namespace, metaByPost: tc.schema})
			defer server.Close()
			c := fixtureConnector(t, server, domain.ExecutionWordPress, wpFixtureSite, wpFixtureUser, wpFixturePassword, true)
			caps, err := c.TestConnection(t.Context())
			if err != nil || caps.SEOPlugin != tc.plugin {
				t.Fatalf("plugin probe %+v %v", caps, err)
			}
			if got := strings.Join(caps.SEOFields, ","); got != tc.field {
				t.Errorf("writable metadata %q, want %q", got, tc.field)
			}
		})
	}
}

func TestGenericCMSContractAndUnsupportedCapabilities(t *testing.T) {
	store := domain.CMSPage{ID: "article-01", URL: genericFixtureSite + "/story", Resource: "post", Slug: "story", Status: "draft"}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+genericFixtureToken {
			t.Error("missing Generic CMS bearer authorization")
			http.Error(w, "unauthorized", 401)
			return
		}
		switch strings.TrimPrefix(r.URL.Path, "/api/artex-cms/v1/") {
		case "site":
			writeJSON(w, domain.CMSSiteInfo{Name: "Portable CMS", URL: genericFixtureSite, Capabilities: domain.CMSCapabilities{
				ReadPages: true, WritePages: true, Publish: true, Schedule: false, SEOFields: []string{"description"},
			}})
		case "pages", "pages/article-01", "pages/article-01/publish":
			if r.Method == http.MethodGet {
				if r.URL.Query().Get("resource") != "post" {
					t.Error("resource must be explicit")
				}
				if strings.HasSuffix(r.URL.Path, "/pages") {
					writeJSON(w, map[string]any{"items": []domain.CMSPage{store}})
				} else {
					writeJSON(w, store)
				}
				return
			}
			var in map[string]any
			if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
				t.Error(err)
				return
			}
			if _, unknown := in["seo_title"]; unknown {
				t.Error("unsupported metadata was sent")
			}
			if v, ok := in["body"].(string); ok {
				store.Body = v
			}
			if v, ok := in["title"].(string); ok {
				store.Title = v
			}
			if v, ok := in["description"].(string); ok {
				store.Description = v
			}
			if v, ok := in["status"].(string); ok {
				store.Status = v
			}
			writeJSON(w, store)
		case "sitemap":
			_, _ = io.WriteString(w, `<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"></urlset>`)
		default:
			t.Errorf("unsupported route should not have been contacted: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	c := fixtureConnector(t, server, domain.ExecutionGeneric, genericFixtureSite, "", genericFixtureToken, true)
	ctx := t.Context()
	input := domain.CMSPageInput{Resource: "post", Slug: "story", Title: "Test", Body: "<p>draft</p>", SEOTitle: "ignored", Description: "first draft"}
	created, err := c.CreatePage(ctx, input)
	if err != nil || created.Status != "draft" || created.Body != input.Body || created.Description != input.Description {
		t.Fatalf("draft %#v %v", created, err)
	}
	if pages, err := c.ListPages(ctx, "post", input.Slug, 5); err != nil || len(pages) != 1 || pages[0].ID != created.ID {
		t.Fatalf("listing %#v %v", pages, err)
	}
	at := time.Now().Add(time.Hour)
	if _, err := c.PublishPage(ctx, created.ID, domain.CMSPageInput{Resource: "post", ScheduledAt: &at}); !errors.Is(err, domain.ErrExecutionUnsupported) {
		t.Fatalf("unexpected schedule result %v", err)
	}
	if _, err := c.UploadMedia(ctx, "cover.png", "image/png", testPNG(t)); !errors.Is(err, domain.ErrExecutionUnsupported) {
		t.Fatalf("unexpected unsupported media result %v", err)
	}
	if _, err := c.GetMedia(ctx, 1); !errors.Is(err, domain.ErrExecutionUnsupported) {
		t.Fatalf("unexpected unsupported media listing %v", err)
	}
	published, err := c.PublishPage(ctx, created.ID, domain.CMSPageInput{Resource: "post"})
	if err != nil || published.Status != "publish" || published.Body != created.Body {
		t.Fatalf("publish %#v %v", published, err)
	}
	read, err := c.GetPage(ctx, "post", created.ID)
	if err != nil || read.Body != input.Body || read.ID != published.ID {
		t.Fatalf("readback %#v %v", read, err)
	}
	input.Status, input.Description = "draft", ""
	if rolledBack, err := c.UpdatePage(ctx, created.ID, input); err != nil || rolledBack.Status != "draft" || rolledBack.Description != "" {
		t.Fatalf("rollback %#v %v", rolledBack, err)
	}
	if tree, err := c.GetSitemap(ctx); err != nil || !strings.Contains(tree.XML, "urlset") || !strings.HasSuffix(tree.URL, "/api/artex-cms/v1/sitemap") {
		t.Fatalf("sitemap %s %v", tree, err)
	}
}

func TestConnectionScopeBudgetAndRedaction(t *testing.T) {
	details := domain.ExecutionConnection{ExecutionConnectionInput: domain.ExecutionConnectionInput{
		Kind: domain.ExecutionGeneric, SiteID: "site", BaseURL: genericFixtureSite + "/blog", Enabled: true,
	}}
	secret := domain.ExecutionCredential{Secret: genericFixtureToken}
	budget := &domain.DataError{Code: "execution_budget", Retry: true, RetryAfter: 15 * time.Second, KnownUnbilled: true}
	transport := mapTransport(func(req *http.Request) (*http.Response, error) {
		return nil, budget
	})
	c, err := New(details, secret, &http.Client{Transport: transport})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.TestConnection(t.Context()); !errors.Is(err, budget) {
		t.Fatalf("wrapped budget not preserved: %v", err)
	}
	for _, u := range []string{"http://generic.example.test/blog", "https://generic.example.test/other", "https://other.example.test/blog"} {
		if _, err := c.ObservePublic(t.Context(), u); !errors.Is(err, domain.ErrExecutionAuthority) {
			t.Errorf("scope passed for %s: %v", u, err)
		}
	}
	if _, err := c.CreatePage(t.Context(), domain.CMSPageInput{Resource: "post"}); !errors.Is(err, domain.ErrExecutionAuthority) {
		t.Errorf("read-only credential passed write guard: %v", err)
	}
	details.BaseURL = "https://localhost"
	if _, err := New(details, secret, nil); err == nil {
		t.Fatal("accepted an unapproved local destination")
	}
	details.BaseURL = "http://generic.example.test"
	if _, err := New(details, secret, nil); err == nil {
		t.Fatal("accepted cleartext credential transport")
	}

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/artex-cms/v1/site":
			_, _ = io.WriteString(w, `{"name":"`+genericFixtureToken+`"}`)
		case "/redirect":
			http.Redirect(w, r, "https://other.example.test/credential-relay", http.StatusFound)
		case "/busy":
			w.Header().Set("Retry-After", "31")
			w.WriteHeader(http.StatusTooManyRequests)
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
		}
	}))
	defer server.Close()
	c = fixtureConnector(t, server, domain.ExecutionGeneric, genericFixtureSite, "", genericFixtureToken, false)
	var outcome *domain.DataError
	if _, err := c.TestConnection(t.Context()); !errors.As(err, &outcome) || outcome.Code != "cms_sensitive_response" ||
		strings.Contains(err.Error(), genericFixtureToken) {
		t.Fatalf("incorrect reflection handling: %v", err)
	}
	if page, err := c.ObservePublic(t.Context(), genericFixtureSite+"/redirect"); err != nil || page.HTTPStatus != 302 {
		t.Fatalf("redirect not observed in place: %+v %v", page, err)
	}
	if _, err := c.ObservePublic(t.Context(), genericFixtureSite+"/busy"); !errors.As(err, &outcome) ||
		outcome.Code != "cms_http_429" || outcome.RetryAfter != 31*time.Second {
		t.Fatalf("rate limit not classified: %v", err)
	}
}
