// Package contentmodel adapts configured AI models to the editorial contract.
package contentmodel

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Autumn-27/artex/seoos/internal/contentinventory"
	"github.com/Autumn-27/artex/seoos/internal/domain"
	"github.com/Autumn-27/artex/seoos/internal/providerhttp"
)

const maxResponse = 256 << 10

type Client struct{ http http.Client }

func New(fixture *http.Client) *Client {
	client := contentinventory.NewPublicHTTPClient(30 * time.Second)
	// Generation may legitimately take longer than a crawl's header timeout.
	client.Transport.(*http.Transport).ResponseHeaderTimeout = 0
	if fixture != nil && fixture.Transport != nil {
		client.Transport = fixture.Transport
	}
	return &Client{http: *client}
}

func fail(code string, retry, unbilled bool) *domain.DataError {
	return &domain.DataError{Code: code, Retry: retry, KnownUnbilled: unbilled}
}

func (c *Client) Generate(ctx context.Context, req domain.ContentModelRequest, call domain.ContentModelReservation) (
	out domain.ContentModelResponse, problem *domain.DataError,
) {
	provider := call.Provider
	diagnostic := domain.ProviderReceipt{Provider: provider.Kind, Operation: provider.Model, CostSource: "unknown"}
	start := time.Now()
	defer func() {
		diagnostic.DurationMS = time.Since(start).Milliseconds()
		if problem != nil {
			diagnostic.Code = problem.Code
			diagnostic.Retryable = problem.Retry || providerhttp.IsTransient(diagnostic.HTTPStatus)
			diagnostic.MayBeCharged = !problem.KnownUnbilled
		} else {
			diagnostic.Category, diagnostic.MayBeCharged = "success", true
		}
		if out.UsageReported {
			diagnostic.InputTokens, diagnostic.OutputTokens = &out.InputTokens, &out.OutputTokens
		}
		out.Diagnostic = diagnostic
		out.ReceiptID = call.ID
	}()
	adapter := adapterFor(provider)
	target := adapter.endpoint
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		diagnostic.Category = "timeout"
		return out, fail("content_model_timeout", true, true)
	}
	if errors.Is(ctx.Err(), context.Canceled) {
		diagnostic.Category = "cancelled"
		return out, fail("content_model_cancelled", false, true)
	}
	if !strings.HasPrefix(target, "https://") || call.APIKey == "" {
		diagnostic.Category = "request"
		return out, fail("content_model_unavailable", false, true)
	}
	body, err := buildPayload(req, call)
	if err != nil {
		diagnostic.Category = "request"
		return out, fail("content_model_invalid_request", false, true)
	}
	timeout := time.Duration(provider.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	requestCtx, cancel := context.WithTimeout(ctx, min(timeout, 30*time.Second))
	defer cancel()
	var connected atomic.Bool
	requestCtx = httptrace.WithClientTrace(requestCtx, &httptrace.ClientTrace{
		GotConn: func(httptrace.GotConnInfo) { connected.Store(true) },
	})
	r, err := http.NewRequestWithContext(requestCtx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		diagnostic.Category = "request"
		return out, fail("content_model_invalid_request", false, true)
	}
	provider.ModelAuthorization(r.Header, call.APIKey)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "application/json")
	if provider.Kind == domain.ProviderOpenAI && call.ID != "" {
		r.Header.Set("X-Client-Request-Id", call.ID)
	}
	reply, err := c.http.Do(r)
	if err != nil {
		beforeConnection := !connected.Load()
		code, category := "content_model_transport", "transport"
		var network net.Error
		switch {
		case errors.Is(err, domain.ErrCrawlUnauthorized):
			diagnostic.Category = "permission"
			return out, fail("content_model_destination_rejected", false, true)
		case errors.Is(requestCtx.Err(), context.DeadlineExceeded) || errors.As(err, &network) && network.Timeout():
			code, category = "content_model_timeout", "timeout"
		case errors.Is(requestCtx.Err(), context.Canceled):
			code, category = "content_model_cancelled", "cancelled"
		}
		diagnostic.Category = category
		return out, fail(code, beforeConnection && ctx.Err() == nil, beforeConnection)
	}
	defer reply.Body.Close()
	providerhttp.Response(&diagnostic, reply, call.APIKey, r.Header.Get(provider.Options().AuthHeader), r.Header.Get("Authorization"))
	diagnostic.Category = providerhttp.HTTPCategory(reply.StatusCode)
	if reply.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(reply.Body, (64<<10)+1))
		providerhttp.ErrorDetails(&diagnostic, raw, call.APIKey)
		if reply.StatusCode == http.StatusTooManyRequests && diagnostic.UpstreamCode == "insufficient_quota" {
			diagnostic.Category = "billing"
			return out, fail("content_model_billing", false, true)
		}
		return out, modelHTTPFailure(reply.StatusCode, reply.Header.Get("Retry-After"))
	}
	raw, err := io.ReadAll(io.LimitReader(reply.Body, maxResponse+1))
	if err != nil {
		diagnostic.Category = "transport"
		if requestCtx.Err() == context.DeadlineExceeded {
			diagnostic.Category = "timeout"
			return out, fail("content_model_timeout", false, false)
		}
		return out, fail("content_model_response_read", false, false)
	}
	if len(raw) > maxResponse {
		diagnostic.Category = "response"
		return out, fail("content_model_response_limit", false, false)
	}
	parsed, err := adapter.decode(raw)
	if err != nil {
		diagnostic.Category = "response"
		if bytes.Contains(raw, []byte(call.APIKey)) {
			return out, fail("content_model_reflected_credential", false, false)
		}
		return out, fail("content_model_invalid_response", false, false)
	}
	out = parsed.response
	out.Model = providerhttp.Identifier(out.Model, call.APIKey)
	if out.Model == "" {
		out.Model = provider.Model
	}
	out.ResponseID = providerhttp.Identifier(out.ResponseID, call.APIKey)
	diagnostic.CachedTokens = parsed.cached
	if diagnostic.RequestID == "" {
		diagnostic.RequestID = out.ResponseID
	}
	if parsed.badUsage {
		diagnostic.Category = "response"
		out.Text = ""
		return out, fail("content_model_invalid_usage", false, false)
	}
	if bytes.Contains(raw, []byte(call.APIKey)) || strings.Contains(out.Text, call.APIKey) {
		diagnostic.Category = "response"
		out.Text = ""
		return out, fail("content_model_reflected_credential", false, false)
	}
	if parsed.code != "" {
		diagnostic.Category = parsed.category
		diagnostic.UpstreamCode = providerhttp.Identifier(parsed.upstream, call.APIKey)
		out.Text = ""
		return out, fail(parsed.code, false, false)
	}
	out.Text = strings.TrimSpace(out.Text)
	if out.Text == "" || strings.ContainsRune(out.Text, 0) || len(out.Text) > 40000 {
		diagnostic.Category = "response"
		out.Text = ""
		return out, fail("content_model_empty_output", false, false)
	}
	if schema := schemaFor(req.Stage); schema != nil && outputFormat(provider, req.Stage) != domain.ModelText {
		if normal, valid := JSONObject(out.Text); valid {
			out.Text = normal
		} else {
			diagnostic.Category, out.Text = "response", ""
			return out, fail("content_model_invalid_json_output", false, false)
		}
		if outputFormat(provider, req.Stage) == domain.ModelSchema {
			var value any
			if json.Unmarshal([]byte(out.Text), &value) != nil || !matchesSchema(schema, value) {
				diagnostic.Category, out.Text = "response", ""
				return out, fail("content_model_schema_mismatch", false, false)
			}
		}
	}
	if !out.UsageReported {
		diagnostic.Category, out.Text = "response", ""
		return out, fail("content_model_usage_missing", false, false)
	}
	return out, nil
}

