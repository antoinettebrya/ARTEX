package domain

import (
	"testing"

	"github.com/google/uuid"
)

func TestRoleAndEmailValidation(t *testing.T) {
	for _, test := range []struct {
		role         Role
		valid, write bool
	}{{RoleOwner, true, true}, {RoleEditor, true, true}, {RoleViewer, true, false}, {"admin", false, false}} {
		if test.role.Valid() != test.valid || test.role.CanWrite() != test.write {
			t.Fatalf("wrong permissions for role %q", test.role)
		}
	}
	if email, err := NormalizeEmail("  OWNER@Example.Test "); err != nil || email != "owner@example.test" {
		t.Fatalf("normalized email = %q with error %v", email, err)
	}
	for _, input := range []string{"", "Name <owner@example.test>", "owner@exam ple.test", "owner@example.test\x00"} {
		if _, err := NormalizeEmail(input); err == nil {
			t.Fatalf("accepted invalid email %q", input)
		}
	}
}

func TestJobAndProviderValidation(t *testing.T) {
	job := JobInput{SiteID: uuid.NewString(), Kind: JobSiteInventory, IdempotencyKey: "prepare:inventory-1"}
	if err := job.Validate(); err != nil {
		t.Fatal(err)
	}
	job.Kind = JobSiteBrief
	if err := job.Validate(); err == nil {
		t.Fatal("site brief without provider was accepted")
	}
	job.ProviderID = uuid.NewString()
	if err := job.Validate(); err != nil {
		t.Fatal(err)
	}
	job.IdempotencyKey = "invalid key"
	if err := job.Validate(); err == nil {
		t.Fatal("invalid idempotency key was accepted")
	}

	provider := ProviderInput{
		ProviderSettings: ProviderSettings{Name: " Team ", Model: "test-model", TimeoutSeconds: 10,
			RequestsPerMinute: 2, RequestsPerDay: 3, MaxOutputTokens: 1024, Enabled: true},
		APIKey: "local-validation-key",
	}
	if p, err := provider.Normalize(true); err != nil || p.Name != "Team" {
		t.Fatalf("normalize provider name: %q, %v", p.Name, err)
	}
	provider.APIKey, provider.Version = "", 1
	if _, err := provider.Normalize(false); err != nil {
		t.Fatalf("update should allow retaining the key: %v", err)
	}
	provider.RequestsPerDay = 0
	if _, err := provider.Normalize(false); err == nil {
		t.Fatal("unbounded provider budget accepted")
	}
}
