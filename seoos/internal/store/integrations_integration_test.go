package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Autumn-27/artex/seoos/internal/domain"
	"github.com/google/uuid"
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
		RefreshToken: "fixture-refresh", AccessToken: "fixture-expired", ExpiresAt: &expired, ExternalSubject: "fixture-subject",
	}, consumed.Scopes)
	if err != nil || account.IdentityStatus != domain.IntegrationIdentityVerified || account.VerifiedSubject != "fixture-subject" {
		t.Fatalf("save verified identity: %v", err)
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
		const id = "sc-domain:example.test"
		in := domain.IntegrationBindingInput{SiteID: site.ID, AccountID: account.ID, Kind: domain.ExecutionGSC, ResourceID: id, Enabled: true}
		resource := domain.IntegrationResource{ID: id, Kind: domain.ExecutionGSC, Name: id, Permission: "siteOwner"}
		binding, err := s.SaveIntegrationBinding(owner, org, "", in, site, resource, account.Version)
		if err != nil {
			t.Fatal(err)
		}
		return binding
	}
	aBinding, bBinding := bind(a), bind(b)
	gsc := domain.IntegrationResource{Kind: domain.ExecutionGSC, ID: "sc-domain:example.test", Permission: "siteOwner"}
	duplicate := domain.IntegrationBindingInput{SiteID: a.ID, AccountID: account.ID, Kind: gsc.Kind, ResourceID: gsc.ID, Enabled: true}
	if _, err := s.SaveIntegrationBinding(owner, org, "", duplicate, a, gsc, account.Version); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("a site can have multiple bindings to the same service: %v", err)
	}
	foreignSite := integrationSite(t, s, otherOwner, otherOrg, "foreign.example.test")
	duplicate.SiteID = foreignSite.ID
	if _, err := s.SaveIntegrationBinding(otherOwner, otherOrg, "", duplicate, foreignSite, gsc, account.Version); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("a foreign organization could bind through this account: %v", err)
	}
	for i, site := range []domain.Site{a, b} {
		resource := domain.IntegrationResource{Kind: domain.ExecutionGA4, ID: "properties/54321", TimeZone: "America/New_York",
			WebStreamIDs: []string{[]string{"101", "202"}[i]}}
		input := domain.IntegrationBindingInput{SiteID: site.ID, AccountID: account.ID, Kind: resource.Kind, ResourceID: resource.ID, Enabled: true}
		id, err := s.SaveIntegrationBinding(owner, org, "", input, site, resource, account.Version)
		if err != nil {
			t.Fatal(err)
		}
		config, _, err := s.ExecutionConnectionAccess(owner, org, site.ID, id)
		if err != nil || len(config.PropertyWebStreamIDs) != 1 || config.PropertyWebStreamIDs[0] != resource.WebStreamIDs[0] {
			t.Fatalf("shared property did not retain separate verified metadata: %v", err)
		}
	}
	if data, err := s.IntegrationSummary(owner, org); err != nil || len(data) == 0 {
		t.Fatalf("summary: %v", err)
	}
	for _, section := range []string{"applications", "accounts", "bindings", "sites", "health", "history", "usage"} {
		rows, err := s.IntegrationRows(owner, org, section, domain.IntegrationFilter{Limit: 20})
		if err != nil || len(rows) == 0 {
			t.Fatalf("%s list: %v (%d rows)", section, err, len(rows))
		}
		for _, row := range rows {
			if bytes.Contains(row, []byte("fixture-secret")) || bytes.Contains(row, []byte("fixture-refresh")) ||
				bytes.Contains(row, []byte("fixture-subject")) {
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
	if rebound := bind(a); rebound == aBinding {
		t.Fatal("rebinding reused a removed projection")
	} else if err := s.RemoveIntegrationBinding(owner, org, rebound); err != nil {
		t.Fatal(err)
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
	reconnect, err := s.integrationAccess(owner, s.db, org, account.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	reauthorization := domain.ExecutionCredential{RefreshToken: "fixture-new-refresh", ExternalSubject: "other-fixture-subject"}
	var problem *domain.DataError
	if _, err := s.SaveIntegrationGrant(owner, org, reconnect, reauthorization, app.AllowedScopes); !errors.As(err, &problem) || problem.Code != "integration_oauth_identity_mismatch" {
		t.Fatalf("a different identity reauthorized the revoked account: %v", err)
	}
	reauthorization.ExternalSubject = account.VerifiedSubject
	reconnected, err := s.SaveIntegrationGrant(owner, org, reconnect, reauthorization, app.AllowedScopes)
	if err != nil {
		t.Fatal(err)
	}
	projection, _, err := s.ExecutionConnectionAccess(owner, org, b.ID, bBinding)
	if !errors.Is(err, domain.ErrIntegrationRevoked) {
		t.Fatalf("reauthorizing automatically enabled a site: %v", err)
	}
	resource := domain.IntegrationResource{Kind: domain.ExecutionGSC, ID: "sc-domain:example.test", Permission: "siteOwner"}
	input := domain.IntegrationBindingInput{SiteID: b.ID, Kind: resource.Kind, AccountID: account.ID, ResourceID: resource.ID,
		Enabled: true, Version: projection.Version}
	if _, err := s.SaveIntegrationBinding(owner, org, bBinding, input, b, resource, reconnected.Version); err != nil {
		t.Fatal(err)
	}
	if projection, _, err = s.ExecutionConnectionAccess(owner, org, b.ID, bBinding); err != nil || !projection.Enabled {
		t.Fatalf("explicit site verification did not restore binding: %v", err)
	}
}

func TestPostgresLegacyAccountMigrationMatching(t *testing.T) {
	s, ctx, _ := openTestStore(t, true)
	if err := s.SetProviderKey(bytes.Repeat([]byte("m"), 32)); err != nil {
		t.Fatal(err)
	}
	org, owner := integrationOrg(t, s, ctx)
	otherOrg, otherOwner := integrationOrg(t, s, ctx)
	const bingKey = "fixture-bing-key"
	shared := domain.ExecutionCredential{ClientID: "fixture-client", ClientSecret: "fixture-client-secret", RefreshToken: "fixture-shared-refresh"}
	otherGrant, otherApp := shared, shared
	otherGrant.RefreshToken = "fixture-distinct-refresh"
	otherApp.ClientSecret = "fixture-distinct-secret"
	firstGoogle := saveLegacySearch(t, s, owner, org, "one.example.test", domain.ExecutionGSC, "sc-domain:example.test", shared)
	secondGoogle := saveLegacySearch(t, s, owner, org, "two.example.test", domain.ExecutionGA4, "properties/54321", shared)
	thirdGoogle := saveLegacySearch(t, s, owner, org, "three.example.test", domain.ExecutionGSC, "sc-domain:example.test", otherGrant)
	fourthGoogle := saveLegacySearch(t, s, owner, org, "four.example.test", domain.ExecutionGSC, "sc-domain:example.test", otherApp)
	firstBing := saveLegacySearch(t, s, owner, org, "bing-one.example.test", domain.ExecutionBing, "https://bing-one.example.test/", domain.ExecutionCredential{Secret: bingKey})
	secondBing := saveLegacySearch(t, s, owner, org, "bing-two.example.test", domain.ExecutionBing, "https://bing-two.example.test/", domain.ExecutionCredential{Secret: bingKey})
	foreign := saveLegacySearch(t, s, otherOwner, otherOrg, "other.example.test", domain.ExecutionGSC, "sc-domain:example.test", shared)

	report := func(runCtx context.Context, runOrg string, options LegacyIntegrationOptions) map[string]LegacyIntegrationResult {
		t.Helper()
		rows, err := s.ReviewLegacyIntegrations(runCtx, runOrg, options)
		if err != nil {
			t.Fatal(err)
		}
		result := make(map[string]LegacyIntegrationResult, len(rows))
		for _, row := range rows {
			result[row.ConnectionID] = row
		}
		raw, err := json.Marshal(rows)
		if err != nil || strings.Contains(string(raw), "fixture-") {
			t.Fatal("migration report contained a credential or could not be encoded")
		}
		return result
	}
	assertCounts := func(accounts, bindings, receipts int) {
		t.Helper()
		for table, expected := range map[string]int{"seo_integration_accounts": accounts,
			"seo_site_integration_bindings": bindings, "seo_integration_legacy_migration_receipts": receipts} {
			var actual int
			if err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM public."+table+" WHERE organization_id=$1", org).Scan(&actual); err != nil || actual != expected {
				t.Fatalf("%s got %d, want %d: %v", table, actual, expected, err)
			}
		}
	}
	preview := report(owner, org, LegacyIntegrationOptions{DryRun: true})
	gTarget := preview[firstGoogle.ID].TargetAccountID
	bTarget := preview[firstBing.ID].TargetAccountID
	if len(preview) != 6 || gTarget == "" || bTarget == "" || preview[secondGoogle.ID].TargetAccountID != gTarget ||
		preview[secondGoogle.ID].MatchBasis != legacyGoogleMatch ||
		preview[secondGoogle.ID].ReauthorizationReason != "GOOGLE_IDENTITY_UNVERIFIED" ||
		preview[thirdGoogle.ID].MatchBasis != "GOOGLE_CLIENT_ID_WITH_DIFFERENT_GRANT" || preview[thirdGoogle.ID].TargetAccountID == gTarget ||
		preview[fourthGoogle.ID].MatchBasis != "GOOGLE_CLIENT_ID_WITH_DIFFERENT_GRANT" || preview[fourthGoogle.ID].TargetAccountID == gTarget ||
		preview[secondBing.ID].TargetAccountID != bTarget || preview[secondBing.ID].Status != "MERGE_REVIEW_REQUIRED" {
		t.Fatalf("unexpected migration matches: %+v", preview)
	}
	if listed := report(owner, org, LegacyIntegrationOptions{DuplicatesOnly: true}); len(listed) != 2 {
		t.Fatalf("duplicate report has %d rows", len(listed))
	}
	rows := report(otherOwner, otherOrg, LegacyIntegrationOptions{DryRun: true})
	if len(rows) != 1 || rows[foreign.ID].TargetAccountID == gTarget {
		t.Fatal("credential or target matched across organizations")
	}
	assertCounts(0, 0, 0)
	wrong := []LegacyIntegrationApproval{{ConnectionID: secondBing.ID, TargetAccountID: uuid.NewString()}}
	if _, err := s.ReviewLegacyIntegrations(owner, org, LegacyIntegrationOptions{Approvals: wrong}); err == nil {
		t.Fatal("stale approval was accepted")
	}
	assertCounts(0, 0, 0)
	firstRun := report(owner, org, LegacyIntegrationOptions{})
	if firstRun[secondGoogle.ID].ReceiptID == "" || firstRun[secondBing.ID].Status != "MERGE_REVIEW_REQUIRED" {
		t.Fatalf("unguarded Bing merge or missing Google receipt: %+v", firstRun)
	}
	assertCounts(4, 5, 5)
	approved := LegacyIntegrationOptions{Approvals: []LegacyIntegrationApproval{{ConnectionID: secondBing.ID, TargetAccountID: bTarget}}}
	applied := report(owner, org, approved)
	if got := applied[secondBing.ID]; got.BindingResult != "BOUND" || got.ReceiptID == "" {
		t.Fatalf("approved Bing merge: %+v", got)
	}
	assertCounts(4, 6, 6)
	if again := report(owner, org, approved); again[secondBing.ID].Status != "ALREADY_MIGRATED" {
		t.Fatalf("approved run is not idempotent: %+v", again[secondBing.ID])
	}
	assertCounts(4, 6, 6)
	access, err := s.integrationAccess(owner, s.db, org, gTarget, "")
	if err != nil || !access.Account.Allows(domain.ExecutionGSC, false) || !access.Account.Allows(domain.ExecutionGA4, false) {
		t.Fatalf("matching Google grant did not retain both legacy scopes: %v", err)
	}
}

func TestPostgresLegacyMigrationReassignsExistingAliases(t *testing.T) {
	s, ctx, _ := openTestStore(t, true)
	if err := s.SetProviderKey(bytes.Repeat([]byte("m"), 32)); err != nil {
		t.Fatal(err)
	}
	org, owner := integrationOrg(t, s, ctx)
	shared := domain.ExecutionCredential{ClientID: "fixture-client", ClientSecret: "fixture-secret", RefreshToken: "fixture-refresh"}
	gsc := saveLegacySearch(t, s, owner, org, "one.example.test", domain.ExecutionGSC, "sc-domain:example.test", shared)
	ga4 := saveLegacySearch(t, s, owner, org, "two.example.test", domain.ExecutionGA4, "properties/54321", shared)
	first, second := uuid.NewString(), uuid.NewString()
	tx, err := s.db.BeginTx(owner, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for _, legacy := range []struct {
		selection domain.ExecutionConnection
		id        string
	}{{gsc, first}, {ga4, second}} {
		if err := s.migrateLegacyAccount(owner, tx, legacy.selection, shared, legacy.id); err != nil {
			t.Fatal(err)
		}
		run := legacyMigration{repo: s, tx: tx, org: org}
		if err := run.insertBinding(owner, legacy.selection, "", "", LegacyIntegrationResult{
			TargetProperty: legacy.selection.Property, TargetAccountID: legacy.id,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	report, err := s.MigrateLegacyIntegrations(owner, org, false)
	if err != nil || len(report) != 2 || report[1].Status != "REASSIGNED" || report[1].PreviousAccountID != second || report[1].TargetAccountID != first {
		t.Fatalf("existing alias migration: %+v, %v", report, err)
	}
	var before int64
	var oldAccount string
	err = s.db.QueryRowContext(ctx, `SELECT previous_account_id::text,previous_connection_version
		FROM public.seo_integration_legacy_migration_receipts WHERE organization_id=$1 AND id=$2`, org, report[1].ReceiptID).
		Scan(&oldAccount, &before)
	if err != nil || before != ga4.Version || oldAccount != second {
		t.Fatalf("missing rollback inputs: %v", err)
	}
	projection, secret, err := s.ExecutionConnectionAccess(owner, org, ga4.SiteID, ga4.ID)
	if err != nil || projection.Version != ga4.Version+1 || projection.IntegrationAccountID != first || secret.RefreshToken != shared.RefreshToken {
		t.Fatalf("projection was not safely redirected: %v", err)
	}
	if _, err := s.integrationAccess(owner, s.db, org, second, ""); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("the duplicate account remained selectable: %v", err)
	}
	var sealed []byte
	err = s.db.QueryRowContext(ctx, `SELECT encrypted_credentials FROM public.seo_integration_accounts
		WHERE organization_id=$1 AND id=$2 AND deleted_at IS NOT NULL`, org, second).Scan(&sealed)
	var old domain.ExecutionCredential
	if err != nil || s.openIntegration("account", org, second, sealed, &old) != nil || old.RefreshToken != shared.RefreshToken {
		t.Fatalf("previous credential not retained for rollback: %v", err)
	}
	if err := s.RemoveIntegrationBinding(owner, org, ga4.ID); err != nil {
		t.Fatal(err)
	}
	report, err = s.MigrateLegacyIntegrations(owner, org, false)
	if err != nil || len(report) != 2 || report[1].Status != "BINDING_REMOVED" {
		t.Fatalf("migration recreated a removed binding: %+v, %v", report, err)
	}
}

func saveLegacySearch(t *testing.T, s *Store, owner context.Context, org, host, kind, property string,
	secret domain.ExecutionCredential) domain.ExecutionConnection {
	t.Helper()
	site := integrationSite(t, s, owner, org, host)
	in := domain.ExecutionConnectionInput{SiteID: site.ID, Kind: kind, Property: property, Name: "Legacy " + kind, Enabled: true}
	if kind == domain.ExecutionGSC || kind == domain.ExecutionGA4 {
		in.RedirectURI = "https://console.example.test" + domain.GoogleOAuthCallbackPath
	}
	connection, err := s.SaveExecutionConnection(owner, org, "", domain.ExecutionConnectionSave{
		ExecutionConnectionInput: in, Credential: &secret,
	})
	if err != nil {
		t.Fatal(err)
	}
	return connection
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
