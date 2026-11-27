package executionsearch

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Autumn-27/artex/seoos/internal/domain"
)

const googleLiveListen = "127.0.0.1:42821"

type liveGoogleWire struct {
	http.RoundTripper
	refreshIssued bool
	grantedGA4    bool
}

func (wire *liveGoogleWire) RoundTrip(r *http.Request) (*http.Response, error) {
	resp, err := wire.RoundTripper.RoundTrip(r)
	if err != nil || r.URL.String() != googleTokenURL || resp.StatusCode != http.StatusOK || r.GetBody == nil {
		return resp, err
	}
	copyBody, err := r.GetBody()
	if err != nil {
		resp.Body.Close()
		return nil, err
	}
	payload, err := io.ReadAll(copyBody)
	copyBody.Close()
	if err != nil {
		resp.Body.Close()
		return nil, err
	}
	form, err := url.ParseQuery(string(payload))
	if err != nil {
		resp.Body.Close()
		return nil, err
	}
	if form.Get("grant_type") != "authorization_code" {
		return resp, nil
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	resp.Body.Close()
	if err != nil {
		return nil, err
	}
	resp.Body = io.NopCloser(bytes.NewReader(body))
	var token struct {
		Refresh string `json:"refresh_token"`
		Scope   string `json:"scope"`
	}
	if json.Unmarshal(body, &token) == nil {
		wire.refreshIssued = token.Refresh != ""
		wire.grantedGA4 = slices.Contains(strings.Fields(token.Scope), domain.GA4ReadScope)
	}
	return resp, nil
}

// This opt-in test uses a separate Google Cloud project and a Web client.
// It never writes the authorization code, token, or account ID to its output.
func TestLiveGoogleIncrementalOAuth(t *testing.T) {
	configPath := os.Getenv("SEOOS_LIVE_GOOGLE_CONFIG")
	if configPath == "" {
		t.Skip("LIVE_PROVIDER requires a private Google Web OAuth test configuration")
	}
	st, err := os.Stat(configPath)
	if err != nil || !st.Mode().IsRegular() || st.Mode().Perm()&0o077 != 0 {
		t.Fatal("LIVE_PROVIDER configuration must be a private regular file")
	}
	content, err := os.ReadFile(configPath)
	var config struct {
		ClientID        string `json:"client_id"`
		ClientSecret    string `json:"client_secret"`
		IsolatedProject bool   `json:"isolated_project"`
	}
	if err != nil || json.Unmarshal(content, &config) != nil || !config.IsolatedProject ||
		!validSecret(config.ClientID) || !validSecret(config.ClientSecret) {
		t.Fatal("LIVE_PROVIDER requires an isolated OAuth project and configured Web client")
	}
	defer clear(content)
	allowed := []string{domain.GSCReadScope, domain.GA4ReadScope}
	access := domain.IntegrationAccess{
		Application: domain.IntegrationApplication{Provider: domain.IntegrationGoogle, ClientID: config.ClientID,
			Enabled: true, AllowedScopes: allowed, RedirectURI: "http://" + googleLiveListen + domain.GoogleOAuthCallbackPath},
		Account: domain.IntegrationAccount{Provider: domain.IntegrationGoogle, Mode: domain.IntegrationOAuth,
			Status: domain.IntegrationReauth},
		Credential: domain.ExecutionCredential{ClientID: config.ClientID, ClientSecret: config.ClientSecret},
	}
	wire := &liveGoogleWire{RoundTripper: http.DefaultTransport}
	client := New(&http.Client{Transport: wire, Timeout: 12 * time.Second})
	ctx, cancel := context.WithTimeout(t.Context(), 12*time.Minute)
	defer cancel()
	authorize := func(label, scope string) domain.IntegrationAccess {
		t.Helper()
		verifier, state := googleLiveState(), googleLiveState()
		configured := access
		// The loopback URI is registered only for this isolated test.
		configured.Application.RedirectURI = "https://localhost" + domain.GoogleOAuthCallbackPath
		raw, err := IntegrationAuthorizationURL(configured, []string{scope}, state, verifier)
		if err != nil {
			t.Fatal(err)
		}
		start, err := url.Parse(raw)
		if err != nil {
			t.Fatal("cannot create a Google authorization URL")
		}
		q := start.Query()
		q.Set("redirect_uri", access.Application.RedirectURI)
		start.RawQuery = q.Encode()
		code := receiveLiveGoogleCode(ctx, t, label, state, start.String())
		credential, scopes, err := client.ExchangeIntegration(ctx, access, []string{scope}, code, verifier)
		if err != nil {
			t.Fatalf("LIVE_PROVIDER %s code exchange: %v", label, err)
		}
		if credential.ExternalSubject == "" || !slices.Contains(scopes, scope) {
			t.Fatalf("LIVE_PROVIDER %s missing verified identity or requested scope", label)
		}
		next := access
		next.Account.Scopes, next.Account.Status, next.Account.VerifiedSubject = scopes, domain.IntegrationConnected, credential.ExternalSubject
		next.Credential = credential
		return next
	}
	access = authorize("first GSC grant", domain.GSCReadScope)
	if !wire.refreshIssued || wire.grantedGA4 {
		t.Fatal("LIVE_PROVIDER first grant needs a fresh project/user pair with only GSC permission")
	}
	t.Log("LIVE_PROVIDER first GSC grant returned a refresh token and passed signed OIDC identity verification")
	original := access
	access = authorize("add GA4 permission on the same Google account", domain.GA4ReadScope)
	if access.Account.VerifiedSubject != original.Account.VerifiedSubject || !slices.Contains(access.Account.Scopes, domain.GSCReadScope) {
		t.Fatal("LIVE_PROVIDER incremental OAuth changed the account or removed the GSC scope")
	}
	t.Logf("LIVE_PROVIDER new refresh token issued=%t, stored refresh token replaced=%t",
		wire.refreshIssued, access.Credential.RefreshToken != original.Credential.RefreshToken)
	_, _, err = client.refreshIntegrationToken(ctx, original, allowed)
	var problem *domain.DataError
	switch {
	case err == nil:
		t.Log("LIVE_PROVIDER original refresh token can obtain both scopes")
	case errors.As(err, &problem) && (problem.Code == "integration_oauth_scope_required" || problem.Code == "integration_invalid_grant"):
		t.Logf("LIVE_PROVIDER original refresh token cannot obtain both scopes (%s)", problem.Code)
	default:
		t.Fatalf("LIVE_PROVIDER original refresh token probe: %v", err)
	}
	for _, service := range []string{domain.ExecutionGSC, domain.ExecutionGA4} {
		_, _, err := client.DiscoverIntegration(ctx, access, service, "")
		if err != nil {
			t.Errorf("LIVE_PROVIDER %s read after increment: %v", service, err)
		} else {
			t.Logf("LIVE_PROVIDER %s resource discovery succeeded with the incremented account", service)
		}
	}
}

func googleLiveState() string {
	var entropy [32]byte
	rand.Read(entropy[:])
	return base64.RawURLEncoding.EncodeToString(entropy[:])
}

func receiveLiveGoogleCode(ctx context.Context, t *testing.T, label, state, authorizeURL string) string {
	t.Helper()
	listener, err := net.Listen("tcp4", googleLiveListen)
	if err != nil {
		t.Fatal(err)
	}
	result := make(chan string, 1)
	done := make(chan struct{}, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /start", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		http.Redirect(w, r, authorizeURL, http.StatusFound)
	})
	mux.HandleFunc("GET "+domain.GoogleOAuthCallbackPath, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if subtle.ConstantTimeCompare([]byte(q.Get("state")), []byte(state)) != 1 {
			http.Error(w, "Invalid authorization state.", http.StatusForbidden)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		http.Redirect(w, r, "/done", http.StatusSeeOther)
		select {
		case result <- q.Get("code"):
		default:
		}
	})
	mux.HandleFunc("GET /done", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		_, _ = io.WriteString(w, "OAuth callback received. Return to the Phase 4.1 test output.")
		select {
		case done <- struct{}{}:
		default:
		}
	})
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	defer server.Close()
	go func() { _ = server.Serve(listener) }()
	// Stream this instruction even while go test is waiting for the browser.
	t.Logf("LIVE_PROVIDER %s: open http://%s/start in Chrome", label, googleLiveListen)
	select {
	case <-ctx.Done():
		t.Fatal("LIVE_PROVIDER timed out waiting for the Google callback")
	case code := <-result:
		select {
		case <-done:
		case <-time.After(time.Second):
		}
		if code == "" {
			t.Fatal("LIVE_PROVIDER browser returned no authorization code")
		}
		return code
	}
	return ""
}
