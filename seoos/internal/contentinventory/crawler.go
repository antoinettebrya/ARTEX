// Package contentinventory reads public pages and sources within an explicit
// site owner's authorization. The crawler makes no CMS writes.
package contentinventory

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/Autumn-27/artex/seoos/internal/domain"
)

const (
	defaultPageLimit = 100
	maxPageLimit     = 1000
	maxSourceOrigins = 16
	requestInterval  = 250 * time.Millisecond
	requestTimeout   = 8 * time.Second
	maxPageBytes     = 2 << 20
	maxListingBytes  = 5 << 20
)

type Crawler struct {
	client *http.Client
	mu     sync.Mutex
	next   map[string]time.Time
}

// New creates a bounded HTTP reader. A client with a custom RoundTripper can
// be injected by tests. Production callers use the pinned public transport.
func New(client *http.Client) *Crawler {
	value := http.Client{Timeout: requestTimeout}
	if client != nil {
		value = *client
		if value.Timeout <= 0 || value.Timeout > requestTimeout {
			value.Timeout = requestTimeout
		}
	}
	if value.Transport == nil {
		value.Transport = NewPublicHTTPClient(requestTimeout).Transport
	}
	value.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	value.Jar = nil
	return &Crawler{client: &value, next: make(map[string]time.Time)}
}

// NewPublicHTTPClient pins public DNS answers, keeps credentials on the
// original host, and uses a bounded timeout. It never follows redirects.
func NewPublicHTTPClient(timeout time.Duration) *http.Client {
	if timeout <= 0 || timeout > 30*time.Second {
		timeout = requestTimeout
	}
	return &http.Client{Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}, Transport: &http.Transport{
		DialContext:            dialPublic,
		ForceAttemptHTTP2:      true,
		MaxIdleConns:           32,
		MaxIdleConnsPerHost:    2,
		IdleConnTimeout:        30 * time.Second,
		TLSHandshakeTimeout:    4 * time.Second,
		ResponseHeaderTimeout:  5 * time.Second,
		MaxResponseHeaderBytes: 64 << 10,
	}}
}

// ValidatePolicy normalizes an owner's read authorization against its current
// registered site. It makes no network request.
func ValidatePolicy(site domain.Site, policy domain.CrawlPolicy) (domain.CrawlPolicy, error) {
	if policy.SiteID != "" && policy.SiteID != site.ID {
		return domain.CrawlPolicy{}, invalid("site_id", "must match the registered site")
	}
	policy.SiteID = site.ID
	base, err := publicURL(site.BaseURL)
	if err != nil {
		return domain.CrawlPolicy{}, invalid("base_url", "must identify a public HTTP(S) site")
	}
	policy.Connector = strings.ToLower(strings.TrimSpace(policy.Connector))
	if policy.Connector == "" {
		policy.Connector = "sitemap"
	}
	if policy.Connector != "sitemap" && policy.Connector != "wordpress" && policy.Connector != "json" {
		return domain.CrawlPolicy{}, invalid("connector", "must be sitemap, wordpress or json")
	}
	if policy.MaxPages == 0 {
		policy.MaxPages = defaultPageLimit
	}
	if policy.MaxPages < 1 || policy.MaxPages > maxPageLimit {
		return domain.CrawlPolicy{}, invalid("max_pages", "must be between 1 and 1000")
	}
	if policy.SitemapURL != "" {
		sitemap, err := scopedURL(base, policy.SitemapURL)
		if err != nil {
			return domain.CrawlPolicy{}, invalid("sitemap_url", "must be on the site's authorized origin and path")
		}
		policy.SitemapURL = sitemap.String()
	}
	if policy.ConnectorURL != "" {
		api, err := scopedURL(base, policy.ConnectorURL)
		if err != nil || (policy.Connector == "wordpress" && api.RawQuery != "") {
			return domain.CrawlPolicy{}, invalid("connector_url", "must be a public endpoint within the site's path")
		}
		policy.ConnectorURL = api.String()
	}
	if policy.Enabled && policy.Connector == "json" && policy.ConnectorURL == "" {
		return domain.CrawlPolicy{}, invalid("connector_url", "is required for the JSON connector")
	}
	if len(policy.SourceOrigins) > maxSourceOrigins {
		return domain.CrawlPolicy{}, invalid("source_origins", "must contain at most 16 public origins")
	}
	origins := make([]string, 0, len(policy.SourceOrigins))
	seen := make(map[string]bool, len(policy.SourceOrigins))
	for _, raw := range policy.SourceOrigins {
		u, err := publicURL(raw)
		if err != nil || u.RawQuery != "" || (u.Path != "" && u.Path != "/") {
			return domain.CrawlPolicy{}, invalid("source_origins", "must list exact public origins without paths")
		}
		key := origin(u)
		if !seen[key] {
			origins = append(origins, key)
			seen[key] = true
		}
	}
	policy.SourceOrigins = origins
	return policy, nil
}

