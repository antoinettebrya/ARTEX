// Package providerprobe checks saved provider credentials through read-only vendor APIs.
package providerprobe

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/Autumn-27/artex/seoos/internal/contentinventory"
	"github.com/Autumn-27/artex/seoos/internal/domain"
	"github.com/Autumn-27/artex/seoos/internal/providerhttp"
)

const maxResponse = 1 << 20

type Client struct{ http http.Client }

func New(fixture *http.Client) *Client {
	client := *contentinventory.NewPublicHTTPClient(8 * time.Second)
	if fixture != nil {
		client = *fixture
	}
	client.Jar = nil
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{http: client}
}

func target(provider domain.Provider) string {
	model := url.PathEscape(provider.Model)
	switch provider.Kind {
	case domain.ProviderOpenAI:
		return "https://api.openai.com/v1/models/" + model
	case domain.ProviderAnthropic:
		return "https://api.anthropic.com/v1/models/" + model
	case domain.ProviderGemini:
		return "https://generativelanguage.googleapis.com/v1beta/models/" + model
	case domain.ProviderDeepSeek:
		return "https://api.deepseek.com/models"
	case domain.ProviderCompatible:
		return provider.Options().BaseURL + "/models"
	case domain.ProviderDataForSEO:
		return "https://api.dataforseo.com/v3/appendix/user_data"
	default:
		return ""
	}
}

func (c *Client) Test(ctx context.Context, access domain.ProviderTestAccess) (string, domain.ProviderReceipt) {
	provider := access.Provider
	operation := "models"
	if provider.Kind == domain.ProviderDataForSEO {
		operation = "appendix_user_data"
	}
	zero := int64(0)
	receipt := domain.ProviderReceipt{Provider: provider.Kind, Operation: operation, CostSource: "unbilled", CostMicros: &zero}
	start := time.Now()
	status := c.test(ctx, access, &receipt)
	receipt.DurationMS = time.Since(start).Milliseconds()
	if status == "ok" {
		receipt.Category = "success"
	} else {
		receipt.Code = status
		if receipt.HTTPStatus == 0 {
			receipt.Category = "transport"
		}
		switch status {
		case "timeout", "cancelled":
			receipt.Category = status
		case "invalid_credentials":
			receipt.Category = "authentication"
		case "model_not_found":
			receipt.Category = "not_found"
		case "access_denied", "address_not_allowed", "account_unverified":
			receipt.Category = "permission"
		case "upstream_rate_limited":
			receipt.Category = "rate_limit"
		case "insufficient_balance":
			receipt.Category = "billing"
		default:
			if receipt.Category == "success" {
				receipt.Category = "response"
			}
		}
		receipt.Retryable = status == "timeout" || status == "upstream_rate_limited" || status == "unavailable"
	}
	return status, receipt
}

func (c *Client) test(ctx context.Context, access domain.ProviderTestAccess, receipt *domain.ProviderReceipt) string {
	p := access.Provider
	ctx, cancel := context.WithTimeout(ctx, min(8*time.Second, time.Duration(p.TimeoutSeconds)*time.Second))
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target(p), nil)
	if err != nil || req.URL.Host == "" {
		return "unexpected_response"
	}
	req.Header.Set("Accept", "application/json")
	switch p.Kind {
	case domain.ProviderDataForSEO:
		req.SetBasicAuth(access.Login, access.Password)
	case domain.ProviderAnthropic:
		req.Header.Set("x-api-key", access.APIKey)
		req.Header.Set("anthropic-version", "2023-06-01")
		if workspace := p.Options().WorkspaceID; workspace != "" {
			req.Header.Set("Anthropic-Workspace-Id", workspace)
		}
	case domain.ProviderGemini:
		req.Header.Set("x-goog-api-key", access.APIKey)
	default:
		p.ModelAuthorization(req.Header, access.APIKey)
	}
	res, err := c.http.Do(req)
	if err != nil {
		return transportStatus(ctx, err)
	}
	defer res.Body.Close()
	providerhttp.Response(receipt, res, access.APIKey, access.Login, access.Password, req.Header.Get("Authorization"))
	receipt.Category = providerhttp.HTTPCategory(res.StatusCode)
	switch res.StatusCode {
	case http.StatusUnauthorized:
		return "invalid_credentials"
	case http.StatusPaymentRequired:
		return "insufficient_balance"
	case http.StatusTooManyRequests:
		return "upstream_rate_limited"
	case http.StatusNotFound:
		if p.Kind != domain.ProviderDataForSEO && p.Kind != domain.ProviderDeepSeek && p.Kind != domain.ProviderCompatible {
			return "model_not_found"
		}
	case http.StatusRequestTimeout, http.StatusGatewayTimeout:
		return "timeout"
	}
	if res.StatusCode == http.StatusOK ||
		p.Kind == domain.ProviderGemini && (res.StatusCode == http.StatusBadRequest || res.StatusCode == http.StatusForbidden) {
		body, err := io.ReadAll(io.LimitReader(res.Body, maxResponse+1))
		if err != nil {
			return transportStatus(ctx, err)
		}
		if len(body) > maxResponse {
			return "unexpected_response"
		}
		providerhttp.ErrorDetails(receipt, body, access.APIKey, access.Login, access.Password)
		return parseResponse(p, res.StatusCode, body)
	}
	if res.StatusCode == http.StatusForbidden {
		return "access_denied"
	}
	if res.StatusCode >= 500 {
		return "unavailable"
	}
	return "unexpected_response"
}

