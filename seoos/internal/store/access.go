package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/Autumn-27/artex/seoos/internal/domain"
	"github.com/google/uuid"
)

func newCredential() (string, [32]byte, error) {
	var secret [32]byte
	if _, err := rand.Read(secret[:]); err != nil {
		return "", [32]byte{}, err
	}
	token := "seo_" + hex.EncodeToString(secret[:])
	return token, sha256.Sum256([]byte(token)), nil
}

func (s *Store) Authenticate(ctx context.Context, digest [32]byte) (domain.Principal, error) {
	principal := domain.Principal{Kind: "member"}
	err := s.db.QueryRowContext(ctx, `
		SELECT m.organization_id::text, m.user_id::text, u.email, m.role
		FROM public.seo_memberships m JOIN public.seo_users u ON u.id = m.user_id
		WHERE m.token_digest = $1`, digest[:],
	).Scan(&principal.OrganizationID, &principal.UserID, &principal.Email, &principal.Role)
	return principal, classifyError(err)
}

// ProvisionOrganization runs only from the database operator's CLI.
func (s *Store) ProvisionOrganization(ctx context.Context, name, email string) (string, domain.Credential, error) {
	name, err := domain.NormalizeOrganizationName(name)
	if err != nil {
		return "", domain.Credential{}, err
	}
	orgID := uuid.NewString()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", domain.Credential{}, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `INSERT INTO public.seo_organizations (id, name) VALUES ($1, $2)`, orgID, name); err != nil {
		return "", domain.Credential{}, err
	}
	if err := audit(ctx, tx, orgID, "organization.create", orgID, map[string]string{"name": name}); err != nil {
		return "", domain.Credential{}, err
	}
	credential, err := addMember(ctx, tx, orgID, email, domain.RoleOwner)
	if err != nil {
		return "", domain.Credential{}, err
	}
	if err := tx.Commit(); err != nil {
		return "", domain.Credential{}, err
	}
	return orgID, credential, nil
}

