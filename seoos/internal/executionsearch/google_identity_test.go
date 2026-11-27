package executionsearch

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"maps"
	"math/big"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Autumn-27/artex/seoos/internal/domain"
)

type oauthTestSigner struct{ key *rsa.PrivateKey }

func newOAuthTestSigner(t *testing.T) oauthTestSigner {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return oauthTestSigner{key}
}

func (s oauthTestSigner) jwks(w http.ResponseWriter) {
	encode := base64.RawURLEncoding.EncodeToString
	response(w, map[string]any{"keys": []map[string]string{{
		"kty": "RSA", "kid": "oidc-contract-key", "use": "sig", "alg": "RS256",
		"n": encode(s.key.N.Bytes()), "e": encode(big.NewInt(int64(s.key.E)).Bytes()),
	}}})
}

func (s oauthTestSigner) sign(t *testing.T, claims map[string]any) string {
	t.Helper()
	raw, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	encode := base64.RawURLEncoding.EncodeToString
	input := encode([]byte(`{"alg":"RS256","kid":"oidc-contract-key"}`)) + "." + encode(raw)
	hash := sha256.Sum256([]byte(input))
	signature, err := rsa.SignPKCS1v15(rand.Reader, s.key, crypto.SHA256, hash[:])
	if err != nil {
		t.Fatal(err)
	}
	return input + "." + encode(signature)
}

func oauthTestClaims(aud, subject, nonce string) map[string]any {
	return map[string]any{"iss": googleIssuer, "aud": aud, "sub": subject,
		"iat": time.Now().Unix(), "exp": time.Now().Add(time.Hour).Unix(), "nonce": nonce}
}

func TestGoogleIdentityVerifiesSignedClaims(t *testing.T) {
	signer := newOAuthTestSigner(t)
	imposter := newOAuthTestSigner(t)
	client := localClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Host != "www.googleapis.com" || r.URL.Path != "/oauth2/v3/certs" || r.Header.Get("Authorization") != "" {
			t.Error("unexpected key endpoint")
		}
		signer.jwks(w)
	})
	const audience, subject = "test-client-id", "stable-test-subject"
	nonce := googleNonce(strings.Repeat("a", 43))
	claims := oauthTestClaims(audience, subject, nonce)
	claim := func(name string, value any) map[string]any {
		copy := maps.Clone(claims)
		copy[name] = value
		return copy
	}
	tests := []struct {
		name   string
		claims map[string]any
		signer oauthTestSigner
		valid  bool
	}{
		{"valid", claims, signer, true},
		{"legacy Google issuer", claim("iss", "accounts.google.com"), signer, true},
		{"other issuer", claim("iss", "https://unrelated.example.test"), signer, false},
		{"wrong audience", claim("aud", "wrong"), signer, false},
		{"wrong authorized party", claim("azp", "wrong"), signer, false},
		{"multiple audiences require azp", claim("aud", []string{audience, "wrong"}), signer, false},
		{"wrong nonce", claim("nonce", googleNonce(strings.Repeat("b", 43))), signer, false},
		{"missing nonce", claim("nonce", ""), signer, false},
		{"wrong access token hash", claim("at_hash", "wrong"), signer, false},
		{"expired", claim("exp", time.Now().Add(-time.Hour).Unix()), signer, false},
		{"missing subject", claim("sub", ""), signer, false},
		{"untrusted signing key", claims, imposter, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := client.verifiedGoogleSubject(t.Context(), tc.signer.sign(t, tc.claims), audience, nonce, "fixture-access")
			if tc.valid && (err != nil || got != subject) || !tc.valid && (err == nil || got != "") {
				t.Fatalf("identity verification did not produce the expected result: %v", err)
			}
		})
	}
}

