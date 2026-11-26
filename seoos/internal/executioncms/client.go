// Package executioncms implements approved-site CMS REST operations. The
// execution worker owns authorization, durable retries, and version snapshots.
package executioncms

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/Autumn-27/artex/seoos/internal/contentinventory"
	"github.com/Autumn-27/artex/seoos/internal/domain"
)

const (
	maxResponseBytes = 4 << 20
	maxMediaBytes    = 8 << 20
	maxPageBody      = 250_000
	maxListing       = 250
)

type client struct {
	connection domain.ExecutionConnection
	credential domain.ExecutionCredential
	site       domain.Site
	base       *url.URL
	http       *http.Client
}

// New binds a CMS adapter to the approved HTTPS site and its dedicated
// credential. The optional transport is used by the execution request ledger
// and isolated HTTP fixtures.
func New(conn domain.ExecutionConnection, secret domain.ExecutionCredential, injected *http.Client) (domain.CMSConnector, error) {
	if !domain.IsCMSConnection(conn.Kind) {
		return nil, domain.ErrExecutionUnsupported
	}
	if !conn.Enabled {
		return nil, domain.ErrExecutionAuthority
	}
	site := domain.Site{ID: conn.SiteID, BaseURL: conn.BaseURL}
	base, err := domain.EvidenceURL(conn.BaseURL)
	if err != nil || base.Scheme != "https" || base.RawQuery != "" || base.ForceQuery {
		return nil, invalid("base_url", "must be the approved HTTPS site root")
	}
	policy := domain.CrawlPolicy{SiteID: site.ID, Enabled: true}
	clean, err := contentinventory.ValidatePageURL(site, policy, base.String())
	if err != nil {
		return nil, domain.ErrExecutionAuthority
	}
	base, err = url.Parse(clean)
	if err != nil {
		return nil, domain.ErrExecutionAuthority
	}
	if !validCredential(secret.Secret, 1024) ||
		conn.Kind == domain.ExecutionWordPress && (!validCredential(secret.Username, 1024) || strings.Contains(secret.Username, ":")) {
		return nil, invalid("credential", "supply a valid CMS credential")
	}
	if conn.Kind == domain.ExecutionGeneric && strings.ContainsFunc(secret.Secret, unicode.IsSpace) {
		return nil, invalid("credential", "the generic CMS requires a bearer token")
	}
	timeout := time.Duration(conn.TimeoutSeconds) * time.Second
	if timeout == 0 {
		timeout = 8 * time.Second
	}
	if timeout < time.Second || timeout > 30*time.Second {
		return nil, invalid("timeout_seconds", "use a timeout from 1 to 30 seconds")
	}
	bounded := contentinventory.NewPublicHTTPClient(timeout)
	if injected != nil && injected.Transport != nil {
		bounded.Transport = injected.Transport
		if injected.Timeout > 0 {
			bounded.Timeout = min(timeout, injected.Timeout)
		}
	}
	bounded.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	bounded.Jar = nil
	c := &client{connection: conn, credential: secret, site: site, base: base, http: bounded}
	if conn.Kind == domain.ExecutionWordPress {
		return &wordpress{client: c}, nil
	}
	return &generic{client: c}, nil
}

func validCredential(s string, maximum int) bool {
	return len(s) <= maximum && strings.TrimSpace(s) == s && s != "" &&
		strings.IndexFunc(s, unicode.IsControl) < 0
}

func invalid(field, message string) *domain.ValidationError {
	return &domain.ValidationError{Field: field, Message: message}
}

func (c *client) endpoint(parts ...string) *url.URL {
	u := *c.base
	u.Path = strings.TrimSuffix(c.base.Path, "/") + "/" + path.Join(parts...)
	u.RawPath = ""
	return &u
}

func (c *client) approved(raw string) (*url.URL, error) {
	policy := domain.CrawlPolicy{SiteID: c.site.ID, Enabled: true}
	value, err := contentinventory.ValidatePageURL(c.site, policy, raw)
	if err != nil {
		return nil, domain.ErrExecutionAuthority
	}
	return url.Parse(value)
}

