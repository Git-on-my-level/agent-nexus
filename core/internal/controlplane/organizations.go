package controlplane

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

func (s *Service) ListOrganizations(ctx context.Context, identity RequestIdentity, page PageRequest) (Page[Organization], error) {
	limit := normalizePageLimit(page.Limit)
	sortAt, sortID, err := decodeCursor(page.Cursor)
	if err != nil {
		return Page[Organization]{}, invalidRequest("cursor is invalid")
	}

	query := `SELECT o.id, o.slug, o.display_name, o.plan_tier, o.status, o.created_at, o.updated_at
		FROM organizations o
		JOIN organization_memberships m ON m.organization_id = o.id
		WHERE m.account_id = ? AND m.status = 'active'`
	args := []any{identity.Account.ID}
	if sortAt != "" {
		query += ` AND (o.created_at > ? OR (o.created_at = ? AND o.id > ?))`
		args = append(args, sortAt, sortAt, sortID)
	}
	query += ` ORDER BY o.created_at ASC, o.id ASC LIMIT ?`
	args = append(args, limit+1)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return Page[Organization]{}, internalError("failed to list organizations")
	}
	defer rows.Close()

	var organizations []Organization
	for rows.Next() {
		var organization Organization
		if err := rows.Scan(&organization.ID, &organization.Slug, &organization.DisplayName, &organization.PlanTier, &organization.Status, &organization.CreatedAt, &organization.UpdatedAt); err != nil {
			return Page[Organization]{}, internalError("failed to scan organization")
		}
		organizations = append(organizations, organization)
	}
	if err := rows.Err(); err != nil {
		return Page[Organization]{}, internalError("failed to iterate organizations")
	}
	return pageFromItems(organizations, limit, func(organization Organization) (string, string) {
		return organization.CreatedAt, organization.ID
	}), nil
}

