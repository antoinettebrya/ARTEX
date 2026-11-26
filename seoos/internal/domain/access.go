package domain

import (
	"context"
	"encoding/json"
	"errors"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"
)

type Role string

const (
	RoleOwner  Role = "owner"
	RoleEditor Role = "editor"
	RoleViewer Role = "viewer"
)

func (r Role) Valid() bool    { return r == RoleOwner || r == RoleEditor || r == RoleViewer }
func (r Role) CanWrite() bool { return r == RoleOwner || r == RoleEditor }

var (
	ErrMemberExists = errors.New("user is already a member of this organization")
	ErrLastOwner    = errors.New("an organization must retain an enabled owner")
)

type Principal struct {
	OrganizationID string `json:"organization_id"`
	UserID         string `json:"user_id,omitempty"`
	Email          string `json:"email,omitempty"`
	Role           Role   `json:"role"`
	Kind           string `json:"kind"`
}

type Member struct {
	UserID    string    `json:"user_id"`
	Email     string    `json:"email"`
	Role      Role      `json:"role"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
}

type Credential struct {
	Member Member `json:"member"`
	Token  string `json:"token"`
}

type AuditEvent struct {
	ID          int64           `json:"id"`
	ActorKind   string          `json:"actor_kind"`
	ActorUserID string          `json:"actor_user_id,omitempty"`
	Action      string          `json:"action"`
	ResourceID  string          `json:"resource_id"`
	Details     json.RawMessage `json:"details"`
	CreatedAt   time.Time       `json:"created_at"`
}

type AccessRepository interface {
	Authenticate(context.Context, [32]byte) (Principal, error)
	ListMembers(context.Context, string, int, int) ([]Member, error)
	AddMember(context.Context, string, string, Role) (Credential, error)
	SetMemberRole(context.Context, string, string, Role) (Member, error)
	RotateMemberToken(context.Context, string, string) (Credential, error)
	RevokeMemberToken(context.Context, string, string) error
	ListAudit(context.Context, string, int, int) ([]AuditEvent, error)
}

type principalKey struct{}

func WithPrincipal(ctx context.Context, principal Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, principal)
}

func PrincipalFromContext(ctx context.Context) Principal {
	principal, _ := ctx.Value(principalKey{}).(Principal)
	return principal
}

func NormalizeEmail(input string) (string, error) {
	email := strings.ToLower(strings.TrimSpace(input))
	parsed, err := mail.ParseAddress(email)
	if len(email) > 254 || err != nil || parsed.Address != email || !strings.Contains(email, "@") || strings.ContainsFunc(email, func(r rune) bool { return r < '!' || r > '~' }) {
		return "", invalid("email", "must be an email address of at most 254 ASCII characters")
	}
	return email, nil
}

func NormalizeOrganizationName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if !utf8.ValidString(name) || utf8.RuneCountInString(name) < 1 || utf8.RuneCountInString(name) > 200 || hasControl(name) {
		return "", invalid("organization_name", "must contain 1 to 200 characters without control characters")
	}
	return name, nil
}
