package store

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Autumn-27/artex/seoos/internal/domain"
)

func integrationOrg(t *testing.T, repo *Store, ctx context.Context) (string, context.Context) {
	t.Helper()
	org := testOrganization(t, repo, ctx)
	return org, domain.WithPrincipal(ctx, domain.Principal{OrganizationID: org, Role: domain.RoleOwner, Kind: "bootstrap"})
}

func integrationSite(t *testing.T, repo *Store, ctx context.Context, org, host string) domain.Site {
	t.Helper()
	in := validSiteInput()
	in.BaseURL = "https://" + host
	in.Name = host
	s, err := repo.CreateSite(ctx, org, in)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestPostgresIntegrationLifecycle(t *testing.T) {
	s, ctx, _ := openTestStore(t, true)
	if err := s.SetProviderKey(bytes.Repeat([]byte("x"), 32)); err != nil {
		t.Fatal(err)
	}
	org, owner := integrationOrg(t, s, ctx)
	otherOrg, otherOwner := integrationOrg(t, s, ctx)
	a := integrationSite(t, s, owner, org, "first.example.test")
	b := integrationSite(t, s, owner, org, "second.example.test")
	app, err := s.SaveIntegrationApplication(owner, org, "", domain.IntegrationApplicationInput{
		Provider: domain.IntegrationGoogle, Label: "Shared Google application", ClientID: "fixture-application",
		ClientSecret: "fixture-secret", Enabled: true, RedirectURI: "https://console.example.test" + domain.GoogleOAuthCallbackPath,
		AllowedScopes: []string{domain.GSCReadScope, domain.GA4ReadScope},
	})
	if err != nil {
		t.Fatal(err)
	}
	account, err := s.CreateIntegrationAccount(owner, org, domain.IntegrationAccountInput{
		Label: "Shared search account", Mode: domain.IntegrationOAuth, ApplicationID: app.ID, Provider: domain.IntegrationGoogle,
	})
	if err != nil {
		t.Fatal(err)
	}
	pending, state, err := s.StartIntegrationOAuth(owner, org, account.ID, app.AllowedScopes)
	if err != nil || len(state) != 43 || len(pending.Verifier) != 43 {
		t.Fatalf("OAuth setup failed: %v", err)
	}
	if _, err := s.ConsumeIntegrationOAuth(otherOwner, otherOrg, app.Provider, state, app.RedirectURI); !errors.Is(err, domain.ErrExecutionAuthority) {
		t.Fatalf("cross-organization state exchange: %v", err)
	}
	if _, err := s.ConsumeIntegrationOAuth(owner, org, app.Provider, state, "https://elsewhere.example.test"+domain.GoogleOAuthCallbackPath); !errors.Is(err, domain.ErrExecutionAuthority) {
		t.Fatalf("state exchanged on different callback: %v", err)
	}
	consumed, err := s.ConsumeIntegrationOAuth(owner, org, app.Provider, state, app.RedirectURI)
	if err != nil || consumed.Verifier != pending.Verifier {
		t.Fatalf("consume correct state: %v", err)
	}
	if _, err = s.ConsumeIntegrationOAuth(owner, org, app.Provider, state, app.RedirectURI); !errors.Is(err, domain.ErrExecutionAuthority) {
		t.Fatalf("OAuth state reused: %v", err)
	}
	expired := time.Now().Add(-time.Minute)
	account, err = s.SaveIntegrationGrant(owner, org, consumed.Access, domain.ExecutionCredential{
		RefreshToken: "fixture-refresh", AccessToken: "fixture-expired", ExpiresAt: &expired,
	}, consumed.Scopes)
	if err != nil {
		t.Fatal(err)
	}
	var refreshes atomic.Int32
	refresh := func(ctx context.Context, access domain.IntegrationAccess) (domain.ExecutionCredential, []string, error) {
		refreshes.Add(1)
		// The real account ledger must work while a refresh owns the account row.
		call, err := s.ReserveIntegrationRequest(ctx, org, account.ID, account.Version, "oauth_token", false)
		if err != nil {
			return domain.ExecutionCredential{}, nil, err
		}
		if err := s.SettleIntegrationRequest(ctx, org, account.ID, call, domain.ProviderReceipt{Operation: "oauth_token", HTTPStatus: 200, Category: "success"}); err != nil {
			return domain.ExecutionCredential{}, nil, err
		}
		end := time.Now().Add(time.Hour)
		secret := access.Credential
		secret.AccessToken, secret.ExpiresAt = "fixture-fresh", &end
		return secret, access.Account.Scopes, nil
	}
	results := make(chan error, 5)
	var done sync.WaitGroup
	for range 5 {
		done.Go(func() {
			_, err := s.RefreshIntegration(owner, org, account.ID, account.Version, false, refresh)
			results <- err
		})
	}
	done.Wait()
	close(results)
	for result := range results {
		if result != nil {
			t.Fatal(result)
		}
	}
	if refreshes.Load() != 1 {
		t.Fatalf("concurrent refreshes = %d", refreshes.Load())
	}
	bind := func(site domain.Site) string {
		id := "sc-domain:" + strings.TrimPrefix(site.BaseURL, "https://")
		in := domain.IntegrationBindingInput{SiteID: site.ID, AccountID: account.ID, Kind: domain.ExecutionGSC, ResourceID: id, Enabled: true}
		resource := domain.IntegrationResource{ID: id, Kind: domain.ExecutionGSC, Name: id, Permission: "siteOwner"}
		binding, err := s.SaveIntegrationBinding(owner, org, "", in, site, resource, account.Version)
		if err != nil {
			t.Fatal(err)
		}
		return binding
	}
	aBinding, bBinding := bind(a), bind(b)
	if data, err := s.IntegrationSummary(owner, org); err != nil || len(data) == 0 {
		t.Fatalf("summary: %v", err)
	}
	for _, section := range []string{"applications", "accounts", "bindings", "sites", "health", "history", "usage"} {
		rows, err := s.IntegrationRows(owner, org, section, domain.IntegrationFilter{Limit: 20})
		if err != nil || len(rows) == 0 {
			t.Fatalf("%s list: %v (%d rows)", section, err, len(rows))
		}
		for _, row := range rows {
			if bytes.Contains(row, []byte("fixture-secret")) || bytes.Contains(row, []byte("fixture-refresh")) {
				t.Fatalf("credential was returned by %s", section)
			}
		}
	}
	foreign, err := s.IntegrationRows(otherOwner, otherOrg, "bindings", domain.IntegrationFilter{Limit: 5})
	if err != nil || len(foreign) != 0 {
		t.Fatalf("organization list leaked bindings: %v, %d", err, len(foreign))
	}
	if err := s.RemoveIntegrationBinding(owner, org, aBinding); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.ExecutionConnectionAccess(owner, org, b.ID, bBinding); err != nil {
		t.Fatalf("detaching one site changed the other: %v", err)
	}
	job, _, err := s.EnqueueExecution(owner, org, domain.ExecutionJobInput{
		SiteID: b.ID, Kind: domain.ExecGSCSync, IdempotencyKey: "integration-default",
	})
	if err != nil || job.ID == "" {
		t.Fatalf("default binding job: %v", err)
	}
	if _, err := s.RevokeIntegrationAccount(owner, org, account.ID, true); !errors.Is(err, domain.ErrIntegrationDependency) {
		t.Fatalf("removed account still bound to a site: %v", err)
	}
	if _, err := s.RevokeIntegrationAccount(owner, org, account.ID, false); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.ExecutionConnectionAccess(owner, org, b.ID, bBinding); !errors.Is(err, domain.ErrIntegrationRevoked) {
		t.Fatalf("revoked account still resolved: %v", err)
	}
}

func TestPostgresLegacyMigrationAndIndexNowRotation(t *testing.T) {
	s, ctx, _ := openTestStore(t, true)
	if err := s.SetProviderKey(bytes.Repeat([]byte("m"), 32)); err != nil {
		t.Fatal(err)
	}
	org, owner := integrationOrg(t, s, ctx)
	a := integrationSite(t, s, owner, org, "legacy.example.test")
	const previousKey = "LegacyKey64"
	legacy, err := s.SaveExecutionConnection(owner, org, "", domain.ExecutionConnectionSave{
		ExecutionConnectionInput: domain.ExecutionConnectionInput{SiteID: a.ID, Name: "Legacy IndexNow", Kind: domain.ExecutionIndexNow,
			KeyLocation: a.BaseURL + "/" + previousKey + ".txt", Enabled: true, AllowWrites: true},
		Credential: &domain.ExecutionCredential{Secret: previousKey},
	})
	if err != nil {
		t.Fatal(err)
	}
	run := func(dry bool, want string) {
		result, err := s.MigrateLegacyIntegrations(owner, org, dry)
		if err != nil || len(result) != 1 || result[0].Status != want {
			t.Fatalf("migration %s: %+v, %v", want, result, err)
		}
	}
	run(true, "READY")
	run(false, "MIGRATED")
	run(false, "ALREADY_MIGRATED")
	resolved, secret, err := s.ExecutionConnectionAccess(owner, org, a.ID, legacy.ID)
	if err != nil || !resolved.Managed || !resolved.LegacyKey || resolved.KeyLocation != a.BaseURL+"/"+previousKey+".txt" || secret.Secret != previousKey {
		t.Fatalf("legacy key root migration: %v", err)
	}
	id, err := s.SaveIndexNowKey(owner, org, legacy.ID, domain.IndexNowKeyInput{
		SiteID: a.ID, Version: legacy.Version, Directory: "/news/", Key: "abcdef012345", AllowWrites: true,
	})
	if err != nil || id != legacy.ID {
		t.Fatalf("rotation: %v", err)
	}
	updated, secret, err := s.ExecutionConnectionAccess(owner, org, a.ID, id)
	if err != nil || updated.LegacyKey || updated.KeyLocation != a.BaseURL+"/news/abcdef012345.txt" || secret.Secret != "abcdef012345" {
		t.Fatalf("rotated key not active: %v", err)
	}
	items, err := s.IntegrationRows(owner, org, "keys", domain.IntegrationFilter{Limit: 10})
	if err != nil || len(items) != 1 || bytes.Contains(items[0], []byte(secret.Secret)) {
		t.Fatalf("key metadata exposure: %v", err)
	}
	plain, location, err := s.RevealIndexNowKey(owner, org, id)
	if err != nil || plain != secret.Secret || location != updated.KeyLocation {
		t.Fatalf("explicit key reveal: %v", err)
	}
}
