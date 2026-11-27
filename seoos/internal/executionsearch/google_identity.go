package executionsearch

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"net/http"
	"time"

	"github.com/Autumn-27/artex/seoos/internal/domain"
	"github.com/coreos/go-oidc/v3/oidc"
)

const (
	googleIssuer = "https://accounts.google.com"
	googleJWKS   = "https://www.googleapis.com/oauth2/v3/certs"
)

func googleIdentityKeys(client *http.Client) oidc.KeySet {
	bounded := *client
	if bounded.Timeout <= 0 || bounded.Timeout > 8*time.Second {
		bounded.Timeout = 8 * time.Second
	}
	return oidc.NewRemoteKeySet(oidc.ClientContext(context.Background(), &bounded), googleJWKS)
}

// The one-use PKCE verifier is already protected by the OAuth state record.
// Domain separation gives each authorization an independent public nonce.
func googleNonce(verifier string) string {
	digest := sha256.Sum256([]byte("artex-google-oidc-nonce-v1:" + verifier))
	return base64.RawURLEncoding.EncodeToString(digest[:])
}

func (c *Client) verifiedGoogleSubject(ctx context.Context, token, audience, nonce, accessToken string) (string, error) {
	if !domain.ValidIntegrationSecret(token) {
		return "", failure("integration_oauth_invalid_identity")
	}
	verifier := oidc.NewVerifier(googleIssuer, c.googleKeys, &oidc.Config{
		ClientID: audience, SupportedSigningAlgs: []string{oidc.RS256},
	})
	id, err := verifier.Verify(ctx, token)
	if err != nil {
		var provider *domain.DataError
		if errors.As(err, &provider) {
			return "", provider
		}
		return "", failure("integration_oauth_invalid_identity")
	}
	var claims struct {
		AuthorizedParty string `json:"azp"`
	}
	if !domain.ValidIntegrationSubject(id.Subject) || id.IssuedAt.IsZero() || id.IssuedAt.After(time.Now().Add(2*time.Minute)) ||
		id.Claims(&claims) != nil || len(id.Audience) > 1 && claims.AuthorizedParty == "" ||
		claims.AuthorizedParty != "" && claims.AuthorizedParty != audience ||
		nonce != "" && subtle.ConstantTimeCompare([]byte(id.Nonce), []byte(nonce)) != 1 ||
		id.AccessTokenHash != "" && id.VerifyAccessToken(accessToken) != nil {
		return "", failure("integration_oauth_invalid_identity")
	}
	return id.Subject, nil
}