func (s *Service) CreateOrganization(ctx context.Context, identity RequestIdentity, slug string, displayName string, planTier string) (Organization, Membership, error) {
	slug, err := normalizeSlug(slug)
	if err != nil {
		return Organization{}, Membership{}, err
	}
	displayName, err = normalizeDisplayName(displayName)
	if err != nil {
		return Organization{}, Membership{}, err
	}
	if err := validatePlanTier(planTier); err != nil {
		return Organization{}, Membership{}, err
	}

	now := s.now()
	nowText := now.Format(time.RFC3339Nano)
	organization := Organization{
		ID:          "org_" + uuid.NewString(),
		Slug:        slug,
		DisplayName: displayName,
		PlanTier:    planTier,
		Status:      "active",
		CreatedAt:   nowText,
		UpdatedAt:   nowText,
	}
	membership := Membership{
		ID:             "mem_" + uuid.NewString(),
		OrganizationID: organization.ID,
		AccountID:      identity.Account.ID,
		Role:           "owner",
		Status:         "active",
		CreatedAt:      nowText,
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Organization{}, Membership{}, internalError("failed to begin create organization transaction")
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO organizations(id, slug, display_name, plan_tier, status, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		organization.ID,
		organization.Slug,
		organization.DisplayName,
		organization.PlanTier,
		organization.Status,
		organization.CreatedAt,
		organization.UpdatedAt,
	); err != nil {
		if isSQLiteConstraint(err) {
			return Organization{}, Membership{}, conflict("slug_conflict", "organization slug is already in use")
		}
		return Organization{}, Membership{}, internalError("failed to create organization")
	}
	if err := upsertMembershipTx(ctx, tx, membership); err != nil {
		return Organization{}, Membership{}, err
	}
	if err := insertAuditEventTx(ctx, tx, AuditEvent{
		ID:             "audit_" + uuid.NewString(),
		EventType:      "organization_created",
		OrganizationID: stringPtr(organization.ID),
		TargetType:     "organization",
		TargetID:       organization.ID,
		OccurredAt:     nowText,
		Metadata: map[string]any{
			"plan_tier": organization.PlanTier,
			"slug":      organization.Slug,
		},
	}, stringPtr(identity.Account.ID)); err != nil {
		return Organization{}, Membership{}, err
	}
	if err := tx.Commit(); err != nil {
		return Organization{}, Membership{}, internalError("failed to commit organization creation")
	}

	return organization, membership, nil
}

func (s *Service) GetOrganization(ctx context.Context, identity RequestIdentity, organizationID string) (Organization, error) {
	organization, _, err := s.requireOrganizationAccess(ctx, identity, organizationID, false)
	return organization, err
}

func (s *Service) UpdateOrganization(ctx context.Context, identity RequestIdentity, organizationID string, displayName *string, planTier *string, status *string) (Organization, error) {
	organization, membership, err := s.requireOrganizationAccess(ctx, identity, organizationID, true)
	if err != nil {
		return Organization{}, err
	}
	if !membershipCanManage(membership.Role) {
		return Organization{}, accessDenied("organization updates require owner or admin access")
	}

	if displayName != nil {
		value, err := normalizeDisplayName(*displayName)
		if err != nil {
			return Organization{}, err
		}
		organization.DisplayName = value
	}
	if planTier != nil {
		if err := validatePlanTier(*planTier); err != nil {
			return Organization{}, err
		}
		organization.PlanTier = *planTier
	}
	if status != nil {
		if err := validateOrganizationStatus(*status); err != nil {
			return Organization{}, err
		}
		organization.Status = *status
	}
	organization.UpdatedAt = s.now().Format(time.RFC3339Nano)

	if _, err := s.db.ExecContext(
		ctx,
		`UPDATE organizations SET display_name = ?, plan_tier = ?, status = ?, updated_at = ? WHERE id = ?`,
		organization.DisplayName,
		organization.PlanTier,
		organization.Status,
		organization.UpdatedAt,
		organization.ID,
	); err != nil {
		return Organization{}, internalError("failed to update organization")
	}
	if err := insertAuditEvent(ctx, s.db, AuditEvent{
		ID:             "audit_" + uuid.NewString(),
		EventType:      "organization_updated",
		OrganizationID: stringPtr(organization.ID),
		TargetType:     "organization",
		TargetID:       organization.ID,
		OccurredAt:     organization.UpdatedAt,
		Metadata: map[string]any{
			"plan_tier": organization.PlanTier,
			"status":    organization.Status,
		},
	}, stringPtr(identity.Account.ID)); err != nil {
		return Organization{}, err
	}
	return organization, nil
}

func (s *Service) ListOrganizationMemberships(ctx context.Context, identity RequestIdentity, organizationID string, page PageRequest) (Page[Membership], error) {
	if _, _, err := s.requireOrganizationAccess(ctx, identity, organizationID, false); err != nil {
		return Page[Membership]{}, err
	}

	limit := normalizePageLimit(page.Limit)
	sortAt, sortID, err := decodeCursor(page.Cursor)
	if err != nil {
		return Page[Membership]{}, invalidRequest("cursor is invalid")
	}

	query := `SELECT id, organization_id, account_id, role, status, created_at
		FROM organization_memberships
		WHERE organization_id = ?`
	args := []any{organizationID}
	if sortAt != "" {
		query += ` AND (created_at > ? OR (created_at = ? AND id > ?))`
		args = append(args, sortAt, sortAt, sortID)
	}
	query += ` ORDER BY created_at ASC, id ASC LIMIT ?`
	args = append(args, limit+1)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return Page[Membership]{}, internalError("failed to list memberships")
	}
	defer rows.Close()

	var memberships []Membership
	for rows.Next() {
		var membership Membership
		if err := rows.Scan(&membership.ID, &membership.OrganizationID, &membership.AccountID, &membership.Role, &membership.Status, &membership.CreatedAt); err != nil {
			return Page[Membership]{}, internalError("failed to scan membership")
		}
		memberships = append(memberships, membership)
	}
	if err := rows.Err(); err != nil {
		return Page[Membership]{}, internalError("failed to iterate memberships")
	}
	return pageFromItems(memberships, limit, func(membership Membership) (string, string) {
		return membership.CreatedAt, membership.ID
	}), nil
}

