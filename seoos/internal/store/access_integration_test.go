package store

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Autumn-27/artex/seoos/internal/domain"
	"github.com/google/uuid"
)

func TestPostgresMembershipIsolationAndRotation(t *testing.T) {
	s, ctx, _ := openTestStore(t, true)
	email := uuid.NewString() + "@example.test"
	orgA, owner, err := s.ProvisionOrganization(ctx, "Organization A", email)
	if err != nil {
		t.Fatal(err)
	}
	orgB, other, err := s.ProvisionOrganization(ctx, "Organization B", strings.ToUpper(email))
	if err != nil {
		t.Fatal(err)
	}
	if orgA == orgB || owner.Member.UserID != other.Member.UserID || owner.Token == other.Token {
		t.Fatal("memberships must share identity and have separate credentials")
	}
	principal, err := s.Authenticate(ctx, sha256.Sum256([]byte(owner.Token)))
	if err != nil || principal.OrganizationID != orgA || principal.Role != domain.RoleOwner {
		t.Fatalf("owner not authenticated in the right organization: %#v %v", principal, err)
	}
	auth := domain.WithPrincipal(ctx, principal)
	viewer, err := s.AddMember(auth, orgA, "viewer-"+email, domain.RoleViewer)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetMemberRole(auth, orgB, viewer.Member.UserID, domain.RoleEditor); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("cross-organization role update: %v", err)
	}
	if _, err := s.SetMemberRole(auth, orgA, owner.Member.UserID, domain.RoleEditor); !errors.Is(err, domain.ErrLastOwner) {
		t.Fatalf("last owner demotion: %v", err)
	}
	if err := s.RevokeMemberToken(auth, orgA, owner.Member.UserID); !errors.Is(err, domain.ErrLastOwner) {
		t.Fatalf("last owner revocation: %v", err)
	}
	if _, err := s.SetMemberRole(auth, orgA, viewer.Member.UserID, domain.RoleEditor); err != nil {
		t.Fatal(err)
	}
	current, err := s.Authenticate(ctx, sha256.Sum256([]byte(viewer.Token)))
	if err != nil || current.Role != domain.RoleEditor {
		t.Fatalf("role change invisible on next request: %#v %v", current, err)
	}
	rotated, err := s.RotateMemberToken(auth, orgA, viewer.Member.UserID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Authenticate(ctx, sha256.Sum256([]byte(viewer.Token))); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("old credential still works: %v", err)
	}
	if err := s.RevokeMemberToken(auth, orgA, viewer.Member.UserID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Authenticate(ctx, sha256.Sum256([]byte(rotated.Token))); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("revoked credential still works: %v", err)
	}
	site, err := s.CreateSite(auth, orgA, validSiteInput())
	if err != nil {
		t.Fatal(err)
	}
	events, err := s.ListAudit(auth, orgA, 20, 0)
	if err != nil || len(events) != 7 || events[0].ActorUserID != principal.UserID || events[0].ResourceID != site.ID {
		t.Fatalf("missing scoped audit events: count %d, error %v", len(events), err)
	}
	data, err := json.Marshal(events)
	if err != nil || strings.Contains(string(data), viewer.Token) || strings.Contains(string(data), rotated.Token) {
		t.Fatal("audit encoding failed or exposed a credential")
	}
	if others, err := s.ListAudit(ctx, orgB, 20, 0); err != nil || len(others) != 2 {
		t.Fatalf("audit across organizations: count %d, error %v", len(others), err)
	}
}

func TestPostgresMutationRollsBackWithAudit(t *testing.T) {
	s, ctx, _ := openTestStore(t, true)
	orgID := testOrganization(t, s, ctx)
	unpersistedActor := domain.Principal{OrganizationID: orgID, UserID: uuid.NewString(), Kind: "member"}
	if _, err := s.CreateSite(domain.WithPrincipal(ctx, unpersistedActor), orgID, validSiteInput()); err == nil {
		t.Fatal("site created even though audit could not commit")
	}
	if sites, err := s.ListSites(ctx, orgID, 20, 0); err != nil || len(sites) != 0 {
		t.Fatalf("failed audit did not roll back the site: count %d, error %v", len(sites), err)
	}
}
