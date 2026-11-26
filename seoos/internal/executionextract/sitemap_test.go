package executionextract

import (
	"context"
	"encoding/xml"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/Autumn-27/artex/seoos/internal/domain"
)

type localTransport func(*http.Request) (*http.Response, error)

func (c localTransport) RoundTrip(r *http.Request) (*http.Response, error) { return c(r) }

func TestSitemapIndexHreflangAndScopedExport(t *testing.T) {
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/robots.txt":
			io.WriteString(w, "Sitemap: https://site.example.com/index.xml\nSitemap: https://site.example.com/extra.xml\n")
		case "/extra.xml":
			io.WriteString(w, `<urlset><url><loc>https://site.example.com/c</loc></url></urlset>`)
		case "/index.xml":
			io.WriteString(w, `<sitemapindex><sitemap><loc>https://site.example.com/part.xml</loc></sitemap><sitemap><loc>https://outside.example.com/other.xml</loc></sitemap></sitemapindex>`)
		case "/part.xml":
			io.WriteString(w, `<urlset xmlns:xhtml="http://www.w3.org/1999/xhtml"><url><loc>https://site.example.com/a</loc><lastmod>2026-08-10</lastmod><xhtml:link rel="alternate" hreflang="fr-CA" href="https://site.example.com/fr/a"/></url><url><loc>https://site.example.com/b</loc></url><url><loc>https://site.example.com/c</loc></url><url><loc>https://outside.example.com/x</loc></url></urlset>`)
		default:
			t.Errorf("unexpected fetch %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer local.Close()
	endpoint, _ := url.Parse(local.URL)
	h := &http.Client{Transport: localTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "site.example.com" {
			t.Errorf("outside-scope host %s", r.URL.Host)
		}
		req := r.Clone(r.Context())
		req.URL.Scheme, req.URL.Host = endpoint.Scheme, endpoint.Host
		return http.DefaultTransport.RoundTrip(req)
	})}
	got, err := Sitemap(context.Background(), h, domain.Site{BaseURL: "https://site.example.com"}, "", "")
	if err != nil || len(got.Children) != 2 || len(got.Entries) != 3 || len(got.Entries[1].Alternate) != 1 || got.Entries[1].LastMod != "2026-08-10" {
		t.Fatalf("unexpected sitemap %v %#v", err, got)
	}
	files, err := GenerateSitemaps("https://site.example.com", got.Entries, 2)
	if err != nil || len(files) != 3 {
		t.Fatalf("split: %v %#v", err, files)
	}
	idx := files["artex-sitemap-index.xml"]
	if strings.Count(idx, "<sitemap>") != 2 {
		t.Fatalf("invalid index membership: %s", idx)
	}
	var exported sitemapSource
	if err := xml.Unmarshal([]byte(files["artex-sitemap-0001.xml"]), &exported); err != nil || len(exported.Entries) != 2 || exported.Entries[1].Related[0].Language != "fr-CA" {
		t.Fatalf("export lost hreflang: %#v %v", exported, err)
	}
}