func modelHTTPFailure(status int, retryAfter string) *domain.DataError {
	code := "content_model_rejected"
	switch status {
	case 401:
		code = "content_model_unauthorized"
	case 403:
		code = "content_model_forbidden"
	case 402:
		code = "content_model_billing"
	case 404:
		code = "content_model_not_found"
	case 408, 504:
		code = "content_model_timeout"
	case 429:
		result := fail("content_model_rate_limited", true, true)
		result.RetryAfter = providerhttp.RetryAfter(retryAfter, 12*time.Hour)
		return result
	default:
		if status >= 500 {
			code = "content_model_upstream_unavailable"
		}
	}
	// A gateway failure or timeout does not establish that inference did not run.
	return fail(code, false, status >= 400 && status < 500 && status != 408)
}

type parsedOutput struct {
	response       domain.ContentModelResponse
	cached         *int64
	code, category string
	upstream       string
	badUsage       bool
}

func (p *parsedOutput) usage(input, output, cached *int64) {
	if input == nil || output == nil {
		return
	}
	if *input < 0 || *output < 0 || *input > 2_000_000 || *output > 2_000_000 ||
		cached != nil && (*cached < 0 || *cached > *input) {
		p.badUsage = true
		return
	}
	p.response.InputTokens, p.response.OutputTokens, p.response.UsageReported = *input, *output, true
	p.cached = cached
}