func (s *Service) UpdateOrganizationMembership(ctx context.Context, identity RequestIdentity, organizationID string, membershipID string, role *string, status *string) (Membership, error) {
	organization, callerMembership, err := s.requireOrganizationAccess(ctx, identity, organizationID, true)
	if err != nil {
		return Membership{}, err
	}
	if !membershipCanManage(callerMembership.Role) {
		return Membership{}, accessDenied("membership updates require owner or admin access")
	}

	membership, err := loadMembership(ctx, s.db, organizationID, membershipID)
	if err != nil {
		if err == sql.ErrNoRows {
			return Membership{}, notFound("membership not found")
		}
		return Membership{}, internalError("failed to load membership")
	}
	if role != nil {
		if err := validateMembershipRole(*role); err != nil {
			return Membership{}, err
		}
		membership.Role = *role
	}
	if status != nil {
		if err := validateMembershipStatus(*status); err != nil {
			return Membership{}, err
		}
		membership.Status = *status
	}

	if _, err := s.db.ExecContext(
		ctx,
		`UPDATE organization_memberships SET role = ?, status = ? WHERE id = ?`,
		membership.Role,
		membership.Status,
		membership.ID,
	); err != nil {
		return Membership{}, internalError("failed to update membership")
	}
	if err := insertAuditEvent(ctx, s.db, AuditEvent{
		ID:             "audit_" + uuid.NewString(),
		EventType:      "organization_membership_updated",
		OrganizationID: stringPtr(organization.ID),
		TargetType:     "membership",
		TargetID:       membership.ID,
		OccurredAt:     s.now().Format(time.RFC3339Nano),
		Metadata: map[string]any{
			"role":   membership.Role,
			"status": membership.Status,
		},
	}, stringPtr(identity.Account.ID)); err != nil {
		return Membership{}, err
	}
	return membership, nil
}

func (s *Service) ListOrganizationInvites(ctx context.Context, identity RequestIdentity, organizationID string, page PageRequest) (Page[OrganizationInvite], error) {
	if _, _, err := s.requireOrganizationAccess(ctx, identity, organizationID, false); err != nil {
		return Page[OrganizationInvite]{}, err
	}

	limit := normalizePageLimit(page.Limit)
	sortAt, sortID, err := decodeCursor(page.Cursor)
	if err != nil {
		return Page[OrganizationInvite]{}, invalidRequest("cursor is invalid")
	}
	nowText := s.now().Format(time.RFC3339Nano)
	query := `SELECT id, organization_id, email, role,
			CASE WHEN status = 'pending' AND expires_at <= ? THEN 'expired' ELSE status END,
			created_at, expires_at, accepted_at, revoked_at
		FROM organization_invites
		WHERE organization_id = ?`
	args := []any{nowText, organizationID}
	if sortAt != "" {
		query += ` AND (created_at > ? OR (created_at = ? AND id > ?))`
		args = append(args, sortAt, sortAt, sortID)
	}
	query += ` ORDER BY created_at ASC, id ASC LIMIT ?`
	args = append(args, limit+1)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return Page[OrganizationInvite]{}, internalError("failed to list invites")
	}
	defer rows.Close()

	var invites []OrganizationInvite
	for rows.Next() {
		invite, err := scanInvite(rows)
		if err != nil {
			return Page[OrganizationInvite]{}, err
		}
		invites = append(invites, invite)
	}
	if err := rows.Err(); err != nil {
		return Page[OrganizationInvite]{}, internalError("failed to iterate invites")
	}
	return pageFromItems(invites, limit, func(invite OrganizationInvite) (string, string) {
		return invite.CreatedAt, invite.ID
	}), nil
}

