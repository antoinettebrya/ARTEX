package domain

import (
	"errors"
	"net/url"
	"path"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	IntegrationGoogle = "google"
	IntegrationBing   = "bing"
	IntegrationOAuth  = "oauth"
	IntegrationAPIKey = "api_key"

	GSCReadScope   = "https://www.googleapis.com/auth/webmasters.readonly"
	GA4ReadScope   = "https://www.googleapis.com/auth/analytics.readonly"
	BingReadScope  = "Webmaster.read"
	BingWriteScope = "Webmaster.manage"
	IdentityScope  = "openid"

	IntegrationConnected = "CONNECTED"
	IntegrationReauth    = "REAUTH_REQUIRED"
	IntegrationRevoked   = "REVOKED"
)

var (
	ErrIntegrationDependency = errors.New("remove the dependent integration bindings first")
	ErrIntegrationRequired   = errors.New("select an active search integration")
	ErrIntegrationRevoked    = errors.New("the integration authorization was revoked or changed")
	hexIndexNowKey           = regexp.MustCompile(`^[0-9a-fA-F-]{8,128}$`)
	legacyIndexNowKey        = regexp.MustCompile(`^[A-Za-z0-9-]{8,128}$`)
)

type IntegrationApplication struct {
	ID            string    `json:"id"`
	Provider      string    `json:"provider"`
	Label         string    `json:"label"`
	ClientID      string    `json:"client_id"`
	RedirectURI   string    `json:"redirect_uri"`
	AllowedScopes []string  `json:"allowed_scopes"`
	Version       int64     `json:"config_version"`
	Enabled       bool      `json:"enabled"`
	Configured    bool      `json:"configured"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type IntegrationApplicationInput struct {
	Provider      string   `json:"provider"`
	Label         string   `json:"label"`
	ClientID      string   `json:"client_id"`
	ClientSecret  string   `json:"client_secret,omitempty"`
	RedirectURI   string   `json:"redirect_uri"`
	AllowedScopes []string `json:"allowed_scopes"`
	Version       int64    `json:"config_version,omitempty"`
	Enabled       bool     `json:"enabled"`
}

type IntegrationAccount struct {
	ID                string     `json:"id"`
	ApplicationID     string     `json:"application_id,omitempty"`
	Provider          string     `json:"provider"`
	Label             string     `json:"label"`
	Mode              string     `json:"auth_mode"`
	Scopes            []string   `json:"granted_scopes"`
	Version           int64      `json:"credential_version"`
	Status            string     `json:"authorization_status"`
	ExpiresAt         *time.Time `json:"expires_at,omitempty"`
	LastRefreshAt     *time.Time `json:"last_refresh_at,omitempty"`
	RequestsPerMinute int        `json:"requests_per_minute"`
	RequestsPerDay    int        `json:"requests_per_day"`
}

type IntegrationAccountInput struct {
	ApplicationID     string `json:"application_id,omitempty"`
	Provider          string `json:"provider"`
	Label             string `json:"label"`
	Mode              string `json:"auth_mode"`
	APIKey            string `json:"api_key,omitempty"`
	RequestsPerMinute int    `json:"requests_per_minute,omitempty"`
	RequestsPerDay    int    `json:"requests_per_day,omitempty"`
}

type IntegrationAccess struct {
	Account     IntegrationAccount
	Application IntegrationApplication
	Credential  ExecutionCredential
}

type IntegrationResource struct {
	Kind        string   `json:"provider"`
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	URL         string   `json:"url,omitempty"`
	Permission  string   `json:"permission,omitempty"`
	ParentID    string   `json:"parent_id,omitempty"`
	ParentName  string   `json:"parent_name,omitempty"`
	TimeZone    string   `json:"time_zone,omitempty"`
	Currency    string   `json:"currency,omitempty"`
	StreamHosts []string `json:"stream_hosts,omitempty"`
}

type IntegrationBindingInput struct {
	SiteID            string `json:"site_id"`
	AccountID         string `json:"integration_account_id"`
	Kind              string `json:"provider"`
	ResourceID        string `json:"external_resource_id"`
	Enabled           bool   `json:"enabled"`
	AllowWrites       bool   `json:"allow_writes"`
	Version           int64  `json:"version,omitempty"`
	RequestsPerMinute int    `json:"requests_per_minute,omitempty"`
	RequestsPerDay    int    `json:"requests_per_day,omitempty"`
}

type IndexNowKeyInput struct {
	SiteID      string `json:"site_id"`
	Directory   string `json:"directory,omitempty"`
	Key         string `json:"key,omitempty"`
	Version     int64  `json:"version,omitempty"`
	AllowWrites bool   `json:"allow_writes"`
}

type IntegrationFilter struct {
	Provider, SiteID, AccountID, Status, Query, From, To string
	Limit, Offset                                        int
}

func IntegrationScopes(provider string, values []string) ([]string, error) {
	var result []string
	for _, value := range values {
		if !(provider == IntegrationGoogle && (value == GSCReadScope || value == GA4ReadScope || value == IdentityScope) ||
			provider == IntegrationBing && (value == BingReadScope || value == BingWriteScope)) {
			return nil, invalid("scopes", "choose a documented scope for this provider")
		}
		if !slices.Contains(result, value) {
			result = append(result, value)
		}
	}
	if len(result) == 0 || len(result) == 1 && result[0] == IdentityScope {
		return nil, invalid("scopes", "choose at least one search or analytics permission")
	}
	return result, nil
}

func (a IntegrationAccount) Allows(kind string, write bool) bool {
	if a.Status != IntegrationConnected {
		return false
	}
	switch kind {
	case ExecutionGSC:
		return a.Provider == IntegrationGoogle && !write && slices.Contains(a.Scopes, GSCReadScope)
	case ExecutionGA4:
		return a.Provider == IntegrationGoogle && !write && slices.Contains(a.Scopes, GA4ReadScope)
	case ExecutionBing:
		return a.Provider == IntegrationBing && (a.Mode == IntegrationAPIKey ||
			slices.Contains(a.Scopes, BingWriteScope) || !write && slices.Contains(a.Scopes, BingReadScope))
	}
	return false
}

func ValidIntegrationText(value string, limit int) bool {
	return strings.TrimSpace(value) != "" && len(value) <= limit && utf8.ValidString(value) && !hasControl(value)
}

func ValidIntegrationSecret(value string) bool {
	return len(value) > 0 && len(value) <= 8192 && !strings.ContainsFunc(value, func(ch rune) bool { return ch < '!' || ch > '~' })
}

func ValidIntegrationCallback(raw, provider string) bool {
	u, err := EvidenceURL(raw)
	return err == nil && u.Scheme == "https" && !u.ForceQuery && u.RawQuery == "" &&
		u.EscapedPath() == "/seo/api/integrations/oauth/callback/"+provider
}

func ValidIndexNowKey(value string, legacy bool) bool {
	if legacy {
		return legacyIndexNowKey.MatchString(value)
	}
	return hexIndexNowKey.MatchString(value) && strings.Trim(value, "-") != ""
}

var indexNowDirectory = regexp.MustCompile(`^(/[A-Za-z0-9._~-]+)*/?$`)

func IndexNowDirectory(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "/" {
		return "/", nil
	}
	if len(raw) > 1024 || !indexNowDirectory.MatchString(raw) || strings.TrimSuffix(raw, "/") != path.Clean(raw) {
		return "", invalid("directory", "choose a directory such as /catalog/ without escaped segments")
	}
	return path.Clean(raw) + "/", nil
}

func IndexNowLocation(base, directory, key string) (string, error) {
	u, err := EvidenceURL(base)
	if err != nil || u.Scheme != "https" || (u.Port() != "" && u.Port() != "443") || u.RawQuery != "" {
		return "", invalid("site_id", "IndexNow needs a registered HTTPS host on its standard port")
	}
	dir, err := IndexNowDirectory(directory)
	if err != nil {
		return "", err
	}
	if !ValidIndexNowKey(key, true) {
		return "", invalid("key", "invalid IndexNow key")
	}
	return (&url.URL{Scheme: "https", Host: u.Hostname(), Path: dir + key + ".txt"}).String(), nil
}
