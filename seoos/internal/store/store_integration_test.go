package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Autumn-27/artex/seoos/internal/domain"
	"github.com/google/uuid"
)

// These tests require an explicitly selected disposable PostgreSQL database.
// They migrate the seo_* tables and clean up only their own organization rows;
// they never truncate a table or drop a schema/database.
func openTestStore(t *testing.T, migrate bool) (*Store, context.Context, string) {
	t.Helper()
	dsn := os.Getenv("SEOOS_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set SEOOS_TEST_DATABASE_URL to run PostgreSQL integration tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	t.Cleanup(cancel)
	s, err := Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if migrate {
		if err := s.Migrate(ctx); err != nil {
			t.Fatalf("migrate test database: %v", err)
		}
	}
	return s, ctx, dsn
}

func testOrganization(t *testing.T, s *Store, ctx context.Context) string {
	t.Helper()
	id := uuid.NewString()
	if err := s.EnsureOrganization(ctx, id, "SEO storage integration test"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := s.db.ExecContext(cleanupCtx, `DELETE FROM public.seo_sites WHERE organization_id = $1`, id); err != nil {
			t.Errorf("clean up own test sites: %v", err)
			return
		}
		if _, err := s.db.ExecContext(cleanupCtx, `DELETE FROM public.seo_organizations WHERE id = $1`, id); err != nil {
			t.Errorf("clean up own test organization: %v", err)
		}
	})
	return id
}

func validSiteInput() domain.SiteInput {
	return domain.SiteInput{
		Name: "Example editorial site", BaseURL: "https://example.com",
		CMS: "wordpress", CountryCode: "US", LanguageTag: "en-US", Status: "active",
	}
}