type httpResult struct {
	body   []byte
	header http.Header
	status int
}

func (c *client) request(ctx context.Context, method string, target *url.URL, auth bool, data []byte, headers http.Header) (httpResult, error) {
	u, err := c.approved(target.String())
	if err != nil {
		return httpResult{}, err
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), bytes.NewReader(data))
	if err != nil {
		return httpResult{}, domain.ErrExecutionAuthority
	}
	for name, values := range headers {
		for _, value := range values {
			req.Header.Add(name, value)
		}
	}
	req.Header.Set("User-Agent", "ARTEXSEOOS/1.0")
	var authorization string
	if auth && c.connection.Kind == domain.ExecutionWordPress {
		req.SetBasicAuth(c.credential.Username, c.credential.Secret)
		authorization = req.Header.Get("Authorization")
	} else if auth {
		authorization = "Bearer " + c.credential.Secret
		req.Header.Set("Authorization", authorization)
	}
	res, err := c.http.Do(req)
	if err != nil {
		var outcome *domain.DataError
		if errors.As(err, &outcome) {
			return httpResult{}, outcome
		}
		if errors.Is(err, domain.ErrCrawlUnauthorized) || errors.Is(err, domain.ErrExecutionAuthority) {
			return httpResult{}, domain.ErrExecutionAuthority
		}
		return httpResult{}, &domain.DataError{Code: "cms_transport", Retry: true}
	}
	defer res.Body.Close()
	result := httpResult{header: res.Header, status: res.StatusCode}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return result, statusError(res.StatusCode, res.Header)
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, maxResponseBytes+1))
	if err != nil {
		return result, &domain.DataError{Code: "cms_response_read", Retry: true}
	}
	if len(body) > maxResponseBytes {
		return result, &domain.DataError{Code: "cms_response_limit"}
	}
	if c.reflectedCredential(body, authorization) {
		return result, &domain.DataError{Code: "cms_sensitive_response"}
	}
	result.body = body
	return result, nil
}

func (c *client) reflectedCredential(body []byte, auth string) bool {
	secret := c.credential.Secret
	if secret == "" {
		return false
	}
	if bytes.Contains(body, []byte(secret)) {
		return true
	}
	if auth != "" && bytes.Contains(body, []byte(auth)) {
		return true
	}
	if c.connection.Kind == domain.ExecutionWordPress {
		compact := strings.ReplaceAll(secret, " ", "")
		basic := base64.StdEncoding.EncodeToString([]byte(c.credential.Username + ":" + secret))
		return bytes.Contains(body, []byte(compact)) || bytes.Contains(body, []byte(basic))
	}
	return false
}

func statusError(status int, headers http.Header) *domain.DataError {
	retry := status == 408 || status == 425 || status == 429 || status >= 500 && status < 600
	knownRejected := status >= 400 && status < 500 && status != 408
	return &domain.DataError{
		Code:          "cms_http_" + strconv.Itoa(status),
		Retry:         retry,
		RetryAfter:    retryAfter(headers.Get("Retry-After")),
		KnownUnbilled: knownRejected,
	}
}

func retryAfter(raw string) time.Duration {
	if seconds, err := strconv.ParseInt(raw, 10, 32); err == nil && seconds > 0 {
		return time.Duration(min(seconds, 3600)) * time.Second
	}
	if until, err := http.ParseTime(raw); err == nil {
		return max(0, min(time.Until(until), time.Hour))
	}
	return 0
}

func (c *client) readJSON(ctx context.Context, method string, target *url.URL, value any, into any) (httpResult, error) {
	headers := http.Header{"Accept": {"application/json"}}
	var payload []byte
	if value != nil {
		var err error
		payload, err = json.Marshal(value)
		if err != nil {
			return httpResult{}, invalid("body", "must be valid JSON")
		}
		headers.Set("Content-Type", "application/json")
	}
	result, err := c.request(ctx, method, target, true, payload, headers)
	if err != nil {
		return result, err
	}
	if err := json.Unmarshal(result.body, into); err != nil {
		return result, &domain.DataError{Code: "cms_invalid_response"}
	}
	return result, nil
}

