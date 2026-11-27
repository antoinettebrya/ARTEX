package executionsearch

import (
	"context"
	"net/http"
	"time"

	"github.com/Autumn-27/artex/seoos/internal/domain"
	"github.com/Autumn-27/artex/seoos/internal/executionhttp"
)

type AccountLedger interface {
	ReserveIntegrationRequest(context.Context, string, string, int64, string, bool) (int64, error)
	SettleIntegrationRequest(context.Context, string, string, int64, domain.ProviderReceipt) error
}

func AccountClient(fixture *http.Client, repo AccountLedger, org string, access domain.IntegrationAccess, allowInactive bool) *Client {
	account, secret := access.Account, access.Credential
	return New(executionhttp.Client(fixture, 9*time.Second, executionhttp.Ledger{
		Provider: account.Provider,
		Secrets:  []string{secret.ClientSecret, secret.RefreshToken, secret.AccessToken, secret.Secret},
		ReserveOperation: func(ctx context.Context, operation string) (int64, error) {
			return repo.ReserveIntegrationRequest(ctx, org, account.ID, account.Version, operation, allowInactive)
		},
		Settle: func(ctx context.Context, id int64, receipt domain.ProviderReceipt) error {
			return repo.SettleIntegrationRequest(ctx, org, account.ID, id, receipt)
		},
	}))
}