// ValidatePageURL checks a page destination without fetching it.
func ValidatePageURL(site domain.Site, policy domain.CrawlPolicy, target string) (string, error) {
	if !policy.Enabled || policy.SiteID != site.ID {
		return "", domain.ErrCrawlUnauthorized
	}
	base, err := publicURL(site.BaseURL)
	if err != nil {
		return "", domain.ErrCrawlUnauthorized
	}
	page, err := scopedURL(base, target)
	if err != nil {
		return "", err
	}
	return page.String(), nil
}

// ValidateSource checks an explicitly approved first-party public source.
func ValidateSource(policy domain.CrawlPolicy, source domain.ContentSource) (domain.ContentSource, error) {
	if !policy.Enabled {
		return domain.ContentSource{}, domain.ErrCrawlUnauthorized
	}
	source.Kind = strings.ToLower(strings.TrimSpace(source.Kind))
	switch source.Kind {
	case "official", "government", "primary", "authorized":
	default:
		return domain.ContentSource{}, invalid("kind", "must designate an official, government, primary or authorized source")
	}
	u, err := publicURL(source.URL)
	if err != nil {
		return domain.ContentSource{}, invalid("url", "must identify a public source")
	}
	for _, allowed := range policy.SourceOrigins {
		if origin(u) == allowed {
			source.URL = u.String()
			return source, nil
		}
	}
	return domain.ContentSource{}, domain.ErrCrawlUnauthorized
}

func origin(u *url.URL) string { return u.Scheme + "://" + u.Host }

func scopedURL(base *url.URL, raw string) (*url.URL, error) {
	u, err := publicURL(raw)
	if err != nil || !domain.BelongsToSite(base.String(), u) {
		return nil, domain.ErrCrawlUnauthorized
	}
	return u, nil
}

func publicURL(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	u, err := domain.EvidenceURL(raw)
	if err != nil || strings.Contains(raw, "#") || u.ForceQuery {
		return nil, domain.ErrCrawlUnauthorized
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if !validHost(host) {
		return nil, domain.ErrCrawlUnauthorized
	}
	port := u.Port()
	if port != "" && port != defaultPort(u.Scheme) {
		return nil, domain.ErrCrawlUnauthorized
	}
	// Reject ambiguous path separators and traversal before checking the
	// authorized path. The escaped form is kept for the real request.
	escaped := strings.ToLower(u.EscapedPath())
	if strings.ContainsAny(u.Path, "\\%;") || strings.Contains(escaped, "%2f") ||
		strings.IndexFunc(u.Path, unicode.IsControl) >= 0 {
		return nil, domain.ErrCrawlUnauthorized
	}
	for _, segment := range strings.Split(u.Path, "/") {
		if segment == "." || segment == ".." {
			return nil, domain.ErrCrawlUnauthorized
		}
	}
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return nil, domain.ErrCrawlUnauthorized
	}
	for key, values := range query {
		if strings.IndexFunc(key, unicode.IsControl) >= 0 || credentialParameter(key) {
			return nil, domain.ErrCrawlUnauthorized
		}
		for _, value := range values {
			if strings.IndexFunc(value, unicode.IsControl) >= 0 {
				return nil, domain.ErrCrawlUnauthorized
			}
		}
	}
	u.Host = host
	if strings.Contains(host, ":") {
		u.Host = "[" + host + "]"
	}
	return u, nil
}

func validHost(host string) bool {
	if address, err := netip.ParseAddr(host); err == nil {
		return publicAddress(address)
	}
	if len(host) > 253 || !strings.Contains(host, ".") {
		return false
	}
	for _, privateSuffix := range []string{".localhost", ".local", ".internal", ".home.arpa"} {
		if strings.HasSuffix(host, privateSuffix) {
			return false
		}
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) < 1 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		if strings.IndexFunc(label, func(r rune) bool {
			return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-')
		}) >= 0 {
			return false
		}
	}
	return true
}

func credentialParameter(key string) bool {
	switch strings.ToLower(key) {
	case "access_token", "api-key", "api_key", "apikey", "authorization", "client_secret",
		"credential", "key", "password", "secret", "signature", "token", "x-amz-credential", "x-amz-signature":
		return true
	}
	return false
}

func defaultPort(scheme string) string {
	if scheme == "https" {
		return "443"
	}
	return "80"
}

