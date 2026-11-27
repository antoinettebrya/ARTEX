package providerprobe

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/Autumn-27/artex/seoos/internal/domain"
)

const (
	model    = "test-model"
	testKey  = "fixture-testing-key"
	login    = "fixture@example.test"
	password = "fixture-password"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (fn transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return fn(r) }

func TestProviderChecksUseReadOnlyVendorAPIs(t *testing.T) {
	for _, tt := range []struct {
		kind, endpoint, header, authorization, body string
	}{
		{domain.ProviderOpenAI, "https://api.openai.com/v1/models/test-model", "Authorization", "Bearer " + testKey,
			`{"object":"model","id":"test-model"}`},
		{domain.ProviderAnthropic, "https://api.anthropic.com/v1/models/test-model", "X-Api-Key", testKey,
			`{"type":"model","id":"canonical-model-id"}`},
		{domain.ProviderGemini, "https://generativelanguage.googleapis.com/v1beta/models/test-model", "X-Goog-Api-Key", testKey,
			`{"name":"models/resolved-alias","supportedGenerationMethods":["generateContent"]}`},
		{domain.ProviderDeepSeek, "https://api.deepseek.com/models", "Authorization", "Bearer " + testKey,
			`{"object":"list","data":[{"id":"another-model"},{"id":"test-model"}]}`},
		{domain.ProviderDataForSEO, "https://api.dataforseo.com/v3/appendix/user_data", "Authorization", "",
			`{"status_code":20000,"tasks":[{"status_code":20000,"result_count":1,"result":[{"login":"fixture@example.test"}]}]}`},
	} {
		t.Run(tt.kind, func(t *testing.T) {
			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.ContentLength != 0 || r.URL.RawQuery != "" {
					t.Error("expected a read-only request without a body or query parameters")
				}
				if tt.kind == domain.ProviderDataForSEO {
					name, secret, ok := r.BasicAuth()
					if !ok || name != login || secret != password {
						t.Error("missing DataForSEO Basic authorization")
					}
				} else if r.Header.Get(tt.header) != tt.authorization {
					t.Error("missing vendor authorization")
				}
				if tt.kind == domain.ProviderAnthropic && r.Header.Get("Anthropic-Version") != "2023-06-01" {
					t.Error("missing Anthropic version")
				}
				_, _ = io.WriteString(w, tt.body)
			}))
			defer backend.Close()
			loopback, err := url.Parse(backend.URL)
			if err != nil {
				t.Fatal(err)
			}
			client := New(&http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.String() != tt.endpoint {
					t.Errorf("wrong vendor endpoint: %s", r.URL)
				}
				r = r.Clone(r.Context())
				r.URL.Scheme, r.URL.Host = loopback.Scheme, loopback.Host
				return backend.Client().Transport.RoundTrip(r)
			})})
			access := domain.ProviderTestAccess{
				Provider: domain.Provider{Kind: tt.kind, ProviderSettings: domain.ProviderSettings{Model: model, TimeoutSeconds: 2}},
				APIKey:   testKey, Login: login, Password: password,
			}
			if got, receipt := client.Test(context.Background(), access); got != "ok" || receipt.HTTPStatus != 200 {
				t.Fatalf("connection check = %s", got)
			}
		})
	}
}

func TestConnectionFailuresAreSanitizedAndRedirectsStayUnfollowed(t *testing.T) {
	for _, tt := range []struct {
		name, kind string
		status     int
		body, want string
	}{
		{"bad key", domain.ProviderOpenAI, 401, "secret from upstream", "invalid_credentials"},
		{"denied", domain.ProviderAnthropic, 403, "secret from upstream", "access_denied"},
		{"missing model", domain.ProviderOpenAI, 404, "secret from upstream", "model_not_found"},
		{"upstream limit", domain.ProviderGemini, 429, "", "upstream_rate_limited"},
		{"gemini bad key", domain.ProviderGemini, 400, `{"error":{"details":[{"reason":"API_KEY_INVALID"}]}}`, "invalid_credentials"},
		{"gemini embedding", domain.ProviderGemini, 200, `{"name":"models/test-model","supportedGenerationMethods":["embedContent"]}`, "model_unsupported"},
		{"deepseek model missing", domain.ProviderDeepSeek, 200, `{"object":"list","data":[{"id":"other"}]}`, "model_not_found"},
		{"data login invalid", domain.ProviderDataForSEO, 200, `{"status_code":40100}`, "invalid_credentials"},
		{"data IP denied", domain.ProviderDataForSEO, 200, `{"status_code":20000,"tasks":[{"status_code":40207}]}`, "address_not_allowed"},
		{"data ambiguous success", domain.ProviderDataForSEO, 200, `{"status_code":20000}`, "unexpected_response"},
		{"redirect", domain.ProviderOpenAI, 307, "", "unexpected_response"},
		{"too large", domain.ProviderOpenAI, 200, strings.Repeat("x", maxResponse+1), "unexpected_response"},
		{"provider down", domain.ProviderOpenAI, 503, "", "unavailable"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			client := New(&http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				return &http.Response{Request: r, StatusCode: tt.status,
					Header: http.Header{"Location": {"https://untrusted.example.test/"}},
					Body:   io.NopCloser(strings.NewReader(tt.body))}, nil
			})})
			access := domain.ProviderTestAccess{Provider: domain.Provider{Kind: tt.kind,
				ProviderSettings: domain.ProviderSettings{Model: model, TimeoutSeconds: 1}}}
			if got, _ := client.Test(context.Background(), access); got != tt.want || calls != 1 {
				t.Fatalf("check = %q, calls = %d, want %q and one call", got, calls, tt.want)
			}
		})
	}
}

func TestProviderCheckTimeout(t *testing.T) {
	client := New(&http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		<-r.Context().Done()
		return nil, r.Context().Err()
	})})
	access := domain.ProviderTestAccess{Provider: domain.Provider{Kind: domain.ProviderOpenAI,
		ProviderSettings: domain.ProviderSettings{Model: model, TimeoutSeconds: 1}}}
	if got, _ := client.Test(context.Background(), access); got != "timeout" {
		t.Fatalf("check = %s", got)
	}
}
