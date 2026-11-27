// Package executionsearch translates the official search engine and analytics
// HTTP APIs into site-scoped observations for the execution worker.
package executionsearch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/Autumn-27/artex/seoos/internal/domain"
	"github.com/Autumn-27/artex/seoos/internal/providerhttp"
	"github.com/coreos/go-oidc/v3/oidc"
)

const (
	googleWebmastersURL = "https://www.googleapis.com/webmasters/v3"
	googleInspectionURL = "https://searchconsole.googleapis.com/v1/urlInspection/index:inspect"
	googleTokenURL      = "https://oauth2.googleapis.com/token"
	gaAdminURL          = "https://analyticsadmin.googleapis.com/v1beta"
	gaDataURL           = "https://analyticsdata.googleapis.com/v1beta"
	bingAPIURL          = "https://ssl.bing.com/webmaster/api.svc/json"
	indexNowURL         = "https://api.indexnow.org/indexnow"

	maxResponseBytes = 4 << 20
	reportPageSize   = 1000
	maxSyncRequests  = 32
)

var errRequestLimit = errors.New("search report request limit")

type Client struct {
	http       *http.Client
	googleKeys oidc.KeySet
}

// New refuses redirects. The optional client can supply a custom transport
// for fixtures or metering without changing the upstream API origins.
func New(client *http.Client) *Client {
	var copy http.Client
	if client != nil {
		copy = *client
	}
	if copy.Transport == nil {
		copy.Transport = &http.Transport{
			ForceAttemptHTTP2: true,
			MaxIdleConns:      20,
			IdleConnTimeout:   90 * time.Second,
		}
	}
	copy.Jar = nil
	copy.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{http: &copy, googleKeys: googleIdentityKeys(&copy)}
}

func failure(code string) error { return &domain.DataError{Code: code} }

type report struct {
	client *Client
	ctx    context.Context
	config domain.ExecutionConnection
	token  string
	limit  int
	output domain.ExecutionSync
	rows   map[metricDimensions]struct{}
}

type metricDimensions struct {
	day, page, query, country, device, language string
}

func (s *report) addMetric(row domain.ExecutionMetric) {
	key := metricDimensions{row.Date, row.Page, row.Query, row.Country, row.Device, row.Language}
	if _, exists := s.rows[key]; exists {
		s.output.Truncated = true
		return
	}
	if s.rows == nil {
		s.rows = make(map[metricDimensions]struct{})
	}
	s.rows[key] = struct{}{}
	s.output.Metrics = append(s.output.Metrics, row)
}

// Sync reserves at most maxRequests API calls for one reporting operation.
// An empty property or an empty date window lists properties without a report.
func (c *Client) Sync(ctx context.Context, config domain.ExecutionConnection, secret domain.ExecutionCredential, from, to string, maxRequests int) (domain.ExecutionSync, error) {
	state := report{
		client: c,
		ctx:    ctx,
		config: config,
		token:  secret.AccessToken,
		limit:  min(maxRequests, maxSyncRequests),
		output: domain.ExecutionSync{
			Properties: []domain.ExecutionProperty{},
			Metrics:    []domain.ExecutionMetric{},
			Sitemaps:   []domain.ExecutionSitemap{},
			Indexes:    []domain.ExecutionIndexRecord{},
		},
	}
	if !config.Enabled {
		return state.output, domain.ErrExecutionAuthority
	}
	if _, err := siteURL(config.BaseURL); err != nil {
		return state.output, err
	}
	if !validWindow(from, to) {
		return state.output, failure("execution_invalid_date_window")
	}
	var err error
	switch config.Kind {
	case domain.ExecutionGSC:
		if !validSecret(state.token) {
			return state.output, failure("execution_google_token_required")
		}
		err = state.gsc(from, to)
	case domain.ExecutionGA4:
		if !validSecret(state.token) {
			return state.output, failure("execution_google_token_required")
		}
		err = state.ga4(from, to)
	case domain.ExecutionBing:
		if !hasBingCredential(secret) {
			return state.output, failure("execution_bing_credential_required")
		}
		err = state.bing(secret.Secret, from, to)
	default:
		err = domain.ErrExecutionUnsupported
	}
	if errors.Is(err, errRequestLimit) {
		state.output.Truncated = true
		err = nil
	}
	return state.output, err
}

