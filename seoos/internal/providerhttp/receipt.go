// Package providerhttp extracts bounded, secret-free diagnostics from HTTP calls.
package providerhttp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Autumn-27/artex/seoos/internal/domain"
)

func Response(receipt *domain.ProviderReceipt, response *http.Response, secrets ...string) {
	receipt.HTTPStatus = response.StatusCode
	for _, name := range []string{"X-Request-Id", "Request-Id", "X-Goog-Request-Id", "X-Ms-Request-Id", "X-Correlation-Id", "Trace-Id"} {
		if value := Identifier(response.Header.Get(name), secrets...); value != "" {
			receipt.RequestID = value
			return
		}
	}
}

func Identifier(value string, secrets ...string) string {
	if value == "" || len(value) > 160 {
		return ""
	}
	for _, secret := range secrets {
		if secret != "" && strings.Contains(value, secret) {
			return ""
		}
	}
	for _, c := range value {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' ||
			strings.ContainsRune("._:-[]", c)) {
			return ""
		}
	}
	return value
}

func ErrorDetails(receipt *domain.ProviderReceipt, body []byte, secrets ...string) {
	var envelope struct {
		Error     json.RawMessage `json:"error"`
		ErrorCode json.RawMessage `json:"ErrorCode"`
	}
	if len(body) > 64<<10 || json.Unmarshal(body, &envelope) != nil {
		return
	}
	var details struct {
		Code   json.RawMessage `json:"code"`
		Type   string          `json:"type"`
		Status string          `json:"status"`
		Param  string          `json:"param"`
	}
	_ = json.Unmarshal(envelope.Error, &details)
	scalar := func(raw json.RawMessage) string {
		var code string
		if json.Unmarshal(raw, &code) != nil {
			if _, err := strconv.ParseInt(string(raw), 10, 64); err == nil {
				return string(raw)
			}
		}
		return code
	}
	for _, candidate := range []string{scalar(details.Code), details.Type, details.Status, scalar(envelope.Error), scalar(envelope.ErrorCode)} {
		if v := Identifier(candidate, secrets...); v != "" {
			receipt.UpstreamCode = v
			break
		}
	}
	receipt.Parameter = Identifier(details.Param, secrets...)
}

type outcomeKey struct{}

// Outcome allows a client to mark a recognized upstream failure before it
// closes the response, without copying the response body into the ledger.
type Outcome struct {
	mu      sync.Mutex
	problem *domain.DataError
}

func Observe(ctx context.Context) (context.Context, *Outcome) {
	outcome := &Outcome{}
	return context.WithValue(ctx, outcomeKey{}, outcome), outcome
}

func (o *Outcome) Set(err error) {
	var problem *domain.DataError
	if !errors.As(err, &problem) {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	safe := *problem
	o.problem = &safe
}

func OperationOutcome(ctx context.Context) *domain.DataError {
	outcome, _ := ctx.Value(outcomeKey{}).(*Outcome)
	if outcome == nil {
		return nil
	}
	outcome.mu.Lock()
	defer outcome.mu.Unlock()
	return outcome.problem
}

func HTTPCategory(status int) string {
	switch status {
	case http.StatusUnauthorized:
		return "authentication"
	case http.StatusForbidden:
		return "permission"
	case http.StatusPaymentRequired:
		return "billing"
	case http.StatusTooManyRequests:
		return "rate_limit"
	case http.StatusRequestTimeout, http.StatusGatewayTimeout:
		return "timeout"
	case http.StatusNotFound:
		return "not_found"
	}
	switch {
	case status >= 200 && status < 300:
		return "success"
	case status >= 300 && status < 400:
		return "redirect"
	case status >= 400 && status < 500:
		return "request"
	case status >= 500:
		return "upstream"
	default:
		return "transport"
	}
}

func IsTransient(status int) bool {
	return status == http.StatusRequestTimeout || status == http.StatusTooManyRequests || status >= 500
}

func RetryAfter(raw string, limit time.Duration) time.Duration {
	if seconds, err := strconv.ParseInt(raw, 10, 64); err == nil && seconds >= 0 {
		return time.Duration(min(seconds, int64(limit/time.Second))) * time.Second
	}
	if at, err := http.ParseTime(raw); err == nil {
		return min(max(time.Until(at), 0), limit)
	}
	return 0
}
