package worker

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Autumn-27/artex/seoos/internal/domain"
)

func TestOpenAIResponsesContract(t *testing.T) {
	call := domain.ProviderCall{
		Provider: domain.Provider{ProviderSettings: domain.ProviderSettings{Model: "test-model", MaxOutputTokens: 256, TimeoutSeconds: 2}},
		APIKey:   "fixture-only-test-credential",
	}
	site := domain.Site{Name: "Editorial", CountryCode: "JP", LanguageTag: "ja", CMS: "wordpress", BaseURL: "https://reserved.example.test"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/responses" || r.Header.Get("Authorization") != "Bearer "+call.APIKey {
			t.Error("provider request method, path, or authorization did not match")
		}
		var req struct {
			Model           string          `json:"model"`
			Input           string          `json:"input"`
			Store           *bool           `json:"store"`
			MaxOutputTokens int             `json:"max_output_tokens"`
			Tools           json.RawMessage `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Model != "test-model" ||
			req.Store == nil || *req.Store || req.MaxOutputTokens != 256 || len(req.Tools) > 0 ||
			!strings.Contains(req.Input, `"language":"ja"`) || strings.Contains(req.Input, site.BaseURL) {
			t.Error("provider request did not apply the expected data and output limits")
		}
		_, _ = w.Write([]byte(`{"id":"response_fixture","status":"completed","model":"test-model","output":[{"type":"message","content":[{"type":"output_text","text":"A test brief"}]}],"usage":{"input_tokens":10,"output_tokens":6}}`))
	}))
	defer server.Close()
	outcome := (openAI{endpoint: server.URL + "/v1/responses"}).generate(context.Background(), site, call)
	var got briefResult
	if err := json.Unmarshal(outcome.Result, &got); err != nil || outcome.Code != "" || got.Text != "A test brief" || got.OutputTokens != 6 {
		t.Fatalf("wrong result: %s, %s, %v", outcome.Code, outcome.Result, err)
	}
}

func TestOpenAIFailuresAndRedirect(t *testing.T) {
	for _, test := range []struct {
		status int
		code   string
		retry  bool
	}{
		{http.StatusUnauthorized, "provider_authentication", false},
		{http.StatusTooManyRequests, "provider_retryable", true},
		{http.StatusServiceUnavailable, "provider_retryable", true},
		{http.StatusTemporaryRedirect, "provider_rejected", false},
		{http.StatusOK, "provider_response_invalid", false},
	} {
		t.Run(test.code+http.StatusText(test.status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/" {
					t.Error("adapter followed a redirect")
				}
				w.Header().Set("Location", "/unexpected")
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte("fixture failure"))
			}))
			defer server.Close()
			outcome := (openAI{endpoint: server.URL}).generate(context.Background(), domain.Site{}, domain.ProviderCall{
				Provider: domain.Provider{ProviderSettings: domain.ProviderSettings{TimeoutSeconds: 2}},
			})
			if outcome.Code != test.code || outcome.Retry != test.retry {
				t.Fatalf("wrong outcome %#v", outcome)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	outcome := (openAI{endpoint: "http://127.0.0.1:1"}).generate(ctx, domain.Site{}, domain.ProviderCall{})
	if outcome.Code != "provider_transport" || !outcome.Retry {
		t.Fatalf("cancelled request: %#v", outcome)
	}
}