func (s *report) request(method, endpoint string, body any, out any) error {
	if s.output.Requests >= s.limit {
		return errRequestLimit
	}
	s.output.Requests++
	_, err := s.client.requestJSON(s.ctx, s.config, method, endpoint, s.token, body, out)
	var problem *domain.DataError
	if errors.As(err, &problem) && problem.Code == "execution_budget" {
		s.output.Requests--
		// Earlier calls, including OAuth, may already occupy the rolling window.
		// Save partial data and release the job instead of replaying earlier pages.
		if s.output.Requests > 0 {
			return errRequestLimit
		}
	}
	return err
}

func (c *Client) requestJSON(ctx context.Context, config domain.ExecutionConnection, method, endpoint, bearer string, input, output any) (int, error) {
	var payload []byte
	if input != nil {
		var err error
		payload, err = json.Marshal(input)
		if err != nil {
			return 0, failure("execution_invalid_upstream_request")
		}
	}
	return c.request(ctx, config, method, endpoint, bearer, "application/json", payload, output)
}

func (c *Client) request(ctx context.Context, config domain.ExecutionConnection, method, endpoint, bearer, mediaType string, payload []byte, output any) (status int, problem error) {
	target, err := url.Parse(endpoint)
	if err != nil || target.Scheme != "https" || target.User != nil || target.Fragment != "" || !officialHost(target.Host) {
		return 0, failure("execution_invalid_upstream_endpoint")
	}

	timeout := config.TimeoutSeconds
	if timeout <= 0 {
		timeout = 10
	}
	reqCtx, cancel := context.WithTimeout(ctx, time.Duration(min(timeout, 30))*time.Second)
	defer cancel()
	reqCtx, observation := providerhttp.Observe(reqCtx)
	var body io.Reader
	if payload != nil {
		body = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(reqCtx, method, endpoint, body)
	if err != nil {
		return 0, failure("execution_invalid_upstream_request")
	}
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", mediaType)
	}
	if bearer != "" {
		if !validSecret(bearer) {
			return 0, failure("execution_invalid_upstream_credential")
		}
		req.Header.Set("Authorization", "Bearer "+bearer)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		var typed *domain.DataError
		if errors.As(err, &typed) {
			return 0, typed
		}
		if reqCtx.Err() != nil {
			return 0, &domain.DataError{Code: "execution_upstream_timeout", Retry: ctx.Err() == nil}
		}
		// net/url.Error can print the Bing query string containing its API key.
		return 0, &domain.DataError{Code: "execution_upstream_transport", Retry: true}
	}
	defer func() {
		observation.Set(problem)
		_ = resp.Body.Close()
	}()
	status = resp.StatusCode
	if status < 200 || status >= 300 {
		if status == http.StatusBadRequest && (endpoint == googleTokenURL || endpoint == bingOAuthURL+"token") {
			var tokenError struct {
				Code string `json:"error"`
			}
			if json.NewDecoder(io.LimitReader(resp.Body, 8192)).Decode(&tokenError) == nil && tokenError.Code == "invalid_grant" {
				return status, failure("integration_invalid_grant")
			}
			return status, failure("integration_oauth_rejected")
		}
		return status, responseError(resp, target.Host)
	}
	if output == nil {
		return status, nil
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return status, &domain.DataError{Code: "execution_upstream_read", Retry: true}
	}
	if len(data) > maxResponseBytes {
		return status, failure("execution_upstream_response_too_large")
	}
	if err := json.Unmarshal(data, output); err != nil {
		return status, failure("execution_invalid_upstream_response")
	}
	return status, nil
}

