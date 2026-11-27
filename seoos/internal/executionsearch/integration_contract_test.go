package executionsearch

import (
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Autumn-27/artex/seoos/internal/domain"
)

func TestBingIntegrationOAuthRefreshAndBearerDiscovery(t *testing.T) {
	callback := "https://dashboard.example.com/seo/api/integrations/oauth/callback/bing"
	scopes := []string{domain.BingReadScope, domain.BingWriteScope}
	access := domain.IntegrationAccess{
		Application: domain.IntegrationApplication{Provider: domain.IntegrationBing, ClientID: "fixture-bing-client",
			Enabled: true, RedirectURI: callback, AllowedScopes: scopes},
		Account:    domain.IntegrationAccount{Provider: domain.IntegrationBing, Mode: domain.IntegrationOAuth},
		Credential: domain.ExecutionCredential{ClientID: "fixture-bing-client", ClientSecret: "fixture-bing-secret"},
	}
	state, verifier := strings.Repeat("s", 43), strings.Repeat("v", 48)
	auth, err := IntegrationAuthorizationURL(access, scopes, state, verifier)
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(auth)
	if err != nil || u.Host != "www.bing.com" || u.Path != "/webmasters/oauth/authorize" ||
		u.Query().Get("state") != state || u.Query().Get("redirect_uri") != callback ||
		u.Query().Get("scope") != strings.Join(scopes, " ") {
		t.Fatal("Bing authorize URI did not match the OAuth contract")
	}
	var calls atomic.Int32
	client := localClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Host != "www.bing.com" || r.URL.RawQuery != "" {
			t.Error("Bing OAuth unexpectedly used credentials in a service URL")
		}
		switch r.URL.Path {
		case "/webmasters/oauth/token":
			if r.Method != http.MethodPost || r.ParseForm() != nil ||
				r.Form.Get("client_id") != "fixture-bing-client" || r.Form.Get("client_secret") != "fixture-bing-secret" {
				t.Error("invalid Bing token authentication")
			}
			grant := r.Form.Get("grant_type")
			if grant == "authorization_code" && (r.Form.Get("redirect_uri") != callback || r.Form.Get("code") != "fixture-bing-code") ||
				grant == "refresh_token" && r.Form.Get("refresh_token") != "fixture-bing-refresh" {
				t.Error("invalid Bing token grant")
			}
			response(w, map[string]any{"access_token": "fixture-bing-access", "refresh_token": "fixture-bing-refresh",
				"expires_in": 3600, "token_type": "Bearer", "scope": strings.Join(scopes, " ")})
		case "/webmaster/api.svc/json/GetUserSites":
			if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer fixture-bing-access" {
				t.Error("Bing JSON reads require the OAuth bearer")
			}
			response(w, map[string]any{"d": []map[string]any{{"Url": testSite, "IsVerified": true}}})
		default:
			http.NotFound(w, r)
		}
	})
	credential, granted, err := client.ExchangeIntegration(t.Context(), access, scopes, "fixture-bing-code", verifier)
	if err != nil || !slices.Equal(granted, scopes) {
		t.Fatalf("Bing OAuth exchange failed: %v", err)
	}
	access.Account.Scopes, access.Account.Status, access.Credential = granted, domain.IntegrationConnected, credential
	fresh, _, err := client.RefreshIntegrationToken(t.Context(), access)
	if err != nil {
		t.Fatal(err)
	}
	access.Credential = fresh
	items, cursor, err := client.DiscoverIntegration(t.Context(), access, domain.ExecutionBing, "")
	if err != nil || len(items) != 1 || cursor != "" || items[0].ID != testSite || items[0].Permission != "verified" ||
		calls.Load() != 3 {
		t.Fatalf("Bing bearer resource discovery failed: %v", err)
	}
}

func TestManagedIndexNowReceiptsAndRootScope(t *testing.T) {
	config := connection(domain.ExecutionIndexNow, "")
	config.Managed = true
	secret := domain.ExecutionCredential{Secret: "0123456789abcdef"}
	urls := []string{testSite + "first", testSite + "second"}
	for _, check := range []struct {
		status int
		code   string
	}{
		{200, ""}, {202, ""}, {400, "execution_upstream_rejected"}, {403, "execution_upstream_forbidden"},
		{422, "execution_upstream_rejected"}, {429, "execution_upstream_quota"},
	} {
		t.Run(http.StatusText(check.status), func(t *testing.T) {
			client := localClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Host != "api.indexnow.org" || r.Method != http.MethodPost || r.URL.Path != "/indexnow" {
					t.Error("invalid IndexNow request destination")
				}
				w.Header().Set("Retry-After", "10")
				w.WriteHeader(check.status)
			})
			status, err := client.SubmitStatus(t.Context(), config, secret, urls)
			if status != check.status {
				t.Fatalf("IndexNow returned %d, expected %d", status, check.status)
			}
			if check.code == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			var failure *domain.DataError
			if !errors.As(err, &failure) || failure.Code != check.code ||
				check.status == 429 && failure.RetryAfter != 10*time.Second {
				t.Fatalf("unexpected IndexNow HTTP outcome: %v", err)
			}
		})
	}
}