func (s *Service) CreateOrganizationInvite(ctx context.Context, identity RequestIdentity, organizationID string, email string, role string) (OrganizationInvite, string, error) {
	organization, membership, err := s.requireOrganizationAccess(ctx, identity, organizationID, true)
	if err != nil {
		return OrganizationInvite{}, "", err
	}
	if !membershipCanManage(membership.Role) {
		return OrganizationInvite{}, "", accessDenied("organization invites require owner or admin access")
	}
	email, err = normalizeEmail(email)
	if err != nil {
		return OrganizationInvite{}, "", err
	}
	if err := validateInviteRole(role); err != nil {
		return OrganizationInvite{}, "", err
	}

	var pendingExists bool
	if err := s.db.QueryRowContext(
		ctx,
		`SELECT EXISTS(
			SELECT 1 FROM organization_invites
			WHERE organization_id = ? AND email = ? AND status = 'pending' AND expires_at > ?
			LIMIT 1
		)`,
		organizationID,
		email,
		s.now().Format(time.RFC3339Nano),
	).Scan(&pendingExists); err != nil {
		return OrganizationInvite{}, "", internalError("failed to inspect existing invites")
	}
	if pendingExists {
		return OrganizationInvite{}, "", conflict("invite_conflict", "a pending invite already exists for that email")
	}

	now := s.now()
	nowText := now.Format(time.RFC3339Nano)
	expiresAt := now.Add(s.inviteTTL).Format(time.RFC3339Nano)
	token, err := randomBase64URL(32)
	if err != nil {
		return OrganizationInvite{}, "", internalError("failed to generate invite token")
	}
	invite := OrganizationInvite{
		ID:             "inv_" + uuid.NewString(),
		OrganizationID: organizationID,
		Email:          email,
		Role:           role,
		Status:         "pending",
		CreatedAt:      nowText,
		ExpiresAt:      expiresAt,
	}
	if _, err := s.db.ExecContext(
		ctx,
		`INSERT INTO organization_invites(
			id, organization_id, email, role, status, token_hash, created_at, expires_at, accepted_at, accepted_by_account_id, revoked_at, revoked_by_account_id
		 ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, NULL, NULL, NULL, NULL)`,
		invite.ID,
		invite.OrganizationID,
		invite.Email,
		invite.Role,
		invite.Status,
		hashToken(token),
		invite.CreatedAt,
		invite.ExpiresAt,
	); err != nil {
		return OrganizationInvite{}, "", internalError("failed to create invite")
	}
	if err := insertAuditEvent(ctx, s.db, AuditEvent{
		ID:             "audit_" + uuid.NewString(),
		EventType:      "organization_invite_created",
		OrganizationID: stringPtr(organization.ID),
		TargetType:     "organization_invite",
		TargetID:       invite.ID,
		OccurredAt:     invite.CreatedAt,
		Metadata: map[string]any{
			"email": credentialSafeEmail(email),
			"role":  role,
		},
	}, stringPtr(identity.Account.ID)); err != nil {
		return OrganizationInvite{}, "", err
	}
	return invite, formatTemplateURL(s.inviteURLTemplate, token), nil
}

func (s *Service) RevokeOrganizationInvite(ctx context.Context, identity RequestIdentity, organizationID string, inviteID string) (OrganizationInvite, error) {
	organization, membership, err := s.requireOrganizationAccess(ctx, identity, organizationID, true)
	if err != nil {
		return OrganizationInvite{}, err
	}
	if !membershipCanManage(membership.Role) {
		return OrganizationInvite{}, accessDenied("organization invite revocation requires owner or admin access")
	}

	invite, err := loadInvite(ctx, s.db, organizationID, inviteID, s.now())
	if err != nil {
		if err == sql.ErrNoRows {
			return OrganizationInvite{}, notFound("invite not found")
		}
		return OrganizationInvite{}, internalError("failed to load invite")
	}
	nowText := s.now().Format(time.RFC3339Nano)
	if _, err := s.db.ExecContext(
		ctx,
		`UPDATE organization_invites
		 SET status = 'revoked', revoked_at = ?, revoked_by_account_id = ?
		 WHERE id = ? AND organization_id = ?`,
		nowText,
		identity.Account.ID,
		invite.ID,
		organizationID,
	); err != nil {
		return OrganizationInvite{}, internalError("failed to revoke invite")
	}
	invite.Status = "revoked"
	invite.RevokedAt = stringPtr(nowText)
	if err := insertAuditEvent(ctx, s.db, AuditEvent{
		ID:             "audit_" + uuid.NewString(),
		EventType:      "organization_invite_revoked",
		OrganizationID: stringPtr(organization.ID),
		TargetType:     "organization_invite",
		TargetID:       invite.ID,
		OccurredAt:     nowText,
	}, stringPtr(identity.Account.ID)); err != nil {
		return OrganizationInvite{}, err
	}
	return invite, nil
}

