package executionextract

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Autumn-27/artex/seoos/internal/domain"
)

type extractTestTransport func(*http.Request) (*http.Response, error)

func (fn extractTestTransport) RoundTrip(req *http.Request) (*http.Response, error) { return fn(req) }

func fixturePDF() string {
	content := "BT /F1 18 Tf 20 100 Td (Collected from the real PDF text layer) Tj ET"
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 480 180] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		"<< /Title (  Crawler PDF title  ) /Author (ARTEX fixture) /Subject (Content extraction) /CreationDate (D:20260601000000Z) >>",
	}
	var document strings.Builder
	document.WriteString("%PDF-1.4\n")
	offsets := []int{0}
	for i, obj := range objects {
		offsets = append(offsets, document.Len())
		fmt.Fprintf(&document, "%d 0 obj\n%s\nendobj\n", i+1, obj)
	}
	xrefOffset := document.Len()
	fmt.Fprintf(&document, "xref\n0 %d\n0000000000 65535 f \n", len(offsets))
	for _, offset := range offsets[1:] {
		fmt.Fprintf(&document, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&document, "trailer\n<< /Size %d /Root 1 0 R /Info 6 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets), xrefOffset)
	return document.String()
}

func TestPDFRealPoppler(t *testing.T) {
	for _, tool := range []string{"pdfinfo", "pdftotext"} {
		if _, err := exec.LookPath(tool); err != nil {
			if os.Getenv("SEOOS_REQUIRE_POPPLER") == "1" {
				t.Fatalf("missing %s", tool)
			}
			t.Skipf("install Poppler for the PDF integration test (%s unavailable)", tool)
		}
	}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.Header.Get("Authorization") != "" || r.Header.Get("Accept") != "application/pdf" {
			t.Error("expected a public PDF GET")
		}
		switch r.URL.Path {
		case "/file.pdf":
			w.Header().Set("Content-Type", "application/pdf")
			fmt.Fprint(w, fixturePDF())
		case "/too-big.pdf":
			fmt.Fprint(w, "%PDF-1.4\n", strings.Repeat("x", pdfMaxBytes))
		case "/invalid.pdf":
			fmt.Fprint(w, "<html>this is not a PDF</html>")
		case "/redirect.pdf":
			http.Redirect(w, r, "/should-not-fetch", http.StatusFound)
		default:
			t.Errorf("unexpected PDF request %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	extracted, err := PDF(context.Background(), server.Client(), server.URL+"/file.pdf")
	if err != nil || extracted.Kind != domain.ExecPDF || extracted.Requests != 1 || extracted.Pages != 1 || extracted.Truncated ||
		extracted.Title != "Crawler PDF title" || extracted.Text != "Collected from the real PDF text layer" ||
		extracted.Metadata["author"] != "ARTEX fixture" || extracted.Metadata["created_at"] == "" {
		t.Fatalf("extract PDF = %#v, %v", extracted, err)
	}
	for route, code := range map[string]string{"/invalid.pdf": "pdf_format_invalid", "/too-big.pdf": "pdf_size_limit", "/redirect.pdf": "extraction_http_status"} {
		_, err := PDF(context.Background(), server.Client(), server.URL+route)
		var data *domain.DataError
		if !errors.As(err, &data) || data.Code != code {
			t.Errorf("PDF %s returned %v, want %s", route, err, code)
		}
	}
}

func TestBoundedExtraction(t *testing.T) {
	if _, err := boundedExtractionRead(strings.NewReader("abc"), 2); !errors.Is(err, errExtractionLimit) {
		t.Fatalf("bounded reader = %v", err)
	}
	output := &cappedExtractionOutput{limit: 2}
	if n, err := output.Write([]byte("abc")); n != 2 || !errors.Is(err, errExtractionLimit) || !output.capped {
		t.Fatalf("capped writer = %d, %v", n, err)
	}
	if got := extractionText("  字\r\n🧮 ", 5); got != "字 " {
		t.Fatalf("unexpected UTF-8 text %q", got)
	}
	title, pages, metadata := parsePDFInfo("Title:   About: PDFs   \nPages: 101\nSubject: A formal report\nUnknown: omitted\n")
	if title != "About: PDFs" || pages != 101 || metadata["subject"] != "A formal report" || metadata["unknown"] != "" {
		t.Fatalf("metadata = %q %d %#v", title, pages, metadata)
	}
}

func extractTestSite() (domain.Site, domain.CrawlPolicy) {
	site := domain.Site{ID: "render-site", BaseURL: "https://render.example.test/owned"}
	return site, domain.CrawlPolicy{SiteID: site.ID, Enabled: true}
}

func TestBrowserBrokerAuthorizationAndBudget(t *testing.T) {
	site, policy := extractTestSite()
	const target = "https://render.example.test/owned/index"
	budget := &domain.DataError{Code: "execution_budget", Retry: true, RetryAfter: 19 * time.Second, KnownUnbilled: true}
	var hits []string
	client := &http.Client{Transport: extractTestTransport(func(req *http.Request) (*http.Response, error) {
		hits = append(hits, req.URL.String())
		if req.URL.Path == "/owned/quota" {
			return nil, budget
		}
		return &http.Response{StatusCode: 200, Header: http.Header{
			"Content-Type": []string{"text/html"}, "Set-Cookie": []string{"unexpected=1"},
		}, Body: io.NopCloser(strings.NewReader("<p>Real response</p>"))}, nil
	})}
	broker := browserFetches{client: extractionHTTPClient(client), site: site, policy: policy, target: target}
	for _, event := range []browserMessage{
		{ID: 1, URL: "https://external.example.test/owned/index", Method: "GET", Resource: "script"},
		{ID: 2, URL: "https://render.example.test/other", Method: "GET", Resource: "fetch"},
		{ID: 3, URL: target, Method: "POST", Resource: "xhr"},
		{ID: 4, URL: target, Method: "GET", Resource: "image"},
		{ID: 5, URL: target + "?" + strings.Repeat("q", browserMaxQueryBytes+1), Method: "GET", Resource: "script"},
	} {
		if result := broker.fetch(context.Background(), event); result.OK {
			t.Fatalf("allowed forbidden browser request %#v", event)
		}
	}
	if len(hits) != 0 {
		t.Fatalf("unapproved targets reached the client: %v", hits)
	}
	event := browserMessage{ID: 6, URL: target, Method: "GET", Resource: "document"}
	result := broker.fetch(context.Background(), event)
	if !result.OK || broker.requests != 1 || result.Headers["Set-Cookie"] != "" || !broker.document {
		t.Fatalf("browser document = %#v, state = %#v", result, broker)
	}
	broker.fetch(context.Background(), browserMessage{ID: 7, URL: "https://render.example.test/owned/quota", Method: "GET", Resource: "fetch"})
	extract, err := broker.extraction(browserMessage{URL: target})
	if extract.URL != "" || err != budget || broker.requests != 1 {
		t.Fatalf("failed to preserve execution budget: %#v, %v, requests=%d", extract, err, broker.requests)
	}
}

func TestBrowserBrokerResponseLimits(t *testing.T) {
	site, policy := extractTestSite()
	const target = "https://render.example.test/owned/index"
	client := &http.Client{Transport: extractTestTransport(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{
			"Content-Type": []string{"text/html"},
		}, Body: io.NopCloser(strings.NewReader(strings.Repeat("x", browserMaxAssetBytes+1)))}, nil
	})}
	broker := browserFetches{client: extractionHTTPClient(client), site: site, policy: policy, target: target}
	event := browserMessage{ID: 1, URL: target, Method: "GET", Resource: "document"}
	if reply := broker.fetch(context.Background(), event); reply.OK || !broker.truncated || broker.document || broker.totalBytes != browserMaxAssetBytes+1 {
		t.Fatalf("accepted oversized main document: %#v", broker)
	}
	broker.requests = browserMaxRequests
	broker.fetch(context.Background(), event)
	if broker.requests != browserMaxRequests || broker.blocked != 1 {
		t.Fatalf("did not enforce browser request allowance: %#v", broker)
	}
}

func TestBrowserRealChromium(t *testing.T) {
	if os.Getenv("SEOOS_PLAYWRIGHT_MODULE") == "" && os.Getenv("SEOOS_REQUIRE_CHROMIUM") != "1" {
		t.Skip("set SEOOS_PLAYWRIGHT_MODULE to run the real Chromium integration test")
	}
	if os.Getenv("SEOOS_BROWSER_RENDERER") == "" {
		renderer, err := filepath.Abs("../../scripts/browser_extract.cjs")
		if err != nil {
			t.Fatal(err)
		}
		t.Setenv("SEOOS_BROWSER_RENDERER", renderer)
	}
	var requests []string
	var mu sync.Mutex
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests = append(requests, r.URL.Path)
		mu.Unlock()
		if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
			t.Error("browser broker forwarded a credential")
		}
		switch r.URL.Path {
		case "/owned/index":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Set-Cookie", "discard=test-value")
			fmt.Fprint(w, `<!doctype html><title>Initial source title</title>
<nav>Global navigation</nav><main>
<h1 id="heading">Unrendered title</h1><h2 style="display:none">Hidden heading</h2>
<script src="/owned/render.js"></script></main>`)
		case "/owned/render.js":
			w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
			fmt.Fprint(w, `document.title = 'Actual Chromium title';
			document.querySelector('#heading').textContent = 'Rendered heading';
			fetch('/owned/data.json').then(response => response.text()).then(text => {
			  const article = document.createElement('article');
			  article.textContent = text;
			  document.querySelector('main').append(article);
			});
			fetch('/outside-scope').catch(() => {});
			fetch('https://outside.example.test/assets/info.json').catch(() => {});`)
		case "/owned/data.json":
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			fmt.Fprint(w, "Dynamically fetched crawler content")
		default:
			t.Errorf("browser made an extra network request to %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	dialTarget, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	transport := server.Client().Transport
	client := &http.Client{Transport: extractTestTransport(func(req *http.Request) (*http.Response, error) {
		if req.URL.Scheme != "https" || req.URL.Host != "render.example.test" || !strings.HasPrefix(req.URL.Path, "/owned/") {
			t.Errorf("unapproved request reached the browser broker transport: %v", req.URL)
			return nil, errors.New("unapproved request")
		}
		req = req.Clone(req.Context())
		req.URL.Scheme, req.URL.Host = dialTarget.Scheme, dialTarget.Host
		return transport.RoundTrip(req)
	})}
	site, policy := extractTestSite()
	result, err := Browser(context.Background(), client, site, policy, "https://render.example.test/owned/index")
	if err != nil || result.Title != "Actual Chromium title" || result.Requests != 3 ||
		result.Metadata["headings"] != "H1 Rendered heading" || result.Metadata["blocked_requests"] != "2" ||
		!strings.Contains(result.Text, "Dynamically fetched crawler content") || strings.Contains(result.Text, "Unrendered title") ||
		strings.Contains(result.Text, "Global navigation") ||
		result.Truncated {
		t.Fatalf("real browser extraction = %#v, %v", result, err)
	}
	mu.Lock()
	defer mu.Unlock()
	slices.Sort(requests)
	if !slices.Equal(requests, []string{"/owned/data.json", "/owned/index", "/owned/render.js"}) {
		t.Fatalf("unexpected TLS origin requests: %v", requests)
	}
}
