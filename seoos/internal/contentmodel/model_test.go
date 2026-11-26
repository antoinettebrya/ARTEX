package contentmodel

import (
	"context"
	"encoding/json"
	"io"
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

func TestCompatibleBaseURLHeaderAndStructuredResponse(t *testing.T) {
	const secret = "fixture-custom-private-key"
	p := domain.Provider{Kind: domain.ProviderCompatible, ProviderSettings: domain.ProviderSettings{
		Model: "tenant/model", TimeoutSeconds: 2, MaxOutputTokens: 4096,
		ModelOptions: &domain.ModelOptions{
			BaseURL: "https://api.example.com/gateway/v1", Protocol: domain.ModelChat,
			AuthHeader: "X-Vendor-Key", AuthMode: "raw", TokenField: "max_completion_tokens",
			Output: domain.ModelSchema, Reasoning: "high", Temperature: "on",
		},
	}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Vendor-Key") != secret || r.Header.Get("Authorization") != "" ||
			r.URL.Path != "/gateway/v1/chat/completions" {
			t.Error("wrong custom model route or credential header")
		}
		var payload map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		format := string(payload["response_format"])
		if string(payload["max_completion_tokens"]) != "4096" || payload["max_tokens"] != nil ||
			payload["temperature"] != nil || string(payload["reasoning_effort"]) != `"high"` ||
			!strings.Contains(format, `"strict":true`) || !strings.Contains(format, `"additionalProperties":false`) {
			t.Error("wrong capability-dependent Chat Completions parameters")
		}
		w.Header().Set("X-Request-Id", "req_test_safe")
		_, _ = io.WriteString(w, `{"id":"reply-1","choices":[{"finish_reason":"stop","message":{"content":"{\"passed\":true,\"intent\":true,\"language\":true,\"original_value\":true,\"issues\":[]}"}}],"usage":{"prompt_tokens":1000,"completion_tokens":201,"prompt_tokens_details":{"cached_tokens":300}}}`)
	}))
	defer srv.Close()
	client := fixtureClient(t, srv, p.Options().BaseURL+"/chat/completions")
	out, problem := client.Generate(t.Context(), domain.ContentModelRequest{Stage: "review", Input: json.RawMessage(`{}`)},
		domain.ContentModelReservation{Provider: p, APIKey: secret})
	if problem != nil || out.Diagnostic.RequestID != "req_test_safe" || out.Diagnostic.HTTPStatus != 200 ||
		out.Diagnostic.CachedTokens == nil || *out.Diagnostic.CachedTokens != 300 || out.OutputTokens != 201 {
		t.Fatalf("incorrect sanitized result: %#v, %v", out, problem)
	}
}

func TestResponsesParameterCompatibility(t *testing.T) {
	for _, tc := range []struct {
		kind, model, effort string
		wantTemperature     bool
	}{
		{domain.ProviderOpenAI, "gpt-4.1-mini", "", true},
		{domain.ProviderOpenAI, "gpt-5", "", false},
		{domain.ProviderOpenAI, "o3-mini", "high", false},
		{domain.ProviderOpenAI, "gpt-5.2", "none", true},
		{domain.ProviderOpenAI, "gpt-5.2", "low", false},
		{domain.ProviderAnthropic, "claude-opus-4-7", "", false},
		{domain.ProviderDeepSeek, "deepseek-v4-pro", "high", false},
	} {
		t.Run(tc.model+tc.effort, func(t *testing.T) {
			options := &domain.ModelOptions{Reasoning: tc.effort}
			if tc.effort == "none" {
				options.Temperature = "on"
			}
			call := domain.ContentModelReservation{Provider: domain.Provider{Kind: tc.kind, ProviderSettings: domain.ProviderSettings{
				Model: tc.model, ModelOptions: options, MaxOutputTokens: 4096,
			}}}
			raw, err := buildPayload(domain.ContentModelRequest{Stage: "outline", Input: json.RawMessage(`{}`)}, call)
			var wire map[string]json.RawMessage
			if err != nil || json.Unmarshal(raw, &wire) != nil || (wire["temperature"] != nil) != tc.wantTemperature {
				t.Fatalf("wrong sampling parameters %s, %v", raw, err)
			}
			if tc.kind == domain.ProviderOpenAI && (wire["text"] == nil || wire["response_format"] != nil) {
				t.Fatal("Responses strict schema used the wrong envelope")
			}
		})
	}
}

func TestProviderStatusMatrixKeepsOnlySafeDiagnostics(t *testing.T) {
	const secret = "fixture-credential-azure"
	call := domain.ContentModelReservation{Provider: domain.Provider{Kind: domain.ProviderOpenAI,
		ProviderSettings: domain.ProviderSettings{Model: "gpt-5", TimeoutSeconds: 2}}, APIKey: secret}
	for _, tc := range []struct {
		status              int
		category            string
		retry, mayBeCharged bool
	}{
		{400, "request", false, false}, {401, "authentication", false, false},
		{403, "permission", false, false}, {408, "timeout", true, true},
		{429, "rate_limit", true, false}, {500, "upstream", true, true},
		{502, "upstream", true, true}, {503, "upstream", true, true},
	} {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("X-Request-Id", secret)
				w.Header().Set("Request-Id", "req_safe")
				w.Header().Set("Retry-After", "2")
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, `{"error":{"code":"unsupported_parameter","param":"temperature","message":"`+secret+`"}}`)
			}))
			defer srv.Close()
			out, problem := fixtureClient(t, srv, endpoint(call.Provider.Kind)).Generate(t.Context(),
				domain.ContentModelRequest{Stage: "outline", Input: json.RawMessage(`{}`)}, call)
			receipt := out.Diagnostic
			if problem == nil || receipt.HTTPStatus != tc.status || receipt.Category != tc.category ||
				receipt.Retryable != tc.retry || receipt.MayBeCharged != tc.mayBeCharged ||
				receipt.RequestID != "req_safe" || receipt.UpstreamCode != "unsupported_parameter" ||
				receipt.Parameter != "temperature" || problem.Retry != (tc.status == 429) {
				t.Fatalf("unexpected metadata: %#v, %v", receipt, problem)
			}
			raw, _ := json.Marshal(receipt)
			if strings.Contains(string(raw), secret) {
				t.Fatal("diagnostic exposed vendor credential")
			}
		})
	}
}

func TestConnectedTimeoutAndDisconnectAreNotReplayed(t *testing.T) {
	for _, disconnect := range []bool{false, true} {
		name, expected := "timeout", "content_model_timeout"
		if disconnect {
			name, expected = "disconnect", "content_model_transport"
		}
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				if disconnect {
					conn, _, err := w.(http.Hijacker).Hijack()
					if err != nil {
						t.Error(err)
						return
					}
					_ = conn.Close()
					return
				}
				<-r.Context().Done()
			}))
			defer srv.Close()
			p := domain.Provider{Kind: domain.ProviderOpenAI, ProviderSettings: domain.ProviderSettings{Model: "gpt-5", TimeoutSeconds: 1}}
			out, problem := fixtureClient(t, srv, endpoint(p.Kind)).Generate(t.Context(),
				domain.ContentModelRequest{Stage: "review", Input: json.RawMessage(`{}`)},
				domain.ContentModelReservation{Provider: p, APIKey: "fixture-credential-only"})
			if problem == nil || problem.Code != expected || problem.Retry || !out.Diagnostic.MayBeCharged {
				t.Fatalf("in-flight response loss was unsafe: %v, %#v", problem, out.Diagnostic)
			}
		})
	}
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
		{503, "{}", "content_model_upstream_unavailable", false},
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
