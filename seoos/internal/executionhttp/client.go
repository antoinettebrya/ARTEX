package executionhttp

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Autumn-27/artex/seoos/internal/contentinventory"
	"github.com/Autumn-27/artex/seoos/internal/domain"
	"github.com/Autumn-27/artex/seoos/internal/providerhttp"
)

type Ledger struct {
	Provider string
	Secrets  []string
	Reserve  func(context.Context) (int64, error)
	Settle   func(context.Context, int64, domain.ProviderReceipt) error
}

type countedTransport struct {
	transport http.RoundTripper
	ledger    Ledger
}

func (c countedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	id, err := c.ledger.Reserve(req.Context())
	if err != nil {
		return nil, err
	}
	start := time.Now()
	provider := c.ledger.Provider
	if provider == "" {
		provider = "site"
	}
	zero := int64(0)
	receipt := domain.ProviderReceipt{Provider: provider, Operation: operation(req, provider),
		CostSource: "no_metered_fee", CostMicros: &zero}
	settle := func() {
		receipt.DurationMS = time.Since(start).Milliseconds()
		ctx, cancel := context.WithTimeout(context.WithoutCancel(req.Context()), 3*time.Second)
		defer cancel()
		if err := c.ledger.Settle(ctx, id, receipt); err != nil {
			slog.Warn("Execution request receipt could not be recorded")
		}
	}
	response, err := c.transport.RoundTrip(req)
	if err != nil {
		receipt.Category, receipt.Code, receipt.Retryable = "transport", "execution_transport", req.Context().Err() != context.Canceled
		if req.Context().Err() == context.DeadlineExceeded {
			receipt.Category, receipt.Code = "timeout", "execution_timeout"
		}
		settle()
		return nil, err
	}
	authorization := req.Header.Get("Authorization")
	_, wireSecret, _ := strings.Cut(authorization, " ")
	secrets := append(append([]string{}, c.ledger.Secrets...), authorization, wireSecret, req.URL.Query().Get("apikey"))
	providerhttp.Response(&receipt, response, secrets...)
	receipt.Category = providerhttp.HTTPCategory(response.StatusCode)
	body := response.Body
	var reader io.Reader = body
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		receipt.Code = "http_" + strconv.Itoa(response.StatusCode)
		receipt.Retryable = providerhttp.IsTransient(response.StatusCode)
		prefix, _ := io.ReadAll(io.LimitReader(body, 64<<10))
		providerhttp.ErrorDetails(&receipt, prefix, secrets...)
		reader = io.MultiReader(bytes.NewReader(prefix), body)
	}
	response.Body = &measuredBody{reader: reader, body: body, done: func(readErr error) {
		if problem := providerhttp.OperationOutcome(req.Context()); problem != nil {
			receipt.Code, receipt.Retryable = problem.Code, problem.Retry
			switch {
			case strings.Contains(problem.Code, "quota"):
				receipt.Category = "rate_limit"
			case response.StatusCode >= 200 && response.StatusCode < 300:
				receipt.Category = "response"
			}
		} else if readErr != nil && response.StatusCode >= 200 && response.StatusCode < 300 {
			receipt.Category, receipt.Code, receipt.Retryable = "transport", "execution_response_read", true
		}
		settle()
	}}
	return response, nil
}

type measuredBody struct {
	reader io.Reader
	body   io.ReadCloser
	done   func(error)
	once   sync.Once
	mu     sync.Mutex
	err    error
}

func (b *measuredBody) Read(p []byte) (int, error) {
	n, err := b.reader.Read(p)
	if err != nil && err != io.EOF {
		b.mu.Lock()
		b.err = err
		b.mu.Unlock()
	}
	return n, err
}

func (b *measuredBody) Close() error {
	err := b.body.Close()
	b.once.Do(func() {
		b.mu.Lock()
		readErr := b.err
		b.mu.Unlock()
		b.done(readErr)
	})
	return err
}

func operation(req *http.Request, provider string) string {
	switch req.URL.Hostname() {
	case "oauth2.googleapis.com":
		return "oauth_token"
	case "analyticsadmin.googleapis.com":
		return "ga4_account_summaries"
	case "analyticsdata.googleapis.com":
		return "ga4_run_report"
	case "searchconsole.googleapis.com":
		return "gsc_url_inspection"
	case "www.googleapis.com":
		switch {
		case strings.HasSuffix(req.URL.Path, "/searchAnalytics/query"):
			return "gsc_search_analytics"
		case strings.HasSuffix(req.URL.Path, "/sitemaps"):
			return "gsc_sitemaps"
		default:
			return "gsc_sites"
		}
	case "ssl.bing.com":
		switch method := path.Base(req.URL.Path); method {
		case "GetUserSites", "GetFeeds", "GetRankAndTrafficStats", "GetQueryStats", "GetPageStats", "GetUrlInfo", "SubmitUrlBatch":
			return method
		}
		return "bing_webmaster"
	case "api.indexnow.org":
		return "indexnow_submit"
	}
	if provider == domain.ExecutionIndexNow {
		return "indexnow_ownership"
	}
	return req.Method
}

// Client applies the same request accounting to provider and authorized site
// requests. Injected transports are used by isolated HTTP fixtures.
func Client(fixture *http.Client, timeout time.Duration, ledger Ledger) *http.Client {
	c := contentinventory.NewPublicHTTPClient(timeout)
	c.Transport.(*http.Transport).ResponseHeaderTimeout = 0
	if fixture != nil && fixture.Transport != nil {
		c.Transport = fixture.Transport
	}
	c.Transport = countedTransport{transport: c.Transport, ledger: ledger}
	return c
}
