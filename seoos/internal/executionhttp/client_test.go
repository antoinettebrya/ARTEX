package executionhttp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Autumn-27/artex/seoos/internal/domain"
)

type testTransport func(*http.Request) (*http.Response, error)

func (send testTransport) RoundTrip(r *http.Request) (*http.Response, error) { return send(r) }

func TestBingDiagnosticsDropEchoedCredentials(t *testing.T) {
	const key, bearer = "fixture-bing-api-key", "fixture-bearer-secret"
	for _, mode := range []string{"api_key", "oauth"} {
		t.Run(mode, func(t *testing.T) {
			var got domain.ProviderReceipt
			var sensitive string
			transport := countedTransport{ledger: Ledger{Provider: domain.ExecutionBing,
				ReserveOperation: func(_ context.Context, op string) (int64, error) {
					if op != "GetUserSites" {
						t.Errorf("unexpected operation %s", op)
					}
					return 1, nil
				},
				Settle: func(_ context.Context, _ int64, result domain.ProviderReceipt) error {
					got = result
					return nil
				}}, transport: testTransport(func(r *http.Request) (*http.Response, error) {
				headers := make(http.Header)
				headers.Set("X-Request-ID", "echoed-"+sensitive)
				headers.Set("X-MS-Request-ID", "safe-request-id")
				return &http.Response{
					StatusCode: http.StatusTooManyRequests, Header: headers,
					Body: io.NopCloser(strings.NewReader(`{"error":{"code":"` + sensitive +
						`","type":"quota","param":"` + sensitive + `","message":"private ` + sensitive + `"}}`)),
				}, nil
			})}
			endpoint := "https://ssl.bing.com/webmaster/api.svc/json/GetUserSites"
			if mode == "api_key" {
				sensitive = key
				endpoint += "?apikey=" + key
			} else {
				sensitive = bearer
			}
			req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, endpoint, nil)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "oauth" {
				req.Header.Set("Authorization", "Bearer "+bearer)
			}
			r, err := transport.RoundTrip(req)
			if err != nil {
				t.Fatal(err)
			}
			if err := r.Body.Close(); err != nil {
				t.Fatal(err)
			}
			wire, err := json.Marshal(got)
			if err != nil || strings.Contains(string(wire), sensitive) || strings.Contains(string(wire), "private") ||
				got.RequestID != "safe-request-id" || got.UpstreamCode != "quota" || got.Parameter != "" ||
				got.HTTPStatus != http.StatusTooManyRequests || !got.Retryable {
				t.Fatal("Bing request receipt was not sanitized or classified")
			}
		})
	}
}