func (c *client) allowWrites() error {
	if !c.connection.AllowWrites {
		return domain.ErrExecutionAuthority
	}
	return nil
}

func isNotFound(err error) bool {
	var e *domain.DataError
	return errors.As(err, &e) && e.Code == "cms_http_404"
}

func resourceKind(kind string) (string, error) {
	switch kind {
	case "post":
		return "posts", nil
	case "page":
		return "pages", nil
	default:
		return "", invalid("resource", "choose post or page")
	}
}

var remoteIDPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,119}$`)

func validRemoteID(raw string) bool {
	return remoteIDPattern.MatchString(raw)
}

func checkPageInput(in domain.CMSPageInput) error {
	if _, err := resourceKind(in.Resource); err != nil {
		return err
	}
	if len(in.Slug) > 180 || strings.ContainsAny(in.Slug, "/\\?#%") ||
		in.Slug == "." || in.Slug == ".." || strings.ContainsFunc(in.Slug, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsControl(r)
	}) {
		return invalid("slug", "must be a single URL path segment")
	}
	if len(in.Title) > 2000 || len(in.SEOTitle) > 2000 || len(in.Body) > maxPageBody ||
		len(in.Excerpt) > 16000 || len(in.Description) > 8000 {
		return invalid("body", "exceeds the CMS document size limit")
	}
	if in.Canonical != "" {
		u, err := domain.EvidenceURL(in.Canonical)
		if err != nil || u.User != nil {
			return invalid("canonical", "must be an absolute HTTP(S) URL")
		}
	}
	switch in.Status {
	case "", "draft", "pending", "private", "publish", "future":
	default:
		return invalid("status", "choose a supported article state")
	}
	if in.Status == "future" && (in.ScheduledAt == nil || !in.ScheduledAt.After(time.Now())) {
		return invalid("scheduled_at", "must be a future timestamp")
	}
	if in.Status != "future" && in.ScheduledAt != nil {
		return invalid("scheduled_at", "requires a scheduled publication")
	}
	return nil
}

func publishInput(in domain.CMSPageInput) (domain.CMSPageInput, error) {
	status := "publish"
	if in.ScheduledAt != nil {
		status = "future"
	}
	if in.Status != "" && in.Status != status {
		return in, invalid("status", "must match the publication mode")
	}
	in.Status = status
	return in, checkPageInput(in)
}

func (c *client) upload(ctx context.Context, target *url.URL, filename, mediaType string, data []byte) (httpResult, error) {
	if err := c.allowWrites(); err != nil {
		return httpResult{}, err
	}
	if filename == "" || len(filename) > 180 || filename == "." || filename == ".." ||
		strings.ContainsAny(filename, "/\\") || strings.IndexFunc(filename, unicode.IsControl) >= 0 {
		return httpResult{}, invalid("filename", "must be a plain file name")
	}
	if len(data) == 0 || len(data) > maxMediaBytes {
		return httpResult{}, invalid("media", "must be between 1 byte and 8 MiB")
	}
	switch mediaType {
	case "image/png", "image/jpeg", "image/gif", "image/webp", "application/pdf":
	default:
		return httpResult{}, domain.ErrExecutionUnsupported
	}
	if http.DetectContentType(data) != mediaType {
		return httpResult{}, invalid("media_type", "does not match the file bytes")
	}
	headers := http.Header{"Accept": {"application/json"}, "Content-Type": {mediaType}}
	headers.Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filename}))
	return c.request(ctx, http.MethodPost, target, true, data, headers)
}

func (c *client) sitemap(ctx context.Context, target *url.URL, auth bool) (string, error) {
	result, err := c.request(ctx, http.MethodGet, target, auth, nil,
		http.Header{"Accept": {"application/xml,text/xml;q=0.9"}})
	if err != nil {
		return "", err
	}
	var tree struct{ XMLName xml.Name }
	if xml.Unmarshal(result.body, &tree) != nil || tree.XMLName.Local != "sitemapindex" && tree.XMLName.Local != "urlset" {
		return "", &domain.DataError{Code: "cms_invalid_sitemap"}
	}
	return string(result.body), nil
}
