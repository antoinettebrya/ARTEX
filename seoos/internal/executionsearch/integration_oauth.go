package executionsearch

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/Autumn-27/artex/seoos/internal/domain"
)

const bingOAuthURL = "https://www.bing.com/webmasters/oauth/"

func IntegrationAuthorizationURL(access domain.IntegrationAccess, scopes []string, state, verifier string) (string, error) {
	app := access.Application
	if !validSecret(app.ClientID) || !domain.ValidIntegrationCallback(app.RedirectURI, app.Provider) ||
		!unreserved(state, 43, 43) || !unreserved(verifier, 43, 128) {
		return "", failure("integration_invalid_oauth_configuration")
	}
	params := url.Values{
		"client_id": {app.ClientID}, "redirect_uri": {app.RedirectURI},
		"response_type": {"code"}, "state": {state}, "scope": {strings.Join(scopes, " ")},
	}
	endpoint := bingOAuthURL + "authorize"
	if app.Provider == domain.IntegrationGoogle {
		endpoint = googleAuthorizeURL
		challenge := sha256.Sum256([]byte(verifier))
		params.Set("code_challenge", base64.RawURLEncoding.EncodeToString(challenge[:]))
		params.Set("code_challenge_method", "S256")
		params.Set("access_type", "offline")
		params.Set("include_granted_scopes", "true")
		if (access.Account.Status == domain.IntegrationReauth || access.Account.Status == domain.IntegrationRevoked) && access.Credential.RefreshToken == "" {
			params.Set("prompt", "consent")
		}
	}
	return endpoint + "?" + params.Encode(), nil
}

func (c *Client) ExchangeIntegration(ctx context.Context, access domain.IntegrationAccess, scopes []string, code, verifier string) (domain.ExecutionCredential, []string, error) {
	if !validSecret(code) || !unreserved(verifier, 43, 128) {
		return domain.ExecutionCredential{}, nil, failure("integration_invalid_oauth_code")
	}
	params := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {access.Application.RedirectURI}}
	if access.Account.Provider == domain.IntegrationGoogle {
		params.Set("code_verifier", verifier)
	}
	// A bound account keeps its original identity. Refresh the prior grant and
	// require proof that it now includes the incrementally requested scopes.
	// The consent exchange alone could have used a different signed-in user.
	if access.Credential.RefreshToken != "" {
		if access.Account.Provider != domain.IntegrationGoogle {
			return domain.ExecutionCredential{}, nil, failure("integration_revoke_before_reconnect")
		}
		if _, _, err := c.integrationToken(ctx, access, scopes, params, false); err != nil {
			return domain.ExecutionCredential{}, nil, err
		}
		for _, scope := range access.Account.Scopes {
			if !slices.Contains(scopes, scope) {
				scopes = append(scopes, scope)
			}
		}
		return c.integrationToken(ctx, access, scopes, url.Values{
			"grant_type": {"refresh_token"}, "refresh_token": {access.Credential.RefreshToken},
		}, true)
	}
	return c.integrationToken(ctx, access, scopes, params, true)
}

func (c *Client) RefreshIntegrationToken(ctx context.Context, access domain.IntegrationAccess) (domain.ExecutionCredential, []string, error) {
	if !validSecret(access.Credential.RefreshToken) {
		return domain.ExecutionCredential{}, nil, failure("integration_invalid_grant")
	}
	return c.integrationToken(ctx, access, access.Account.Scopes, url.Values{
		"grant_type": {"refresh_token"}, "refresh_token": {access.Credential.RefreshToken},
	}, true)
}

func (c *Client) integrationToken(ctx context.Context, access domain.IntegrationAccess, requested []string,
	params url.Values, requireRefresh bool,
) (domain.ExecutionCredential, []string, error) {
	prior := access.Credential
	if !validSecret(prior.ClientID) || !validSecret(prior.ClientSecret) {
		return domain.ExecutionCredential{}, nil, failure("integration_invalid_oauth_configuration")
	}
	params.Set("client_id", prior.ClientID)
	params.Set("client_secret", prior.ClientSecret)
	endpoint := googleTokenURL
	refreshing := params.Get("grant_type") == "refresh_token"
	if access.Account.Provider == domain.IntegrationBing {
		endpoint = bingOAuthURL + "token"
	}
	var token struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		TokenType    string `json:"token_type"`
		Scope        string `json:"scope"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	_, err := c.request(ctx, domain.ExecutionConnection{}, http.MethodPost, endpoint, "", "application/x-www-form-urlencoded", []byte(params.Encode()), &token)
	if err != nil {
		return domain.ExecutionCredential{}, nil, err
	}
	if !validSecret(token.AccessToken) || !strings.EqualFold(token.TokenType, "bearer") || token.ExpiresIn <= 0 ||
		token.ExpiresIn > 24*60*60 || token.RefreshToken != "" && !validSecret(token.RefreshToken) {
		return domain.ExecutionCredential{}, nil, failure("integration_invalid_oauth_response")
	}
	granted := strings.Fields(token.Scope)
	if len(granted) == 0 && access.Account.Provider == domain.IntegrationBing {
		// An omitted scope in a compliant token response means the requested scope.
		granted = requested
	}
	if len(granted) == 0 && refreshing {
		// A scope-less refresh can continue already verified permissions only.
		granted = access.Account.Scopes
	}
	selected := make([]string, 0, len(granted))
	for _, scope := range granted {
		if slices.Contains(access.Application.AllowedScopes, scope) && !slices.Contains(selected, scope) &&
			(slices.Contains(requested, scope) || slices.Contains(access.Account.Scopes, scope)) {
			selected = append(selected, scope)
		}
	}
	for _, scope := range requested {
		if !slices.Contains(selected, scope) {
			return domain.ExecutionCredential{}, nil, failure("integration_oauth_scope_required")
		}
	}
	refresh := token.RefreshToken
	if refresh == "" || refreshing && access.Account.Provider == domain.IntegrationGoogle {
		refresh = prior.RefreshToken
	}
	if requireRefresh && !validSecret(refresh) {
		return domain.ExecutionCredential{}, nil, failure("integration_refresh_token_required")
	}
	expires := time.Now().UTC().Add(time.Duration(token.ExpiresIn) * time.Second)
	return domain.ExecutionCredential{ClientID: prior.ClientID, ClientSecret: prior.ClientSecret,
		AccessToken: token.AccessToken, RefreshToken: refresh, ExpiresAt: &expires}, selected, nil
}

func (c *Client) RevokeGoogleIntegration(ctx context.Context, access domain.IntegrationAccess) error {
	if access.Account.Provider != domain.IntegrationGoogle || !validSecret(access.Credential.RefreshToken) {
		return nil
	}
	params := url.Values{"token": {access.Credential.RefreshToken}}
	_, err := c.request(ctx, domain.ExecutionConnection{}, http.MethodPost, "https://oauth2.googleapis.com/revoke", "",
		"application/x-www-form-urlencoded", []byte(params.Encode()), nil)
	return err
}
