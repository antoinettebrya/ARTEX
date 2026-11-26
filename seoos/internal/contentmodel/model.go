// Package contentmodel adapts the configured editorial models to one JSON contract.
package contentmodel

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Autumn-27/artex/seoos/internal/domain"
)

const maxResponse = 256 << 10

type Client struct{ http http.Client }

func New(client *http.Client) *Client {
	if client == nil {
		client = http.DefaultClient
	}
	secure := *client
	secure.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{http: secure}
}

func endpoint(kind string) string {
	switch kind {
	case domain.ProviderOpenAI:
		return "https://api.openai.com/v1/responses"
	case domain.ProviderAnthropic:
		return "https://api.anthropic.com/v1/chat/completions"
	case domain.ProviderGemini:
		return "https://generativelanguage.googleapis.com/v1beta/openai/chat/completions"
	case domain.ProviderDeepSeek:
		return "https://api.deepseek.com/chat/completions"
	}
	return ""
}

type modelMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type modelInput struct {
	Stage    string          `json:"stage"`
	Language string          `json:"language"`
	Data     json.RawMessage `json:"data"`
}

func buildPayload(req domain.ContentModelRequest, call domain.ContentModelReservation) ([]byte, error) {
	input, err := json.Marshal(modelInput{Stage: req.Stage, Language: req.Language, Data: req.Input})
	if err != nil {
		return nil, err
	}
	system := req.System
	if call.Profile.SystemPrompt != "" {
		system += "\nEditorial preferences for this provider:\n" + call.Profile.SystemPrompt
	}
	if call.Provider.Kind == domain.ProviderOpenAI {
		return json.Marshal(map[string]any{
			"model": call.Provider.Model, "store": false, "max_output_tokens": call.Provider.MaxOutputTokens,
			"instructions": system, "input": string(input), "temperature": call.Profile.Temperature,
		})
	}
	return json.Marshal(map[string]any{
		"model": call.Provider.Model, "messages": []modelMessage{{Role: "system", Content: system}, {Role: "user", Content: string(input)}},
		"max_tokens": call.Provider.MaxOutputTokens, "temperature": call.Profile.Temperature,
	})
}

func fail(code string, retry, unbilled bool) *domain.DataError {
	return &domain.DataError{Code: code, Retry: retry, KnownUnbilled: unbilled}
}

func (c *Client) Generate(ctx context.Context, req domain.ContentModelRequest, call domain.ContentModelReservation) (domain.ContentModelResponse, *domain.DataError) {
	var out domain.ContentModelResponse
	target := endpoint(call.Provider.Kind)
	if ctx.Err() != nil || target == "" {
		return out, fail("content_model_unavailable", false, true)
	}
	body, err := buildPayload(req, call)
	if err != nil {
		return out, fail("content_model_invalid_request", false, true)
	}
	timeout := time.Duration(call.Provider.TimeoutSeconds) * time.Second
	ctx, cancel := context.WithTimeout(ctx, min(timeout, 15*time.Second))
	defer cancel()
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return out, fail("content_model_invalid_request", false, true)
	}
	r.Header.Set("Authorization", "Bearer "+call.APIKey)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "application/json")
	if call.Provider.Kind == domain.ProviderOpenAI {
		r.Header.Set("X-Client-Request-Id", call.ID)
	}
	reply, err := c.http.Do(r)
	if err != nil {
		return out, fail("content_model_transport_unknown", false, false)
	}
	defer reply.Body.Close()
	if reply.StatusCode == http.StatusTooManyRequests {
		result := fail("content_model_rate_limited", true, true)
		result.RetryAfter = modelRetryAfter(reply.Header.Get("Retry-After"))
		return out, result
	}
	if reply.StatusCode != 200 {
		unbilled := reply.StatusCode == http.StatusBadRequest || reply.StatusCode == http.StatusUnauthorized ||
			reply.StatusCode == http.StatusForbidden || reply.StatusCode == http.StatusNotFound
		return out, fail("content_model_rejected", false, unbilled)
	}
	raw, err := io.ReadAll(io.LimitReader(reply.Body, maxResponse+1))
	if err != nil || len(raw) > maxResponse {
		return out, fail("content_model_response_limit", false, false)
	}
	if call.APIKey != "" && bytes.Contains(raw, []byte(call.APIKey)) {
		return out, fail("content_model_reflected_credential", false, false)
	}
	if call.Provider.Kind == domain.ProviderOpenAI {
		out, err = parseResponses(raw)
	} else {
		out, err = parseChat(raw)
	}
	if err != nil {
		return out, fail("content_model_invalid_response", false, false)
	}
	out.Text = strings.TrimSpace(out.Text)
	out.ReceiptID = call.ID
	if out.Model == "" {
		out.Model = call.Provider.Model
	}
	if out.InputTokens < 0 || out.OutputTokens < 0 || out.InputTokens > 2000000 || out.OutputTokens > 2000000 {
		return domain.ContentModelResponse{}, fail("content_model_invalid_usage", false, false)
	}
	if out.Text == "" || strings.ContainsRune(out.Text, 0) || len(out.Text) > 40000 {
		out.Text = ""
		return out, fail("content_model_empty_output", false, false)
	}
	if call.APIKey != "" && strings.Contains(out.Text, call.APIKey) {
		out.Text = ""
		return out, fail("content_model_reflected_credential", false, false)
	}
	return out, nil
}

func modelRetryAfter(raw string) time.Duration {
	if seconds, err := strconv.ParseInt(raw, 10, 32); err == nil && seconds >= 0 {
		return min(time.Duration(seconds)*time.Second, 12*time.Hour)
	}
	if at, err := http.ParseTime(raw); err == nil {
		return min(max(time.Until(at), 0), 12*time.Hour)
	}
	return 0
}

func parseResponses(raw []byte) (domain.ContentModelResponse, error) {
	var response struct {
		Model  string `json:"model"`
		Status string `json:"status"`
		Usage  struct {
			Input  int64 `json:"input_tokens"`
			Output int64 `json:"output_tokens"`
		} `json:"usage"`
		Output []struct {
			Type     string `json:"type"`
			Contents []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
	}
	err := json.Unmarshal(raw, &response)
	var text strings.Builder
	if response.Status == "completed" {
		for _, part := range response.Output {
			if part.Type != "message" {
				continue
			}
			for _, content := range part.Contents {
				if content.Type == "output_text" {
					text.WriteString(content.Text)
				}
			}
		}
	}
	return domain.ContentModelResponse{Text: text.String(), Model: response.Model, InputTokens: response.Usage.Input, OutputTokens: response.Usage.Output}, err
}

func parseChat(raw []byte) (domain.ContentModelResponse, error) {
	var response struct {
		Model   string `json:"model"`
		Choices []struct {
			End     string       `json:"finish_reason"`
			Message modelMessage `json:"message"`
		} `json:"choices"`
		Usage struct {
			Input  int64 `json:"prompt_tokens"`
			Output int64 `json:"completion_tokens"`
		} `json:"usage"`
	}
	err := json.Unmarshal(raw, &response)
	out := domain.ContentModelResponse{Model: response.Model, InputTokens: response.Usage.Input, OutputTokens: response.Usage.Output}
	if len(response.Choices) > 0 && response.Choices[0].End == "stop" {
		out.Text = response.Choices[0].Message.Content
	}
	return out, err
}
