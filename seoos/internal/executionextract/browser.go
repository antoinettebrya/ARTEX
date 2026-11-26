package executionextract

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Autumn-27/artex/seoos/internal/contentinventory"
	"github.com/Autumn-27/artex/seoos/internal/domain"
)

const (
	browserMaxRequests   = 16
	browserMaxEvents     = 48
	browserMaxAssetBytes = 1 << 20
	browserMaxTotalBytes = 5 << 20
	browserMaxQueryBytes = 1024
)

type browserMessage struct {
	Type      string   `json:"type"`
	ID        int      `json:"id"`
	URL       string   `json:"url"`
	Method    string   `json:"method"`
	Resource  string   `json:"resource"`
	Title     string   `json:"title"`
	Text      string   `json:"text"`
	Headings  []string `json:"headings"`
	Truncated bool     `json:"truncated"`
}

type browserReply struct {
	ID      int               `json:"id"`
	OK      bool              `json:"ok"`
	Status  int               `json:"status,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    []byte            `json:"body,omitempty"`
}

type browserFetches struct {
	client            *http.Client
	site              domain.Site
	policy            domain.CrawlPolicy
	target            string
	requests, blocked int
	totalBytes        int
	truncated         bool
	document          bool
	documentErr       error
	limitErr          error
}

// Browser renders a single owned page in a disposable Chromium session. The
// worker's HTTP client is the only path that can reach the origin.
func Browser(ctx context.Context, client *http.Client, site domain.Site, policy domain.CrawlPolicy, target string) (domain.ExecutionExtraction, error) {
	scope, err := contentinventory.ValidatePageURL(site, policy, target)
	if err != nil {
		return domain.ExecutionExtraction{}, err
	}
	renderer := os.Getenv("SEOOS_BROWSER_RENDERER")
	if renderer == "" {
		renderer = "scripts/browser_extract.cjs"
	}
	node, err := exec.LookPath("node")
	if err != nil {
		return domain.ExecutionExtraction{}, &domain.DataError{Code: "browser_runtime_unavailable"}
	}
	if _, err := os.Stat(renderer); err != nil {
		return domain.ExecutionExtraction{}, &domain.DataError{Code: "browser_runtime_unavailable"}
	}
	ctx, cancel := context.WithTimeout(ctx, 24*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, node, "--max-old-space-size=128", renderer)
	cmd.Env = browserEnvironment()
	cmd.Stderr = io.Discard
	cmd.WaitDelay = 2 * time.Second
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	input, err := cmd.StdinPipe()
	if err != nil {
		return domain.ExecutionExtraction{}, &domain.DataError{Code: "browser_runtime_unavailable"}
	}
	defer input.Close()
	output, err := cmd.StdoutPipe()
	if err != nil {
		return domain.ExecutionExtraction{}, &domain.DataError{Code: "browser_runtime_unavailable"}
	}
	defer output.Close()
	if err := cmd.Start(); err != nil {
		return domain.ExecutionExtraction{}, &domain.DataError{Code: "browser_runtime_unavailable"}
	}
	defer func() {
		input.Close()
		output.Close()
		cancel()
		_ = cmd.Wait()
	}()
	encoder := json.NewEncoder(input)
	if err := encoder.Encode(map[string]string{"url": scope}); err != nil {
		return domain.ExecutionExtraction{}, &domain.DataError{Code: "browser_protocol_error"}
	}
	broker := browserFetches{client: extractionHTTPClient(client), site: site, policy: policy, target: scope}
	lines := bufio.NewScanner(output)
	lines.Buffer(make([]byte, 16<<10), 1<<20)
	for event := 0; event < browserMaxEvents && lines.Scan(); event++ {
		var message browserMessage
		if err := json.Unmarshal(lines.Bytes(), &message); err != nil {
			return domain.ExecutionExtraction{}, &domain.DataError{Code: "browser_protocol_error"}
		}
		switch message.Type {
		case "request":
			if err := encoder.Encode(broker.fetch(ctx, message)); err != nil {
				return domain.ExecutionExtraction{}, &domain.DataError{Code: "browser_protocol_error"}
			}
		case "result":
			return broker.extraction(message)
		case "unavailable":
			return domain.ExecutionExtraction{}, &domain.DataError{Code: "browser_runtime_unavailable"}
		case "failed":
			return domain.ExecutionExtraction{}, broker.renderError(ctx)
		default:
			return domain.ExecutionExtraction{}, &domain.DataError{Code: "browser_protocol_error"}
		}
	}
	return domain.ExecutionExtraction{}, broker.renderError(ctx)
}

func (b *browserFetches) fetch(ctx context.Context, message browserMessage) browserReply {
	reply := browserReply{ID: message.ID}
	validURL, err := contentinventory.ValidatePageURL(b.site, b.policy, message.URL)
	if err != nil || message.ID <= 0 || message.Method != http.MethodGet || !browserResource(message.Resource) {
		b.blocked++
		return reply
	}
	parsed, _ := url.Parse(validURL)
	mainDocument := message.Resource == "document" && validURL == b.target && !b.document
	if len(parsed.RawQuery) > browserMaxQueryBytes || (message.Resource == "document" && !mainDocument) {
		b.blocked++
		return reply
	}
	if b.requests >= browserMaxRequests || b.totalBytes >= browserMaxTotalBytes-1 || b.limitErr != nil {
		b.truncated = true
		b.blocked++
		return reply
	}
	assetCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	res, err := extractionGET(assetCtx, b.client, validURL, "*/*")
	if err != nil {
		var dataErr *domain.DataError
		if errors.As(err, &dataErr) && (dataErr.Code == "execution_budget" || dataErr.Code == "execution_rate_limit") {
			b.limitErr = err
			return reply
		}
		b.requests++
		if mainDocument {
			b.documentErr = err
		}
		b.truncated = true
		return reply
	}
	b.requests++
	defer res.Body.Close()
	// Only successful same-origin responses may be fulfilled. Redirects are
	// withheld, so even Playwright's redirect behavior cannot escape this scope.
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		if res.StatusCode == http.StatusTooManyRequests {
			b.limitErr = extractionStatus(res)
		} else if mainDocument {
			b.documentErr = extractionStatus(res)
		}
		b.blocked++
		b.truncated = true
		return reply
	}
	contentType := res.Header.Get("Content-Type")
	kind, _, err := mime.ParseMediaType(contentType)
	if mainDocument && (err != nil || (kind != "text/html" && kind != "application/xhtml+xml")) {
		b.documentErr = &domain.DataError{Code: "browser_document_type"}
		return reply
	}
	remaining := browserMaxTotalBytes - b.totalBytes
	assetLimit := min(browserMaxAssetBytes, remaining-1)
	body, err := boundedExtractionRead(res.Body, assetLimit)
	b.totalBytes += len(body)
	if err != nil {
		if mainDocument {
			b.documentErr = &domain.DataError{Code: "browser_document_read_failed", Retry: !errors.Is(err, errExtractionLimit)}
		}
		b.truncated = true
		return reply
	}
	b.document = b.document || mainDocument
	headers := make(map[string]string, 3)
	for _, name := range []string{"Content-Type", "Content-Security-Policy", "X-Content-Type-Options"} {
		if value := res.Header.Get(name); value != "" {
			headers[name] = value
		}
	}
	return browserReply{ID: message.ID, OK: true, Status: res.StatusCode, Headers: headers, Body: body}
}

func (b *browserFetches) extraction(message browserMessage) (domain.ExecutionExtraction, error) {
	if b.limitErr != nil || b.documentErr != nil || !b.document {
		return domain.ExecutionExtraction{}, b.renderError(context.Background())
	}
	actual, err := contentinventory.ValidatePageURL(b.site, b.policy, message.URL)
	if err != nil {
		return domain.ExecutionExtraction{}, err
	}
	headings := make([]string, 0, min(len(message.Headings), 100))
	for _, heading := range message.Headings[:min(len(message.Headings), 100)] {
		if text := extractionText(heading, 256); text != "" {
			headings = append(headings, text)
		}
	}
	return domain.ExecutionExtraction{
		URL: actual, Kind: domain.ExecBrowser, Title: extractionText(message.Title, 1024),
		Text: extractionText(message.Text, extractTextLimit), Requests: b.requests,
		Truncated: b.truncated || message.Truncated || len(message.Text) > extractTextLimit || len(message.Headings) > 100,
		Metadata: map[string]string{
			"extractor": "chromium", "headings": strings.Join(headings, "\n"), "blocked_requests": strconv.Itoa(b.blocked),
		},
	}, nil
}

func (b *browserFetches) renderError(ctx context.Context) error {
	if b.limitErr != nil {
		return b.limitErr
	}
	if b.documentErr != nil {
		return b.documentErr
	}
	if ctx.Err() != nil {
		return &domain.DataError{Code: "browser_extract_timeout", Retry: true}
	}
	return &domain.DataError{Code: "browser_render_failed", Retry: true}
}

func browserResource(kind string) bool {
	switch kind {
	case "document", "script", "stylesheet", "fetch", "xhr":
		return true
	default:
		return false
	}
}

func browserEnvironment() []string {
	env := []string{"LANG=C", "LC_ALL=C"}
	for _, name := range []string{"PATH", "HOME", "TMPDIR", "PLAYWRIGHT_BROWSERS_PATH", "SEOOS_PLAYWRIGHT_CHANNEL", "SEOOS_PLAYWRIGHT_MODULE", "SEOOS_CHROMIUM_EXECUTABLE"} {
		if value, ok := os.LookupEnv(name); ok {
			env = append(env, name+"="+value)
		}
	}
	return env
}