func responseError(resp *http.Response, host string) error {
	status := resp.StatusCode
	delay := retryAfter(resp.Header.Get("Retry-After"))
	switch status {
	case http.StatusBadRequest:
		if host == bingHost || host == "www.bing.com" {
			return bingAPIError(resp.Body, delay)
		}
	case http.StatusUnauthorized:
		return failure("execution_upstream_unauthorized")
	case http.StatusForbidden:
		var g struct {
			Error struct {
				Status string `json:"status"`
				Errors []struct {
					Reason string `json:"reason"`
				} `json:"errors"`
			} `json:"error"`
		}
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 32<<10))
		if json.Unmarshal(data, &g) == nil {
			if g.Error.Status == "RESOURCE_EXHAUSTED" {
				return quotaError(delay)
			}
			for _, item := range g.Error.Errors {
				switch item.Reason {
				case "rateLimitExceeded", "userRateLimitExceeded", "quotaExceeded", "dailyLimitExceeded":
					return quotaError(delay)
				}
			}
		}
		return failure("execution_upstream_forbidden")
	case http.StatusNotFound:
		return failure("execution_upstream_not_found")
	case http.StatusTooManyRequests:
		return quotaError(delay)
	case http.StatusRequestTimeout:
		return &domain.DataError{Code: "execution_upstream_timeout", Retry: true, RetryAfter: delay}
	}
	if status >= 500 && status < 600 {
		return &domain.DataError{Code: "execution_upstream_unavailable", Retry: true, RetryAfter: delay}
	}
	return failure("execution_upstream_rejected")
}

func quotaError(delay time.Duration) error {
	return &domain.DataError{Code: "execution_upstream_quota", Retry: true, RetryAfter: delay}
}

func retryAfter(value string) time.Duration {
	const limit = 24 * time.Hour
	if seconds, err := strconv.Atoi(value); err == nil && seconds >= 0 {
		return time.Duration(min(seconds, int(limit/time.Second))) * time.Second
	}
	if date, err := http.ParseTime(value); err == nil {
		return min(max(0, time.Until(date)), limit)
	}
	return 0
}

func officialHost(host string) bool {
	switch host {
	case "www.googleapis.com", "searchconsole.googleapis.com", "oauth2.googleapis.com",
		"analyticsadmin.googleapis.com", "analyticsdata.googleapis.com", "ssl.bing.com", "www.bing.com", "api.indexnow.org":
		return true
	}
	return false
}

func validSecret(value string) bool {
	if len(value) == 0 || len(value) > 8192 {
		return false
	}
	for _, ch := range value {
		if ch < 0x21 || ch > 0x7e {
			return false
		}
	}
	return true
}

func validWindow(from, to string) bool {
	if from == "" && to == "" {
		return true
	}
	start, startErr := time.Parse(time.DateOnly, from)
	end, endErr := time.Parse(time.DateOnly, to)
	return startErr == nil && endErr == nil && !start.After(end)
}

func siteURL(raw string) (*url.URL, error) {
	base, err := domain.EvidenceURL(raw)
	if err != nil || base.RawQuery != "" || base.ForceQuery {
		return nil, failure("execution_invalid_site_url")
	}
	return base, nil
}

func targetWithin(base *url.URL, raw string) bool {
	target, err := domain.EvidenceURL(raw)
	if err != nil || target.Scheme != base.Scheme || urlHost(target) != urlHost(base) {
		return false
	}
	cleanBase := path.Clean("/" + strings.TrimPrefix(base.Path, "/"))
	cleanTarget := path.Clean("/" + strings.TrimPrefix(target.Path, "/"))
	return cleanBase == "/" || cleanTarget == cleanBase || strings.HasPrefix(cleanTarget, cleanBase+"/")
}

func urlHost(u *url.URL) string {
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	port := u.Port()
	if port == "" || (u.Scheme == "https" && port == "443") || (u.Scheme == "http" && port == "80") {
		return host
	}
	return host + ":" + port
}