func TestPostgresMigrationsSerializeAndRepeat(t *testing.T) {
	s, ctx, _ := openTestStore(t, false)
	const clients = 4
	results := make(chan error, clients)
	for range clients {
		go func() { results <- s.Migrate(ctx) }()
	}
	for range clients {
		if err := <-results; err != nil {
			t.Errorf("concurrent migration failed: %v", err)
		}
	}
	if t.Failed() {
		return
	}
	if err := s.Migrate(ctx); err != nil {
		t.Fatalf("repeat migration failed: %v", err)
	}
	all, err := migrations()
	if err != nil {
		t.Fatal(err)
	}
	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM public.seo_schema_migrations`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != len(all) {
		t.Errorf("migration count = %d, want %d", count, len(all))
	}
}

func TestPostgresTenantIsolationAndSiteLifecycle(t *testing.T) {
	s, ctx, dsn := openTestStore(t, true)
	orgA := testOrganization(t, s, ctx)
	orgB := testOrganization(t, s, ctx)
	input := validSiteInput()
	siteA, err := s.CreateSite(ctx, orgA, input)
	if err != nil {
		t.Fatal(err)
	}
	if siteA.OrganizationID != orgA || siteA.Version != 1 || siteA.CreatedAt.IsZero() || siteA.UpdatedAt.IsZero() {
		t.Fatalf("unexpected newly persisted site: %#v", siteA)
	}
	if _, err := s.CreateSite(ctx, orgA, input); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("same-organization duplicate = %v, want ErrConflict", err)
	}
	siteB, err := s.CreateSite(ctx, orgB, input)
	if err != nil {
		t.Fatalf("same URL in another organization should be allowed: %v", err)
	}
	if siteA.ID == siteB.ID {
		t.Fatal("different organizations unexpectedly received the same site ID")
	}
	if _, err := s.GetSite(ctx, orgB, siteA.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("cross-organization get = %v, want ErrNotFound", err)
	}
	if _, err := s.UpdateSite(ctx, orgB, siteA.ID, 1, input); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("cross-organization update = %v, want ErrNotFound", err)
	}
	for _, org := range []string{orgA, orgB} {
		sites, err := s.ListSites(ctx, org, 50, 0)
		if err != nil || len(sites) != 1 || sites[0].OrganizationID != org {
			t.Fatalf("organization-scoped list = %#v, %v", sites, err)
		}
	}
	if _, err := s.GetSite(ctx, orgA, "invalid-id"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("malformed resource identifier = %v, want ErrNotFound", err)
	}

	input.Name = "Updated site"
	input.Status = "paused"
	updated, err := s.UpdateSite(ctx, orgA, siteA.ID, 1, input)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Version != 2 || updated.Name != input.Name || updated.Status != "paused" || updated.CreatedAt != siteA.CreatedAt {
		t.Fatalf("unexpected updated site: %#v", updated)
	}
	if updated.UpdatedAt.Before(siteA.UpdatedAt) {
		t.Fatal("update timestamp moved backwards")
	}
	stale := input
	stale.Name = "This stale write must not persist"
	if _, err := s.UpdateSite(ctx, orgA, siteA.ID, 1, stale); !errors.Is(err, domain.ErrVersionConflict) {
		t.Fatalf("stale update = %v, want ErrVersionConflict", err)
	}
	input.Status = "archived"
	archived, err := s.UpdateSite(ctx, orgA, siteA.ID, updated.Version, input)
	if err != nil || archived.Status != "archived" || archived.Version != 3 {
		t.Fatalf("archive = %#v, %v", archived, err)
	}

	// A separate connection pool must observe the durable state, rather than an
	// in-process cache. Archiving retains the site and its version history marker.
	reopened, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	persisted, err := reopened.GetSite(ctx, orgA, siteA.ID)
	if err != nil || persisted.Name != "Updated site" || persisted.Status != "archived" || persisted.Version != 3 {
		t.Fatalf("reopened database = %#v, %v", persisted, err)
	}
	unchanged, err := reopened.GetSite(ctx, orgB, siteB.ID)
	if err != nil || unchanged.Version != 1 || unchanged.Status != "active" {
		t.Fatalf("other organization was changed: %#v, %v", unchanged, err)
	}
}

func TestPostgresConcurrentUpdatesConsumeOneVersion(t *testing.T) {
	s, ctx, _ := openTestStore(t, true)
	org := testOrganization(t, s, ctx)
	site, err := s.CreateSite(ctx, org, validSiteInput())
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	for i := range 2 {
		go func() {
			input := validSiteInput()
			input.Name = fmt.Sprintf("Concurrent update %d", i)
			<-start
			_, err := s.UpdateSite(ctx, org, site.ID, site.Version, input)
			results <- err
		}()
	}
	close(start)
	writes, conflicts := 0, 0
	for range 2 {
		switch err := <-results; {
		case err == nil:
			writes++
		case errors.Is(err, domain.ErrVersionConflict):
			conflicts++
		default:
			t.Fatalf("unexpected concurrent update error: %v", err)
		}
	}
	if writes != 1 || conflicts != 1 {
		t.Fatalf("writes=%d conflicts=%d, want one of each", writes, conflicts)
	}
	persisted, err := s.GetSite(ctx, org, site.ID)
	if err != nil || persisted.Version != 2 {
		t.Fatalf("final concurrent version = %#v, %v", persisted, err)
	}
}

func TestPostgresConflictingUpdateDoesNotChangeSite(t *testing.T) {
	s, ctx, _ := openTestStore(t, true)
	org := testOrganization(t, s, ctx)
	first, err := s.CreateSite(ctx, org, validSiteInput())
	if err != nil {
		t.Fatal(err)
	}
	other := validSiteInput()
	other.BaseURL = "https://other.example.com"
	second, err := s.CreateSite(ctx, org, other)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateSite(ctx, org, second.ID, second.Version, validSiteInput()); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("duplicate URL update = %v, want ErrConflict", err)
	}
	persisted, err := s.GetSite(ctx, org, second.ID)
	if err != nil || persisted.Version != second.Version || persisted.BaseURL != second.BaseURL {
		t.Fatalf("conflicting update changed data: %#v, %v", persisted, err)
	}
	pageOne, err := s.ListSites(ctx, org, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	pageTwo, err := s.ListSites(ctx, org, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(pageOne) != 1 || len(pageTwo) != 1 || pageOne[0].ID == pageTwo[0].ID {
		t.Fatalf("unexpected pagination: %#v / %#v", pageOne, pageTwo)
	}
	if pageOne[0].ID != second.ID || pageTwo[0].ID != first.ID {
		t.Fatal("pagination is not in newest-first order")
	}
	invalid := validSiteInput()
	invalid.Status = "unknown"
	if _, err := s.CreateSite(ctx, org, invalid); err == nil {
		t.Fatal("store accepted an invalid site")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	other.BaseURL = "https://cancelled.example.com"
	if _, err := s.CreateSite(cancelled, org, other); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled write = %v, want context.Canceled", err)
	}
	sites, err := s.ListSites(ctx, org, 50, 0)
	if err != nil || len(sites) != 2 {
		t.Fatalf("failed writes unexpectedly persisted: %#v, %v", sites, err)
	}
}

func TestPostgresOrganizationBootstrapDoesNotOverwrite(t *testing.T) {
	s, ctx, _ := openTestStore(t, true)
	org := testOrganization(t, s, ctx)
	if err := s.EnsureOrganization(ctx, org, "Unexpected replacement name"); err != nil {
		t.Fatal(err)
	}
	var name string
	if err := s.db.QueryRowContext(ctx, `SELECT name FROM public.seo_organizations WHERE id = $1`, org).Scan(&name); err != nil {
		t.Fatal(err)
	}
	if name != "SEO storage integration test" {
		t.Fatalf("bootstrap overwrote name: %q", name)
	}
}

func TestOpenDoesNotEchoInvalidDSNCredentials(t *testing.T) {
	const secret = "never-log-this-test-password"
	_, err := Open(context.Background(), "postgres://user:"+secret+"@%zz/database")
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Fatal("invalid connection configuration must fail without echoing credentials")
	}
}
