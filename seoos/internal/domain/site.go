// Package domain contains the SEO service's data contracts. It has no dependency
// on the legacy ARTEX application, agent runtime, tools, or database.
package domain

import (
	"context"
	"errors"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"golang.org/x/text/language"
)

var (
	ErrNotFound        = errors.New("site not found")
	ErrConflict        = errors.New("site URL already exists in this organization")
	ErrVersionConflict = errors.New("site was changed by another request")
)

type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string { return e.Field + ": " + e.Message }

type Site struct {
	ID             string    `json:"id"`
	OrganizationID string    `json:"organization_id"`
	Name           string    `json:"name"`
	BaseURL        string    `json:"base_url"`
	CMS            string    `json:"cms"`
	CountryCode    string    `json:"country_code"`
	LanguageTag    string    `json:"language_tag"`
	Status         string    `json:"status"`
	Version        int64     `json:"version"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// SiteInput is a full replacement of editable site settings. Organization and
// resource IDs are assigned by the service and cannot be supplied in this body.
type SiteInput struct {
	Name        string `json:"name"`
	BaseURL     string `json:"base_url"`
	CMS         string `json:"cms"`
	CountryCode string `json:"country_code"`
	LanguageTag string `json:"language_tag"`
	Status      string `json:"status"`
}

type Repository interface {
	Ping(context.Context) error
	CreateSite(context.Context, string, SiteInput) (Site, error)
	GetSite(context.Context, string, string) (Site, error)
	ListSites(context.Context, string, int, int) ([]Site, error)
	UpdateSite(context.Context, string, string, int64, SiteInput) (Site, error)
}

// ValidID deliberately accepts only canonical UUID strings, so identifiers have
// one representation in routes, configuration and persistence.
func ValidID(id string) bool {
	u, err := uuid.Parse(id)
	return err == nil && u != uuid.Nil && u.String() == id
}

func ValidatePage(limit, offset int) error {
	if limit < 1 || limit > 200 {
		return invalid("limit", "must be between 1 and 200")
	}
	if offset < 0 || offset > 100000 {
		return invalid("offset", "must be between 0 and 100000")
	}
	return nil
}

func NormalizeSiteInput(in SiteInput) (SiteInput, error) {
	in.Name = strings.TrimSpace(in.Name)
	if !utf8.ValidString(in.Name) || utf8.RuneCountInString(in.Name) < 1 || utf8.RuneCountInString(in.Name) > 120 || hasControl(in.Name) {
		return SiteInput{}, invalid("name", "must contain 1 to 120 characters without control characters")
	}
	canonical, err := normalizeURL(in.BaseURL)
	if err != nil {
		return SiteInput{}, err
	}
	in.BaseURL = canonical
	in.CMS = strings.ToLower(strings.TrimSpace(in.CMS))
	if in.CMS == "" {
		in.CMS = "wordpress"
	}
	if in.CMS != "wordpress" && in.CMS != "generic" {
		return SiteInput{}, invalid("cms", "must be wordpress or generic")
	}
	in.Status = strings.ToLower(strings.TrimSpace(in.Status))
	if in.Status == "" {
		in.Status = "active"
	}
	if in.Status != "active" && in.Status != "paused" && in.Status != "archived" {
		return SiteInput{}, invalid("status", "must be active, paused or archived")
	}
	in.CountryCode = strings.ToUpper(strings.TrimSpace(in.CountryCode))
	region, err := language.ParseRegion(in.CountryCode)
	if len(in.CountryCode) != 2 || err != nil || !region.IsCountry() {
		return SiteInput{}, invalid("country_code", "must be a two-letter country code")
	}
	lang := strings.TrimSpace(in.LanguageTag)
	tag, err := language.Parse(lang)
	if lang == "" || len(lang) > 64 || err != nil || tag == language.Und {
		return SiteInput{}, invalid("language_tag", "must be a valid language tag, such as en-US or zh-Hans")
	}
	in.LanguageTag = tag.String()
	return in, nil
}

// Registration stores an address; it never requests or resolves that address.
// Outbound CMS connectors will need their own destination authorization and
// network checks. URL syntax validation is not an SSRF boundary.
func normalizeURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	bad := func(message string) (string, error) { return "", invalid("base_url", message) }
	if len(raw) == 0 || len(raw) > 2048 || !utf8.ValidString(raw) || hasControl(raw) {
		return bad("must contain a valid HTTP(S) URL of at most 2048 bytes")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Opaque != "" || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return bad("must be an absolute HTTP(S) URL")
	}
	if u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(raw, "#") {
		return bad("must not include credentials, query parameters or fragments")
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if host == "" || strings.ContainsAny(host, " \\/%") {
		return bad("must include a valid hostname")
	}
	// Keep the first version's hostname representation deterministic. IDNs can be
	// registered using their ASCII (punycode) form until IDNA support is added.
	if ip := net.ParseIP(host); ip != nil {
		host = ip.String()
	} else {
		if len(host) > 253 {
			return bad("hostname is too long")
		}
		for _, label := range strings.Split(host, ".") {
			if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
				return bad("hostname must use valid ASCII DNS labels")
			}
			for _, c := range label {
				if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
					return bad("hostname must use valid ASCII DNS labels (punycode for international names)")
				}
			}
		}
	}
	port := u.Port()
	if port != "" {
		n, e := strconv.Atoi(port)
		if e != nil || n < 1 || n > 65535 {
			return bad("port must be between 1 and 65535")
		}
		port = strconv.Itoa(n)
	}
	if (u.Scheme == "https" && port == "443") || (u.Scheme == "http" && port == "80") {
		port = ""
	}
	if port != "" {
		u.Host = net.JoinHostPort(host, port)
	} else if strings.Contains(host, ":") {
		u.Host = "[" + host + "]"
	} else {
		u.Host = host
	}
	for _, segment := range strings.Split(u.Path, "/") {
		if segment == "." || segment == ".." {
			return bad("must not include relative path segments")
		}
	}
	if hasControl(u.Path) || strings.Contains(u.Path, "\\") {
		return bad("must not include control characters or backslashes")
	}
	// Subdirectory WordPress installs are supported; path casing is significant.
	u.RawPath = canonicalEscapedPath(strings.TrimRight(u.EscapedPath(), "/"))
	u.Path, _ = url.PathUnescape(u.RawPath) // EscapedPath produces valid escaping.
	canonical := u.String()
	if len(canonical) > 2048 {
		return bad("canonical URL must be at most 2048 bytes")
	}
	return canonical, nil
}

// Decode only unreserved characters: an escaped slash may identify a different
// resource and must not become a directory separator during normalization.
func canonicalEscapedPath(s string) string {
	var out strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+2 < len(s) {
			n, err := strconv.ParseUint(s[i+1:i+3], 16, 8)
			if err == nil {
				b := byte(n)
				if b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || strings.ContainsRune("-._~", rune(b)) {
					out.WriteByte(b)
				} else {
					out.WriteString(strings.ToUpper(s[i : i+3]))
				}
				i += 2
				continue
			}
		}
		out.WriteByte(s[i])
	}
	return out.String()
}

func hasControl(s string) bool {
	for _, c := range s {
		if unicode.IsControl(c) {
			return true
		}
	}
	return false
}

func invalid(field, message string) error {
	return &ValidationError{Field: field, Message: message}
}
