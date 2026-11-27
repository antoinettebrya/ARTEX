package contentmodel

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Autumn-27/artex/seoos/internal/domain"
)

type anthropicFixture func(*http.Request) (*http.Response, error)

func (f anthropicFixture) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestClaudeMessagesWireUsageAndSchema(t *testing.T) {
	req := domain.ContentModelRequest{Stage: "review", System: "Review the draft.", Input: json.RawMessage(`{}`)}
	p := domain.Provider{Kind: domain.ProviderAnthropic, ProviderSettings: domain.ProviderSettings{
		Model: "claude-sonnet-4-6", TimeoutSeconds: 2, MaxOutputTokens: 256,
		ModelOptions: &domain.ModelOptions{Protocol: domain.ModelAnthropic, Output: domain.ModelSchema, WorkspaceID: "wrkspc_fixture"},
	}}
	client := New(&http.Client{Transport: anthropicFixture(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != "https://api.anthropic.com/v1/messages" || r.Header.Get("X-Api-Key") != "fixture-anthropic-key" ||
			r.Header.Get("Anthropic-Version") != "2023-06-01" || r.Header.Get("Authorization") != "" {
			t.Fatalf("wrong Claude auth or endpoint: %s", r.URL.Hostname())
		}
		var body map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body["output_config"] == nil || body["response_format"] != nil ||
			body["system"] == nil || body["temperature"] != nil {
			t.Fatalf("wrong native request: %v", body)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Request-Id": {"req-native-fixture"}},
			Body: io.NopCloser(strings.NewReader(`{"id":"msg_abc","model":"claude-sonnet-4-6","stop_reason":"end_turn","content":[{"type":"thinking","text":"ignored"},{"type":"text","text":"{\"passed\":true,\"intent\":true,\"language\":true,\"original_value\":true,\"issues\":[]}"}],"usage":{"input_tokens":200,"cache_creation_input_tokens":100,"cache_read_input_tokens":300,"output_tokens":24}}`))}, nil
	})})
	out, problem := client.Generate(context.Background(), req, domain.ContentModelReservation{Provider: p, APIKey: "fixture-anthropic-key"})
	if problem != nil || !out.UsageReported || out.InputTokens != 600 || out.OutputTokens != 24 ||
		out.Diagnostic.CachedTokens == nil || *out.Diagnostic.CachedTokens != 300 || out.Diagnostic.RequestID != "req-native-fixture" {
		t.Fatalf("wrong Claude receipt: %+v %v", out, problem)
	}
}