var nonPublic = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"), netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"), netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"), netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"), netip.MustParsePrefix("2001::/23"),
	netip.MustParsePrefix("2001:db8::/32"), netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("3fff::/20"),
}

var globalIPv6 = netip.MustParsePrefix("2000::/3")

func publicAddress(addr netip.Addr) bool {
	if addr.Zone() != "" {
		return false
	}
	addr = addr.Unmap()
	if !addr.IsGlobalUnicast() || addr.IsPrivate() || (addr.Is6() && !globalIPv6.Contains(addr)) {
		return false
	}
	for _, blocked := range nonPublic {
		if blocked.Contains(addr) {
			return false
		}
	}
	return true
}

type addressLookup interface {
	LookupNetIP(context.Context, string, string) ([]netip.Addr, error)
}

func publicAddresses(ctx context.Context, resolver addressLookup, host string) ([]netip.Addr, error) {
	if literal, err := netip.ParseAddr(host); err == nil {
		if !publicAddress(literal) {
			return nil, domain.ErrCrawlUnauthorized
		}
		return []netip.Addr{literal}, nil
	}
	addrs, err := resolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, err
	}
	if len(addrs) == 0 {
		return nil, errors.New("destination has no addresses")
	}
	for _, addr := range addrs {
		if !publicAddress(addr) {
			return nil, domain.ErrCrawlUnauthorized
		}
	}
	return addrs, nil
}

func dialPublic(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil || !validHost(host) || (port != "80" && port != "443") {
		return nil, domain.ErrCrawlUnauthorized
	}
	addrs, err := publicAddresses(ctx, net.DefaultResolver, host)
	if err != nil {
		return nil, err
	}
	var conn net.Conn
	dialer := net.Dialer{Timeout: 2 * time.Second, KeepAlive: 15 * time.Second}
	for _, addr := range addrs {
		conn, err = dialer.DialContext(ctx, network, net.JoinHostPort(addr.String(), port))
		if err == nil {
			return conn, nil
		}
		if ctx.Err() != nil {
			break
		}
	}
	return nil, err
}

func (c *Crawler) wait(ctx context.Context, target string) error {
	for ctx.Err() == nil {
		c.mu.Lock()
		now := time.Now()
		delay := c.next[target].Sub(now)
		if delay <= 0 {
			if len(c.next) >= 128 {
				for host, until := range c.next {
					if !until.After(now) {
						delete(c.next, host)
					}
				}
			}
			c.next[target] = now.Add(requestInterval)
		}
		c.mu.Unlock()
		if delay <= 0 {
			return nil
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
		case <-timer.C:
		}
	}
	return ctx.Err()
}

func (c *Crawler) get(ctx context.Context, target *url.URL, headers http.Header) (*http.Response, error) {
	if err := c.wait(ctx, origin(target)); err != nil {
		return nil, &domain.DataError{Code: "inventory_http_timeout", Retry: true}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, domain.ErrCrawlUnauthorized
	}
	for key, values := range headers {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
	req.Header.Set("User-Agent", "ARTEXSEOOS/1.0")
	res, err := c.client.Do(req)
	if err != nil {
		if errors.Is(err, domain.ErrCrawlUnauthorized) {
			return nil, domain.ErrCrawlUnauthorized
		}
		return nil, &domain.DataError{Code: "inventory_http_unavailable", Retry: true}
	}
	return res, nil
}

func readBounded(r io.Reader, maximum int64) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(r, maximum+1))
	if err != nil {
		return nil, &domain.DataError{Code: "inventory_http_read_failed", Retry: true}
	}
	if int64(len(body)) > maximum {
		return nil, &domain.DataError{Code: "inventory_body_limit"}
	}
	return body, nil
}

func transientStatus(code int) bool {
	return code == 408 || code == 425 || code == 429 || code >= 500 && code <= 599
}

func responseError(res *http.Response) *domain.DataError {
	delay := time.Duration(0)
	retry := transientStatus(res.StatusCode)
	if retry {
		raw := strings.TrimSpace(res.Header.Get("Retry-After"))
		if seconds, err := strconv.ParseInt(raw, 10, 64); err == nil && seconds > 0 {
			delay = time.Duration(min(seconds, 3600)) * time.Second
		} else if until, err := http.ParseTime(raw); err == nil {
			delay = max(0, min(time.Until(until), time.Hour))
		}
	}
	return &domain.DataError{Code: "inventory_http_" + strconv.Itoa(res.StatusCode), Retry: retry, RetryAfter: delay}
}

func invalid(field, message string) *domain.ValidationError {
	return &domain.ValidationError{Field: field, Message: message}
}