func transportStatus(ctx context.Context, err error) string {
	var network net.Error
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.As(err, &network) && network.Timeout() {
		return "timeout"
	}
	if errors.Is(ctx.Err(), context.Canceled) {
		return "cancelled"
	}
	return "unavailable"
}

func parseResponse(provider domain.Provider, httpStatus int, body []byte) string {
	// Only read known status/metadata fields. Vendor bodies can contain private account data.
	if provider.Kind == domain.ProviderDataForSEO {
		return dataForSEOStatus(body)
	}
	var payload struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Object string `json:"object"`
		Type   string `json:"type"`
		Data   []struct {
			ID string `json:"id"`
		} `json:"data"`
		Methods []string `json:"supportedGenerationMethods"`
		Error   struct {
			Details []struct {
				Reason string `json:"reason"`
			} `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "unexpected_response"
	}
	if httpStatus != http.StatusOK {
		for _, detail := range payload.Error.Details {
			if detail.Reason == "API_KEY_INVALID" {
				return "invalid_credentials"
			}
		}
		return "access_denied"
	}
	switch provider.Kind {
	case domain.ProviderOpenAI:
		if payload.Object == "model" && payload.ID != "" {
			return "ok"
		}
	case domain.ProviderAnthropic:
		if payload.Type == "model" && payload.ID != "" {
			return "ok"
		}
	case domain.ProviderGemini:
		if model, present := strings.CutPrefix(payload.Name, "models/"); present && model != "" {
			if slices.Contains(payload.Methods, "generateContent") {
				return "ok"
			}
			return "model_unsupported"
		}
	case domain.ProviderDeepSeek, domain.ProviderCompatible:
		if (payload.Object == "list" || provider.Kind == domain.ProviderCompatible) && payload.Data != nil {
			for _, model := range payload.Data {
				if model.ID == provider.Model {
					return "ok"
				}
			}
			return "model_not_found"
		}
	}
	return "unexpected_response"
}

func dataForSEOStatus(body []byte) string {
	var payload struct {
		Code  int `json:"status_code"`
		Tasks []struct {
			Code  int `json:"status_code"`
			Count int `json:"result_count"`
		} `json:"tasks"`
	}
	if json.Unmarshal(body, &payload) != nil {
		return "unexpected_response"
	}
	if payload.Code != 20000 {
		return dataForSEOFailure(payload.Code)
	}
	if len(payload.Tasks) != 1 {
		return "unexpected_response"
	}
	task := payload.Tasks[0]
	if task.Code != 20000 {
		return dataForSEOFailure(task.Code)
	}
	if task.Count < 1 {
		return "unexpected_response"
	}
	return "ok"
}

func dataForSEOFailure(code int) string {
	switch code {
	case 40100:
		return "invalid_credentials"
	case 40104:
		return "account_unverified"
	case 40200, 40210:
		return "insufficient_balance"
	case 40201:
		return "access_denied"
	case 40202, 40209:
		return "upstream_rate_limited"
	case 40207:
		return "address_not_allowed"
	case 50401, 50402:
		return "timeout"
	default:
		if code >= 50000 {
			return "unavailable"
		}
		return "unexpected_response"
	}
}