func (s *Store) AddMember(ctx context.Context, orgID, email string, role domain.Role) (domain.Credential, error) {
	tx, err := s.membershipTransaction(ctx, orgID)
	if err != nil {
		return domain.Credential{}, err
	}
	defer tx.Rollback()
	credential, err := addMember(ctx, tx, orgID, email, role)
	if err != nil {
		return domain.Credential{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Credential{}, err
	}
	return credential, nil
}

func addMember(ctx context.Context, tx *sql.Tx, orgID, email string, role domain.Role) (domain.Credential, error) {
	email, err := domain.NormalizeEmail(email)
	if err != nil {
		return domain.Credential{}, err
	}
	if !role.Valid() {
		return domain.Credential{}, &domain.ValidationError{Field: "role", Message: "must be owner, editor or viewer"}
	}
	token, digest, err := newCredential()
	if err != nil {
		return domain.Credential{}, err
	}
	member := domain.Member{Email: email, Role: role, Enabled: true}
	err = tx.QueryRowContext(ctx, `
		INSERT INTO public.seo_users (id, email) VALUES ($1, $2)
		ON CONFLICT (email) DO UPDATE SET email = EXCLUDED.email RETURNING id::text`,
		uuid.NewString(), email).Scan(&member.UserID)
	if err != nil {
		return domain.Credential{}, err
	}
	err = tx.QueryRowContext(ctx, `
		INSERT INTO public.seo_memberships (organization_id, user_id, role, token_digest)
		VALUES ($1, $2, $3, $4) ON CONFLICT (organization_id, user_id) DO NOTHING RETURNING created_at`,
		orgID, member.UserID, role, digest[:]).Scan(&member.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Credential{}, domain.ErrMemberExists
	}
	if err != nil {
		return domain.Credential{}, err
	}
	member.CreatedAt = member.CreatedAt.UTC()
	if err := audit(ctx, tx, orgID, "member.add", member.UserID, member); err != nil {
		return domain.Credential{}, err
	}
	return domain.Credential{Member: member, Token: token}, nil
}

const memberSelect = `SELECT m.user_id::text, u.email, m.role, m.token_digest IS NOT NULL, m.created_at
	FROM public.seo_memberships m JOIN public.seo_users u ON u.id = m.user_id`

func scanMember(row rowScanner) (domain.Member, error) {
	var member domain.Member
	err := row.Scan(&member.UserID, &member.Email, &member.Role, &member.Enabled, &member.CreatedAt)
	member.CreatedAt = member.CreatedAt.UTC()
	return member, classifyError(err)
}

func (s *Store) ListMembers(ctx context.Context, orgID string, limit, offset int) ([]domain.Member, error) {
	if err := validateOrganizationID(orgID); err != nil {
		return nil, err
	}
	if err := domain.ValidatePage(limit, offset); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, memberSelect+`
		WHERE m.organization_id = $1 ORDER BY m.created_at DESC, m.user_id DESC LIMIT $2 OFFSET $3`, orgID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	members := []domain.Member{}
	for rows.Next() {
		member, err := scanMember(rows)
		if err != nil {
			return nil, err
		}
		members = append(members, member)
	}
	return members, rows.Err()
}

func (s *Store) SetMemberRole(ctx context.Context, orgID, userID string, role domain.Role) (domain.Member, error) {
	if !role.Valid() {
		return domain.Member{}, &domain.ValidationError{Field: "role", Message: "must be owner, editor or viewer"}
	}
	return s.changeMember(ctx, orgID, userID, "member.role", func(ctx context.Context, tx *sql.Tx, member domain.Member) (domain.Member, error) {
		if member.Role == domain.RoleOwner && member.Enabled && role != domain.RoleOwner {
			if err := requireOtherOwner(ctx, tx, orgID, userID); err != nil {
				return member, err
			}
		}
		_, err := tx.ExecContext(ctx, `UPDATE public.seo_memberships SET role = $3
			WHERE organization_id = $1 AND user_id = $2`, orgID, userID, role)
		member.Role = role
		return member, err
	})
}

func (s *Store) RotateMemberToken(ctx context.Context, orgID, userID string) (domain.Credential, error) {
	token, digest, err := newCredential()
	if err != nil {
		return domain.Credential{}, err
	}
	member, err := s.changeMember(ctx, orgID, userID, "member.token.rotate", func(ctx context.Context, tx *sql.Tx, member domain.Member) (domain.Member, error) {
		_, err := tx.ExecContext(ctx, `UPDATE public.seo_memberships SET token_digest = $3
			WHERE organization_id = $1 AND user_id = $2`, orgID, userID, digest[:])
		member.Enabled = true
		return member, err
	})
	if err != nil {
		return domain.Credential{}, err
	}
	return domain.Credential{Member: member, Token: token}, nil
}

func (s *Store) RevokeMemberToken(ctx context.Context, orgID, userID string) error {
	_, err := s.changeMember(ctx, orgID, userID, "member.token.revoke", func(ctx context.Context, tx *sql.Tx, member domain.Member) (domain.Member, error) {
		if member.Role == domain.RoleOwner && member.Enabled {
			if err := requireOtherOwner(ctx, tx, orgID, userID); err != nil {
				return member, err
			}
		}
		_, err := tx.ExecContext(ctx, `UPDATE public.seo_memberships SET token_digest = NULL
			WHERE organization_id = $1 AND user_id = $2`, orgID, userID)
		member.Enabled = false
		return member, err
	})
	return err
}

func requireOtherOwner(ctx context.Context, tx *sql.Tx, orgID, userID string) error {
	var exists bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM public.seo_memberships
		WHERE organization_id = $1 AND user_id <> $2 AND role = 'owner' AND token_digest IS NOT NULL)`, orgID, userID).Scan(&exists)
	if err != nil {
		return err
	}
	if !exists {
		return domain.ErrLastOwner
	}
	return nil
}

func (s *Store) membershipTransaction(ctx context.Context, orgID string) (*sql.Tx, error) {
	if err := validateOrganizationID(orgID); err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	// Serialize membership changes per organization, including its last-owner check.
	var exists int
	err = tx.QueryRowContext(ctx, `SELECT 1 FROM public.seo_organizations WHERE id = $1 FOR NO KEY UPDATE`, orgID).Scan(&exists)
	if err != nil {
		_ = tx.Rollback()
		return nil, classifyError(err)
	}
	return tx, nil
}

func (s *Store) changeMember(ctx context.Context, orgID, userID, action string, change func(context.Context, *sql.Tx, domain.Member) (domain.Member, error)) (domain.Member, error) {
	if !domain.ValidID(userID) {
		return domain.Member{}, domain.ErrNotFound
	}
	tx, err := s.membershipTransaction(ctx, orgID)
	if err != nil {
		return domain.Member{}, err
	}
	defer tx.Rollback()
	before, err := scanMember(tx.QueryRowContext(ctx, memberSelect+` WHERE m.organization_id = $1 AND m.user_id = $2`, orgID, userID))
	if err != nil {
		return domain.Member{}, err
	}
	after, err := change(ctx, tx, before)
	if err != nil {
		return domain.Member{}, err
	}
	if err = audit(ctx, tx, orgID, action, userID, map[string]domain.Member{"before": before, "after": after}); err != nil {
		return domain.Member{}, err
	}
	return after, tx.Commit()
}

func audit(ctx context.Context, tx *sql.Tx, orgID, action, resourceID string, details any) error {
	principal := domain.PrincipalFromContext(ctx)
	if principal.Kind == "" {
		principal.Kind = "system"
	}
	data, err := json.Marshal(details)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO public.seo_audit_events (organization_id, actor_kind, actor_user_id, action, resource_id, details)
		VALUES ($1, $2, NULLIF($3, '')::uuid, $4, $5, $6::jsonb)`,
		orgID, principal.Kind, principal.UserID, action, resourceID, string(data))
	return err
}

func (s *Store) ListAudit(ctx context.Context, orgID string, limit, offset int) ([]domain.AuditEvent, error) {
	if err := validateOrganizationID(orgID); err != nil {
		return nil, err
	}
	if err := domain.ValidatePage(limit, offset); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, actor_kind, COALESCE(actor_user_id::text, ''), action, resource_id::text, details, created_at
		FROM public.seo_audit_events WHERE organization_id = $1 ORDER BY id DESC LIMIT $2 OFFSET $3`,
		orgID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	events := []domain.AuditEvent{}
	for rows.Next() {
		var event domain.AuditEvent
		if err := rows.Scan(&event.ID, &event.ActorKind, &event.ActorUserID, &event.Action, &event.ResourceID, &event.Details, &event.CreatedAt); err != nil {
			return nil, err
		}
		event.CreatedAt = event.CreatedAt.UTC()
		events = append(events, event)
	}
	return events, rows.Err()
}
