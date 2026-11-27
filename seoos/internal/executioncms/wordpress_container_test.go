package executioncms

import (
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Autumn-27/artex/seoos/internal/domain"
	"github.com/google/uuid"
)

// TestWordPressContainer uses an isolated real Apache WordPress container and
// a separate, locally trusted HTTPS test ingress. Run scripts/test_wordpress.sh
// to create and clean up the WordPress and MariaDB instances.
func TestWordPressContainer(t *testing.T) {
	backendAddress := os.Getenv("SEOOS_WP_TEST_HTTP")
	if backendAddress == "" {
		t.Skip("run scripts/test_wordpress.sh to provision a disposable WordPress instance")
	}
	backend, err := url.Parse(backendAddress)
	if err != nil || backend.Scheme != "http" || backend.User != nil || backend.Path != "" ||
		backend.Port() == "" || !net.ParseIP(backend.Hostname()).IsLoopback() ||
		backend.RawQuery != "" || backend.ForceQuery || backend.Fragment != "" {
		t.Fatal("WordPress integration fixture must use an explicit loopback origin")
	}
	p := &httputil.ReverseProxy{
		Rewrite: func(request *httputil.ProxyRequest) {
			request.SetURL(backend)
			request.Out.Host = request.In.Host
			request.Out.Header.Set("X-Forwarded-Proto", "https")
		},
		Transport: &http.Transport{ResponseHeaderTimeout: 8 * time.Second},
	}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/wp-json/") && r.Header.Get("Authorization") != "" {
			t.Error("public URL unexpectedly received the WordPress Application Password")
			http.Error(w, "unexpected public request authorization", http.StatusBadRequest)
			return
		}
		p.ServeHTTP(w, r)
	}))
	defer server.Close()
	const site = "https://wordpress.example.test"
	login := os.Getenv("SEOOS_WP_TEST_USER")
	password := os.Getenv("SEOOS_WP_TEST_PASSWORD")
	if login == "" || password == "" {
		t.Fatal("WordPress fixture application credential is unavailable")
	}
	ctx := t.Context()
	c := fixtureConnector(t, server, domain.ExecutionWordPress, site, login, password, true)
	bad := fixtureConnector(t, server, domain.ExecutionWordPress, site, login, "unauthorized-fixture-token", true)
	var responseError *domain.DataError
	if _, err := bad.TestConnection(ctx); !errors.As(err, &responseError) ||
		responseError.Code != "cms_http_401" && responseError.Code != "cms_http_403" {
		t.Fatalf("invalid application password did not receive an auth rejection: %v", err)
	}
	info, err := c.GetSiteInfo(ctx)
	if err != nil || info.URL != site || !info.Capabilities.ReadPages || !info.Capabilities.WritePages ||
		!info.Capabilities.Publish || !info.Capabilities.Schedule || !info.Capabilities.WriteMedia ||
		info.Capabilities.SEOPlugin != "" || len(info.Categories) < 1 || len(info.Tags) < 1 {
		t.Fatalf("capability or taxonomy discovery failed: %+v, error %v", info, err)
	}
	key := "artex-" + uuid.NewString()
	draftInput := domain.CMSPageInput{Resource: "post", Slug: key, Title: "ARTEX fixture original title",
		Body: "<p>Original test publication.</p><!-- artex-publication:" + key + " -->", Excerpt: "Original synopsis"}
	draft, err := c.CreatePage(ctx, draftInput)
	if err != nil || draft.ID == "" || draft.Status != "draft" || draft.Body != draftInput.Body {
		t.Fatalf("create draft: %s %v", draft.Status, err)
	}
	list, err := c.ListPages(ctx, "post", draftInput.Slug, 10)
	if err != nil || len(list) != 1 || list[0].ID != draft.ID || list[0].Body != draftInput.Body {
		t.Fatalf("find persistent draft marker, got %d results: %v", len(list), err)
	}
	live, err := c.PublishPage(ctx, draft.ID, domain.CMSPageInput{Resource: "post"})
	if err != nil || live.Status != "publish" || live.ID != draft.ID || live.URL == "" {
		t.Fatalf("publish previously staged draft: %s %v", live.Status, err)
	}
	verify := func(title, marker string) {
		t.Helper()
		observed, err := c.ObservePublic(ctx, live.URL)
		if err != nil || observed.HTTPStatus != 200 || observed.Title != title ||
			!strings.Contains(observed.Body, marker) || !strings.Contains(observed.SEOTitle, title) ||
			observed.Canonical != live.URL {
			t.Fatalf("public article read-back did not match: code %d title %q canonical %q, error %v",
				observed.HTTPStatus, observed.Title, observed.Canonical, err)
		}
	}
	verify(draftInput.Title, "Original test publication.")
	before, err := c.GetPage(ctx, "post", live.ID)
	if err != nil || before.ID != live.ID || before.Body != draftInput.Body {
		t.Fatalf("capture actual rollback snapshot: %v", err)
	}
	revised, err := c.UpdatePage(ctx, live.ID, domain.CMSPageInput{Resource: "post", Title: "ARTEX fixture updated title",
		Body: "<p>Updated fixture publication.</p>", Excerpt: "Updated synopsis"})
	if err != nil || revised.Status != "publish" {
		t.Fatalf("update live article under fixture authorization: %v", err)
	}
	verify(revised.Title, "Updated fixture publication.")
	restored, err := c.UpdatePage(ctx, before.ID, domain.CMSPageInput{
		Resource: before.Resource, Slug: before.Slug, Status: before.Status,
		Title: before.Title, Body: before.Body, Excerpt: before.Excerpt, ScheduledAt: before.ScheduledAt,
	})
	if err != nil || restored.Status != before.Status || restored.Body != before.Body {
		t.Fatalf("restore previous version at the same WordPress post ID: %v", err)
	}
	verify(before.Title, "Original test publication.")

	page, err := c.CreatePage(ctx, domain.CMSPageInput{Resource: "page", Slug: key + "-page",
		Title: "Scheduled fixture page", Body: "<p>Schedule only.</p>"})
	if err != nil || page.Status != "draft" {
		t.Fatalf("create Page draft: %v", err)
	}
	scheduledAt := time.Now().UTC().Add(2 * time.Hour).Truncate(time.Second)
	scheduled, err := c.PublishPage(ctx, page.ID, domain.CMSPageInput{Resource: "page", ScheduledAt: &scheduledAt})
	if err != nil || scheduled.Status != "future" || scheduled.ScheduledAt == nil || !scheduled.ScheduledAt.Equal(scheduledAt) {
		t.Fatalf("schedule Page draft: status %s, error %v", scheduled.Status, err)
	}
	if _, err := c.UpdatePage(ctx, page.ID, domain.CMSPageInput{Resource: "page", Slug: page.Slug,
		Title: page.Title, Body: page.Body, Excerpt: page.Excerpt, Status: "draft"}); err != nil {
		t.Fatalf("return scheduled fixture page to draft: %v", err)
	}
	media, err := c.UploadMedia(ctx, key+".png", "image/png", testPNG(t))
	if err != nil || media.ID == "" || media.MediaType != "image/png" {
		t.Fatalf("upload media to disposable fixture: %v", err)
	}
	items, err := c.GetMedia(ctx, 10)
	if err != nil || len(items) < 1 || items[0].ID != media.ID {
		t.Fatalf("read media back: %v", err)
	}
	if tree, err := c.GetSitemap(ctx); err != nil || !strings.Contains(tree.XML, "sitemapindex") || !strings.HasSuffix(tree.URL, "/wp-sitemap.xml") {
		t.Fatalf("read WordPress sitemap: %v", err)
	}
	t.Log("Verified WordPress editor authentication, taxonomy and media reads, draft marker lookup, scheduled and immediate publication, anonymous read-back, and restoration at the original post ID.")
}
