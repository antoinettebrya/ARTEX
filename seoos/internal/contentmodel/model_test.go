package contentmodel

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Autumn-27/artex/seoos/internal/domain"
)

type testTransport func(*http.Request) (*http.Response, error)

func (f testTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func fixtureClient(t *testing.T, server *httptest.Server, target string) *Client {
	t.Helper()
	local, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	client := server.Client()
	transport := client.Transport
	client.Transport = testTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != target {
			t.Errorf("unexpected configured endpoint %s", r.URL)
		}
		next := r.Clone(r.Context())
		next.URL.Scheme, next.URL.Host = local.Scheme, local.Host
		return transport.RoundTrip(next)
	})
	return New(client)
}

func TestOfficialModelHTTPContracts(t *testing.T) {
	for _, kind := range []string{domain.ProviderOpenAI, domain.ProviderAnthropic, domain.ProviderGemini, domain.ProviderDeepSeek} {
		t.Run(kind, func(t *testing.T) {
			call := domain.ContentModelReservation{ID: "test-reservation", APIKey: "test-contract-only-credential",
				Provider: domain.Provider{Kind: kind, ProviderSettings: domain.ProviderSettings{Model: "contract-model", MaxOutputTokens: 2048, TimeoutSeconds: 4}},
				Profile:  domain.ContentModelProfile{SystemPrompt: "editor profile", Temperature: .2}}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "POST" || r.Header.Get("Authorization") != "Bearer "+call.APIKey {
					t.Error("missing model auth")
				}
				var req map[string]json.RawMessage
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Error(err)
					w.WriteHeader(400)
					return
				}
				if kind == domain.ProviderOpenAI {
					if string(req["store"]) != "false" || string(req["max_output_tokens"]) != "2048" || r.Header.Get("X-Client-Request-Id") != call.ID ||
						!strings.Contains(string(req["instructions"]), "editor profile") {
						t.Error("responses request did not follow contract")
					}
					_, _ = w.Write([]byte(`{"status":"completed","model":"contract-model","output":[{"type":"message","content":[{"type":"output_text","text":"{}"}]}],"usage":{"input_tokens":9,"output_tokens":2}}`))
				} else {
					if string(req["max_tokens"]) != "2048" || !strings.Contains(string(req["messages"]), "editor profile") ||
						!strings.Contains(string(req["messages"]), "keyword") {
						t.Error("chat request did not follow contract")
					}
					_, _ = w.Write([]byte(`{"model":"contract-model","choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"{}"}}],"usage":{"prompt_tokens":9,"completion_tokens":2}}`))
				}
			}))
			defer server.Close()
			client := fixtureClient(t, server, endpoint(kind))
			result, err := client.Generate(context.Background(), domain.ContentModelRequest{Stage: "outline", Language: "ja", System: "contract instruction", Input: json.RawMessage(`{"keyword":"フィルター"}`)}, call)
			if err != nil || result.Text != "{}" || result.InputTokens != 9 || result.OutputTokens != 2 || result.ReceiptID != call.ID {
				t.Fatalf("unexpected response %#v %v", result, err)
			}
		})
	}
}

func TestModelLimitsAndCredentialReflections(t *testing.T) {
	call := domain.ContentModelReservation{APIKey: "fixture-credential-for-tests",
		Provider: domain.Provider{Kind: domain.ProviderOpenAI, ProviderSettings: domain.ProviderSettings{Model: "contract-model", TimeoutSeconds: 4, MaxOutputTokens: 100}}}
	for _, test := range []struct {
		status     int
		body, code string
		retry      bool
	}{
		{429, "{}", "content_model_rate_limited", true},
		{503, "{}", "content_model_rejected", false},
		{302, "{}", "content_model_rejected", false},
		{200, call.APIKey, "content_model_reflected_credential", false},
		{200, strings.Repeat("x", maxResponse+1), "content_model_response_limit", false},
	} {
		t.Run(test.code+http.StatusText(test.status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Retry-After", "7")
				w.Header().Set("Location", "/redirection-must-not-be-followed")
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(test.body))
			}))
			defer server.Close()
			_, err := fixtureClient(t, server, endpoint(call.Provider.Kind)).Generate(context.Background(), domain.ContentModelRequest{Input: json.RawMessage(`{}`)}, call)
			if err == nil || err.Code != test.code || err.Retry != test.retry ||
				err.Retry && (err.RetryAfter != 7*time.Second || !err.KnownUnbilled) {
				t.Fatalf("unexpected error %v", err)
			}
		})
	}
}
