package worker

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/Autumn-27/artex/seoos/internal/contentmodel"
	"github.com/Autumn-27/artex/seoos/internal/domain"
	"github.com/Autumn-27/artex/seoos/internal/researchengine"
)

type routedModelFixture struct {
	target    *url.URL
	transport http.RoundTripper
}

func (f routedModelFixture) RoundTrip(r *http.Request) (*http.Response, error) {
	next := r.Clone(r.Context())
	next.URL = f.target
	return f.transport.RoundTrip(next)
}

func fixtureModel(t *testing.T, server *httptest.Server, path string) openAI {
	t.Helper()
	target, err := url.Parse(server.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	return openAI{models: contentmodel.New(&http.Client{Transport: routedModelFixture{target, server.Client().Transport}})}
}

func TestOpenAIResponsesContract(t *testing.T) {
	call := domain.ProviderCall{
		Provider: domain.Provider{Kind: domain.ProviderOpenAI, ProviderSettings: domain.ProviderSettings{Model: "test-model", MaxOutputTokens: 256, TimeoutSeconds: 2}},
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
	outcome := fixtureModel(t, server, "/v1/responses").generate(context.Background(), site, call)
	var got briefResult
	if err := json.Unmarshal(outcome.Result, &got); err != nil || outcome.Code != "" || got.Text != "A test brief" || got.OutputTokens != 6 {
		t.Fatalf("wrong result: %s, %s, %v", outcome.Code, outcome.Result, err)
	}
}

func TestOpenAIIntentCorroboratesObservedEvidence(t *testing.T) {
	call := domain.ProviderCall{APIKey: "fixture-only-intent-credential", Provider: domain.Provider{
		Kind:             domain.ProviderOpenAI,
		ProviderSettings: domain.ProviderSettings{Model: "fixture-model", MaxOutputTokens: 256, TimeoutSeconds: 2},
	}}
	evidence := domain.KeywordEvidence{Term: "best robot vacuum comparison", SERP: []domain.SERPEntry{
		{Rank: 1, URL: "https://example.com/review", Title: "Best robot vacuums", PageType: "comparison", Description: "DETAIL_NOT_SENT"},
		{Rank: 2, URL: "https://example.com/vacuums", Title: "Compare robot vacuums", PageType: "comparison"},
	}, Pages: []domain.PageEvidence{{Description: "CONTENT_NOT_SENT"}}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var data struct {
			Input string `json:"input"`
			Store bool   `json:"store"`
		}
		if err := json.NewDecoder(r.Body).Decode(&data); err != nil || data.Store ||
			!strings.Contains(data.Input, "Best robot vacuums") || strings.Contains(data.Input, "_NOT_SENT") {
			t.Error("intent provider received something other than bounded ranked evidence")
		}
		_, _ = w.Write([]byte(`{"id":"fixture_intent","status":"completed","model":"fixture-model","output":[{"type":"message","content":[{"type":"output_text","text":"{\"primary\":\"transactional\",\"confidence\":1}"}]}],"usage":{"input_tokens":5,"output_tokens":3}}`))
	}))
	defer server.Close()
	market := domain.Market{CountryCode: "US", LocationCode: 2840, LanguageCode: "en", SearchEngine: "google", Device: "desktop"}
	signal, outcome := fixtureModel(t, server, "/").intent(context.Background(), evidence, market, call)
	if outcome.Code != "" || signal == nil || signal.Primary != "transactional" || len(signal.Sources) != 2 {
		t.Fatalf("failed to parse auxiliary signal: %#v, %#v", signal, outcome)
	}
	if intent := researchengine.Classify(evidence, signal); intent.Primary != "commercial" {
		t.Fatalf("model displaced the observed SERP and keyword evidence: %#v", intent)
	}
}

func TestOpenAIFailuresAndRedirect(t *testing.T) {
	for _, test := range []struct {
		status int
		code   string
		retry  bool
	}{
		{http.StatusUnauthorized, "content_model_unauthorized", false},
		{http.StatusTooManyRequests, "content_model_rate_limited", true},
		{http.StatusServiceUnavailable, "content_model_upstream_unavailable", false},
		{http.StatusTemporaryRedirect, "content_model_rejected", false},
		{http.StatusOK, "content_model_invalid_response", false},
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
			outcome := fixtureModel(t, server, "/").generate(context.Background(), domain.Site{}, domain.ProviderCall{
				Provider: domain.Provider{Kind: domain.ProviderOpenAI, ProviderSettings: domain.ProviderSettings{TimeoutSeconds: 2}},
				APIKey:   "fixture-only-error-credential",
			})
			if outcome.Code != test.code || outcome.Retry != test.retry {
				t.Fatalf("wrong outcome %#v", outcome)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	outcome := (openAI{models: contentmodel.New(nil)}).generate(ctx, domain.Site{}, domain.ProviderCall{})
	if outcome.Code != "content_model_cancelled" || outcome.Retry {
		t.Fatalf("cancelled request: %#v", outcome)
	}
}
