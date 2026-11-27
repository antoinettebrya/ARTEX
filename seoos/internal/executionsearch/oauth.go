package executionsearch

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Autumn-27/artex/seoos/internal/domain"
)

const (
	googleAuthorizeURL = "https://accounts.google.com/o/oauth2/v2/auth"
	gscScope           = "https://www.googleapis.com/auth/webmasters.readonly"
	gaScope            = "https://www.googleapis.com/auth/analytics.readonly"
)

// Authorize builds a Google consent URL. The caller owns callback state,
// expiration, tenant identity and single-use enforcement.
func Authorize(config domain.ExecutionConnection, secret domain.ExecutionCredential, state, verifier string) (string, error) {
	scope, err := googleScope(config.Kind)
	if err != nil {
		return "", err
	}
	if !validSecret(secret.ClientID) || !validRedirect(config.RedirectURI) ||
		!unreserved(state, 16, 128) || !unreserved(verifier, 43, 128) {
		return "", failure("execution_invalid_oauth_configuration")
	}
	challenge := sha256.Sum256([]byte(verifier))
	params := url.Values{
		"access_type":           {"offline"},
		"client_id":             {secret.ClientID},
		"code_challenge":        {base64.RawURLEncoding.EncodeToString(challenge[:])},
		"code_challenge_method": {"S256"},
		"prompt":                {"consent"},
		"redirect_uri":          {config.RedirectURI},
		"response_type":         {"code"},
		"scope":                 {scope},
		"state":                 {state},
	}
	return googleAuthorizeURL + "?" + params.Encode(), nil
}

func (c *Client) Exchange(ctx context.Context, config domain.ExecutionConnection, secret domain.ExecutionCredential, code, verifier string) (domain.ExecutionCredential, error) {
	if !validRedirect(config.RedirectURI) || !validSecret(code) || !unreserved(verifier, 43, 128) {
		return domain.ExecutionCredential{}, failure("execution_invalid_oauth_code")
	}
	params := url.Values{
		"code":          {code},
		"code_verifier": {verifier},
		"grant_type":    {"authorization_code"},
		"redirect_uri":  {config.RedirectURI},
	}
	return c.googleToken(ctx, config, secret, params)
}

func (c *Client) Refresh(ctx context.Context, config domain.ExecutionConnection, secret domain.ExecutionCredential) (domain.ExecutionCredential, error) {
	if !validSecret(secret.RefreshToken) {
		return domain.ExecutionCredential{}, failure("execution_google_refresh_required")
	}
	return c.googleToken(ctx, config, secret, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {secret.RefreshToken},
	})
}

func (c *Client) googleToken(ctx context.Context, config domain.ExecutionConnection, secret domain.ExecutionCredential, params url.Values) (domain.ExecutionCredential, error) {
	scope, err := googleScope(config.Kind)
	if err != nil {
		return domain.ExecutionCredential{}, err
	}
	if !validSecret(secret.ClientID) || !validSecret(secret.ClientSecret) {
		return domain.ExecutionCredential{}, failure("execution_invalid_oauth_configuration")
	}
	params.Set("client_id", secret.ClientID)
	params.Set("client_secret", secret.ClientSecret)
	var token struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
		TokenType    string `json:"token_type"`
		Scope        string `json:"scope"`
	}
	_, err = c.request(ctx, config, http.MethodPost, googleTokenURL, "", "application/x-www-form-urlencoded",
		[]byte(params.Encode()), &token)
	if err != nil {
		return domain.ExecutionCredential{}, err
	}
	if !validSecret(token.AccessToken) || !strings.EqualFold(token.TokenType, "bearer") ||
		token.ExpiresIn <= 0 || token.ExpiresIn > int64((24*time.Hour)/time.Second) ||
		(token.RefreshToken != "" && !validSecret(token.RefreshToken)) {
		return domain.ExecutionCredential{}, failure("execution_invalid_oauth_response")
	}
	if token.Scope != "" && !containsScope(token.Scope, scope) {
		return domain.ExecutionCredential{}, failure("execution_oauth_scope_required")
	}
	refresh := token.RefreshToken
	if refresh == "" {
		refresh = secret.RefreshToken
	}
	if !validSecret(refresh) {
		return domain.ExecutionCredential{}, failure("execution_google_refresh_required")
	}
	expires := time.Now().UTC().Add(time.Duration(token.ExpiresIn) * time.Second)
	return domain.ExecutionCredential{
		ClientID:     secret.ClientID,
		ClientSecret: secret.ClientSecret,
		AccessToken:  token.AccessToken,
		RefreshToken: refresh,
		ExpiresAt:    &expires,
	}, nil
}

func containsScope(list, required string) bool {
	for _, scope := range strings.Fields(list) {
		if scope == required {
			return true
		}
	}
	return false
}

func googleScope(kind string) (string, error) {
	switch kind {
	case domain.ExecutionGSC:
		return gscScope, nil
	case domain.ExecutionGA4:
		return gaScope, nil
	}
	return "", domain.ErrExecutionUnsupported
}

func validRedirect(raw string) bool {
	target, err := domain.EvidenceURL(raw)
	if err != nil || target.Hostname() == "" {
		return false
	}
	if target.Scheme == "https" {
		return true
	}
	ip := net.ParseIP(target.Hostname())
	return ip != nil && ip.IsLoopback() || target.Hostname() == "localhost"
}

func unreserved(value string, low, high int) bool {
	if len(value) < low || len(value) > high {
		return false
	}
	for _, ch := range value {
		letter := (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z')
		digit := ch >= '0' && ch <= '9'
		if !letter && !digit && !strings.ContainsRune("-._~", ch) {
			return false
		}
	}
	return true
}