func (s *Service) GetUsageSummary(ctx context.Context, identity RequestIdentity, organizationID string) (UsageSummary, error) {
	organization, _, err := s.requireOrganizationAccess(ctx, identity, organizationID, false)
	if err != nil {
		return UsageSummary{}, err
	}
	plan := planForTier(organization.PlanTier)

	var workspaceCount int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM workspaces WHERE organization_id = ?`, organizationID).Scan(&workspaceCount); err != nil {
		return UsageSummary{}, internalError("failed to count workspaces")
	}
	var humanSeatCount int
	if err := s.db.QueryRowContext(
		ctx,
		`SELECT COUNT(1) FROM organization_memberships WHERE organization_id = ? AND status = 'active'`,
		organizationID,
	).Scan(&humanSeatCount); err != nil {
		return UsageSummary{}, internalError("failed to count memberships")
	}
	monthStart := time.Date(s.now().Year(), s.now().Month(), 1, 0, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	var monthlyLaunchCount int
	if err := s.db.QueryRowContext(
		ctx,
		`SELECT COUNT(1) FROM launch_sessions WHERE workspace_id IN (
			SELECT id FROM workspaces WHERE organization_id = ?
		) AND created_at >= ?`,
		organizationID,
		monthStart,
	).Scan(&monthlyLaunchCount); err != nil {
		return UsageSummary{}, internalError("failed to count launches")
	}

	usage := UsageMeter{
		WorkspaceCount:     workspaceCount,
		HumanSeatCount:     humanSeatCount,
		StorageGB:          0,
		MonthlyLaunchCount: monthlyLaunchCount,
	}
	return UsageSummary{
		OrganizationID: organizationID,
		Plan:           plan,
		Usage:          usage,
		Quota: UsageQuota{
			WorkspacesRemaining: max(0, plan.WorkspaceLimit-workspaceCount),
			HumanSeatsRemaining: max(0, plan.HumanSeatLimit-humanSeatCount),
			StorageGBRemaining:  max(0, plan.IncludedStorageGB-usage.StorageGB),
		},
	}, nil
}

func (s *Service) requireOrganizationAccess(ctx context.Context, identity RequestIdentity, organizationID string, includeSuspended bool) (Organization, Membership, error) {
	organizationID = strings.TrimSpace(organizationID)
	if organizationID == "" {
		return Organization{}, Membership{}, invalidRequest("organization_id is required")
	}

	row := s.db.QueryRowContext(
		ctx,
		`SELECT
			o.id, o.slug, o.display_name, o.plan_tier, o.status, o.created_at, o.updated_at,
			m.id, m.account_id, m.role, m.status, m.created_at
		 FROM organizations o
		 JOIN organization_memberships m ON m.organization_id = o.id
		 WHERE o.id = ? AND m.account_id = ?`,
		organizationID,
		identity.Account.ID,
	)
	var (
		organization Organization
		membership   Membership
	)
	if err := row.Scan(
		&organization.ID,
		&organization.Slug,
		&organization.DisplayName,
		&organization.PlanTier,
		&organization.Status,
		&organization.CreatedAt,
		&organization.UpdatedAt,
		&membership.ID,
		&membership.AccountID,
		&membership.Role,
		&membership.Status,
		&membership.CreatedAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return Organization{}, Membership{}, notFound("organization not found")
		}
		return Organization{}, Membership{}, internalError("failed to load organization access")
	}
	membership.OrganizationID = organization.ID
	if membership.Status != "active" {
		return Organization{}, Membership{}, accessDenied("organization membership is disabled")
	}
	if !includeSuspended && organization.Status != "active" {
		return Organization{}, Membership{}, accessDenied("organization is not active")
	}
	return organization, membership, nil
}

func loadMembership(ctx context.Context, db *sql.DB, organizationID string, membershipID string) (Membership, error) {
	var membership Membership
	err := db.QueryRowContext(
		ctx,
		`SELECT id, organization_id, account_id, role, status, created_at
		 FROM organization_memberships
		 WHERE organization_id = ? AND id = ?`,
		organizationID,
		membershipID,
	).Scan(&membership.ID, &membership.OrganizationID, &membership.AccountID, &membership.Role, &membership.Status, &membership.CreatedAt)
	return membership, err
}

func loadInvite(ctx context.Context, db *sql.DB, organizationID string, inviteID string, now time.Time) (OrganizationInvite, error) {
	row := db.QueryRowContext(
		ctx,
		`SELECT id, organization_id, email, role,
			CASE WHEN status = 'pending' AND expires_at <= ? THEN 'expired' ELSE status END,
			created_at, expires_at, accepted_at, revoked_at
		 FROM organization_invites
		 WHERE organization_id = ? AND id = ?`,
		now.Format(time.RFC3339Nano),
		organizationID,
		inviteID,
	)
	return scanInvite(row)
}

type inviteScanner interface {
	Scan(dest ...any) error
}

func scanInvite(scanner inviteScanner) (OrganizationInvite, error) {
	var (
		invite     OrganizationInvite
		acceptedAt sql.NullString
		revokedAt  sql.NullString
	)
	if err := scanner.Scan(&invite.ID, &invite.OrganizationID, &invite.Email, &invite.Role, &invite.Status, &invite.CreatedAt, &invite.ExpiresAt, &acceptedAt, &revokedAt); err != nil {
		if err == sql.ErrNoRows {
			return OrganizationInvite{}, err
		}
		return OrganizationInvite{}, internalError("failed to scan invite")
	}
	invite.AcceptedAt = nullableString(acceptedAt)
	invite.RevokedAt = nullableString(revokedAt)
	return invite, nil
}

func membershipCanManage(role string) bool {
	role = strings.TrimSpace(role)
	return role == "owner" || role == "admin"
}

func validatePlanTier(planTier string) error {
	switch strings.TrimSpace(planTier) {
	case "starter", "team", "scale", "enterprise":
		return nil
	default:
		return invalidRequest("plan_tier must be one of starter, team, scale, or enterprise")
	}
}

func validateOrganizationStatus(status string) error {
	switch strings.TrimSpace(status) {
	case "active", "suspended":
		return nil
	default:
		return invalidRequest("status must be active or suspended")
	}
}

func validateMembershipRole(role string) error {
	switch strings.TrimSpace(role) {
	case "owner", "admin", "member", "viewer":
		return nil
	default:
		return invalidRequest("role must be owner, admin, member, or viewer")
	}
}

func validateInviteRole(role string) error {
	switch strings.TrimSpace(role) {
	case "admin", "member", "viewer":
		return nil
	default:
		return invalidRequest("role must be admin, member, or viewer")
	}
}

func validateMembershipStatus(status string) error {
	switch strings.TrimSpace(status) {
	case "active", "disabled":
		return nil
	default:
		return invalidRequest("status must be active or disabled")
	}
}

func planForTier(planTier string) UsagePlan {
	switch planTier {
	case "starter":
		return UsagePlan{ID: "starter", DisplayName: "Starter", WorkspaceLimit: 1, HumanSeatLimit: 5, IncludedStorageGB: 10}
	case "scale":
		return UsagePlan{ID: "scale", DisplayName: "Scale", WorkspaceLimit: 25, HumanSeatLimit: 200, IncludedStorageGB: 1000}
	case "enterprise":
		return UsagePlan{ID: "enterprise", DisplayName: "Enterprise", WorkspaceLimit: 100, HumanSeatLimit: 1000, IncludedStorageGB: 5000}
	default:
		return UsagePlan{ID: "team", DisplayName: "Team", WorkspaceLimit: 5, HumanSeatLimit: 25, IncludedStorageGB: 100}
	}
}

func formatTemplateURL(template string, value string) string {
	if strings.Contains(template, "%s") {
		return fmt.Sprintf(template, value)
	}
	return strings.TrimRight(template, "/") + "/" + value
}

func max(left int, right int) int {
	if left > right {
		return left
	}
	return right
}