func parseResponses(raw []byte) (parsedOutput, error) {
	var wire struct {
		ID, Model, Status string
		Error             struct{ Code string }
		Incomplete        struct{ Reason string } `json:"incomplete_details"`
		Usage             struct {
			Input        *int64 `json:"input_tokens"`
			Output       *int64 `json:"output_tokens"`
			InputDetails struct {
				Cached *int64 `json:"cached_tokens"`
			} `json:"input_tokens_details"`
		} `json:"usage"`
		Output []struct {
			Type    string
			Content []struct{ Type, Text string }
		}
	}
	err := json.Unmarshal(raw, &wire)
	p := parsedOutput{response: domain.ContentModelResponse{Model: wire.Model, ResponseID: wire.ID}}
	p.usage(wire.Usage.Input, wire.Usage.Output, wire.Usage.InputDetails.Cached)
	if wire.Status != "completed" {
		p.code, p.category, p.upstream = "content_model_incomplete", "response", wire.Error.Code
		if wire.Incomplete.Reason != "" {
			p.upstream = wire.Incomplete.Reason
		}
		return p, err
	}
	var text strings.Builder
	for _, message := range wire.Output {
		if message.Type != "message" {
			continue
		}
		for _, content := range message.Content {
			switch content.Type {
			case "output_text":
				text.WriteString(content.Text)
			case "refusal":
				p.code, p.category = "content_model_refused", "refusal"
			}
		}
	}
	p.response.Text = text.String()
	return p, err
}

func parseChat(raw []byte) (parsedOutput, error) {
	var wire struct {
		ID, Model string
		Choices   []struct {
			End     string                            `json:"finish_reason"`
			Message struct{ Content, Refusal string } `json:"message"`
		}
		Usage struct {
			Input        *int64 `json:"prompt_tokens"`
			Output       *int64 `json:"completion_tokens"`
			Hit          *int64 `json:"prompt_cache_hit_tokens"`
			InputDetails struct {
				Cached *int64 `json:"cached_tokens"`
			} `json:"prompt_tokens_details"`
		} `json:"usage"`
	}
	err := json.Unmarshal(raw, &wire)
	p := parsedOutput{response: domain.ContentModelResponse{Model: wire.Model, ResponseID: wire.ID}}
	cached := wire.Usage.InputDetails.Cached
	if cached == nil {
		cached = wire.Usage.Hit
	}
	p.usage(wire.Usage.Input, wire.Usage.Output, cached)
	if len(wire.Choices) == 0 {
		p.code, p.category = "content_model_invalid_response", "response"
		return p, err
	}
	choice := wire.Choices[0]
	switch {
	case choice.End == "content_filter" || choice.Message.Refusal != "":
		p.code, p.category, p.upstream = "content_model_refused", "refusal", choice.End
	case choice.End != "stop":
		p.code, p.category, p.upstream = "content_model_incomplete", "response", choice.End
	default:
		p.response.Text = choice.Message.Content
	}
	return p, err
}

// JSONObject accepts one object, optionally enclosed in a single Markdown fence.
func JSONObject(text string) (string, bool) {
	text = strings.TrimSpace(text)
	if first, rest, ok := strings.Cut(text, "\n"); ok && (first == "```" || strings.EqualFold(first, "```json")) {
		if body, closed := strings.CutSuffix(strings.TrimSpace(rest), "```"); closed {
			text = strings.TrimSpace(body)
		}
	}
	return text, strings.HasPrefix(text, "{") && json.Valid([]byte(text))
}