func TestGoogleIncrementalGrantKeepsItsVerifiedIdentity(t *testing.T) {
	signer := newOAuthTestSigner(t)
	verifier := strings.Repeat("v", 43)
	const firstSubject = "fixture-google-subject"
	const originalRefresh, newRefresh = "fixture-original-refresh", "fixture-replacement-refresh"
	scopes := []string{domain.GSCReadScope, domain.GA4ReadScope}
	app := domain.IntegrationApplication{Provider: domain.IntegrationGoogle, Enabled: true, ClientID: "fixture-client",
		RedirectURI: "https://example.test" + domain.GoogleOAuthCallbackPath, AllowedScopes: scopes}
	for _, tc := range []struct {
		name, incumbent, selected, offered, refresh, errorCode string
		oldScopes, refreshedScopes                             []string
		omitScope                                              bool
	}{
		{"first GSC grant", "", firstSubject, originalRefresh, "", "", nil, nil, false},
		{"old refresh gains GA4", firstSubject, firstSubject, "", originalRefresh, "", scopes[:1], scopes, false},
		{"replacement refresh kept", firstSubject, firstSubject, newRefresh, newRefresh, "", scopes[:1], scopes, true},
		{"old refresh missing new scope", firstSubject, firstSubject, "", originalRefresh, "integration_oauth_scope_required", scopes[:1], nil, true},
		{"other Google account", firstSubject, "different-subject", newRefresh, "", "integration_oauth_identity_mismatch", scopes[:1], nil, false},
		{"old identity unproven", "", firstSubject, newRefresh, originalRefresh, "integration_oauth_identity_unverified", scopes[:1], scopes[:1], false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			access := domain.IntegrationAccess{Application: app,
				Account: domain.IntegrationAccount{Provider: app.Provider, Mode: domain.IntegrationOAuth,
					Scopes: tc.oldScopes, VerifiedSubject: tc.incumbent},
				Credential: domain.ExecutionCredential{ClientID: app.ClientID, ClientSecret: "fixture-secret"}}
			requested, wanted := scopes[:1], scopes[:1]
			if len(tc.oldScopes) > 0 {
				access.Credential.RefreshToken = originalRefresh
				requested, wanted = scopes[1:], scopes
			}
			authorizing, err := IntegrationAuthorizationURL(access, requested, strings.Repeat("s", 43), verifier)
			if err != nil {
				t.Fatal(err)
			}
			url, err := url.Parse(authorizing)
			if err != nil || url.Query().Get("nonce") != googleNonce(verifier) ||
				url.Query().Get("include_granted_scopes") != "true" ||
				!slices.Contains(strings.Fields(url.Query().Get("scope")), domain.IdentityScope) {
				t.Fatal("Google code authorization omitted its identity or incremental controls")
			}
			var refreshes int
			client := localClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Host == "www.googleapis.com" && r.URL.Path == "/oauth2/v3/certs" {
					signer.jwks(w)
					return
				}
				if r.Host != "oauth2.googleapis.com" || r.URL.Path != "/token" || r.ParseForm() != nil {
					t.Error("unexpected token request")
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				wire := map[string]any{"access_token": "fixture-access", "expires_in": 3600, "token_type": "Bearer"}
				if r.Form.Get("grant_type") == "authorization_code" {
					if r.Form.Get("code_verifier") != verifier || r.Form.Get("redirect_uri") != app.RedirectURI {
						t.Error("incorrect PKCE exchange")
					}
					wire["scope"] = domain.IdentityScope + " " + strings.Join(wanted, " ")
					wire["id_token"] = signer.sign(t, oauthTestClaims(app.ClientID, tc.selected, googleNonce(verifier)))
					if tc.offered != "" {
						wire["refresh_token"] = tc.offered
					}
				} else {
					refreshes++
					if tc.refresh == "" || r.Form.Get("refresh_token") != tc.refresh {
						t.Error("refresh used an unapproved credential")
					}
					if !tc.omitScope {
						wire["scope"] = strings.Join(tc.refreshedScopes, " ")
					}
				}
				response(w, wire)
			})
			got, granted, err := client.ExchangeIntegration(t.Context(), access, requested, "fixture-code", verifier)
			if tc.errorCode != "" {
				var problem *domain.DataError
				if !errors.As(err, &problem) || problem.Code != tc.errorCode || got.AccessToken != "" {
					t.Fatalf("expected %s without a credential, got %v", tc.errorCode, err)
				}
			} else if err != nil || got.ExternalSubject != firstSubject || !slices.Equal(granted, wanted) ||
				tc.refresh != "" && got.RefreshToken != tc.refresh || tc.refresh == "" && got.RefreshToken != tc.offered {
				t.Fatalf("valid grant did not retain authorizations and identity: %v", err)
			}
			if (tc.refresh != "") != (refreshes == 1) {
				t.Fatalf("unexpected refresh count %d", refreshes)
			}
		})
	}
}
