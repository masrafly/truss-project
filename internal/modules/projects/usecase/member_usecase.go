package usecase

import (
	"context"
	"log/slog"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"truss-project/internal/modules/projects/domain"
	"truss-project/internal/platform/pagination"
)

const maxEmailLength = 255 // sesuai VARCHAR(255)

type MemberUseCase struct {
	repo MemberRepository
	log  *slog.Logger
}

// log boleh nil (mis. pada unit test); dalam kasus itu log dibuang.
func NewMemberUseCase(repo MemberRepository, log *slog.Logger) *MemberUseCase {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &MemberUseCase{repo: repo, log: log.With("module", "projects")}
}

// Add menambahkan anggota workspace (berdasarkan email) ke project.
// Urutan pemeriksaan: dapat melihat project (404) -> validasi input (400) ->
// izin peran (403) -> anggota workspace (404) -> belum menjadi anggota project (409).
func (u *MemberUseCase) Add(
	ctx context.Context,
	actorID, projectID, email, role string,
) (*domain.Member, error) {
	actor, p, err := loadViewable(ctx, u.repo, u.log, actorID, projectID)
	if err != nil {
		return nil, err
	}

	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" || utf8.RuneCountInString(email) > maxEmailLength {
		return nil, domain.ErrInvalidInput
	}
	if role == "" {
		role = string(domain.ProjectMember) // default
	}
	newRole, ok := domain.ParseAssignableRole(role)
	if !ok {
		return nil, domain.ErrInvalidInput
	}

	if !p.CanAddMember(newRole) {
		u.log.WarnContext(ctx, "penambahan anggota project ditolak: peran tidak cukup",
			"project_id", p.ID, "user_id", actor,
			"workspace_role", p.WorkspaceRole, "project_role", p.ProjectRole, "requested_role", newRole)
		return nil, domain.ErrForbidden
	}

	target, err := u.repo.FindWorkspaceMemberByEmail(ctx, p.ID, email)
	if err != nil {
		return nil, err // termasuk domain.ErrWorkspaceUserNotFound
	}

	joinedAt, err := u.repo.AddMember(ctx, p.ID, target.ID, newRole)
	if err != nil {
		return nil, err // termasuk ErrAlreadyMember dan ErrWorkspaceUserNotFound (balapan)
	}

	u.log.InfoContext(ctx, "anggota project ditambahkan",
		"project_id", p.ID, "actor_id", actor, "member_id", target.ID, "role", newRole)

	return &domain.Member{
		UserID:   target.ID,
		Name:     target.Name,
		Email:    target.Email,
		Role:     newRole,
		JoinedAt: joinedAt,
	}, nil
}

// List: semua yang boleh melihat project boleh melihat anggotanya.
func (u *MemberUseCase) List(
	ctx context.Context,
	actorID, projectID string,
	page, limit int,
) (*pagination.Result[domain.Member], error) {
	_, p, err := loadViewable(ctx, u.repo, u.log, actorID, projectID)
	if err != nil {
		return nil, err
	}
	pg, err := pagination.New(page, limit, defaultListLimit, maxListLimit)
	if err != nil {
		return nil, domain.ErrInvalidInput
	}
	list, total, err := u.repo.ListMembers(ctx, p.ID, pg.Limit, pg.Offset)
	if err != nil {
		return nil, err
	}
	return pagination.NewResult(list, total, pg), nil
}

// ChangeRole mengubah peran anggota project (admin <-> member). Hanya tingkat penuh: Owner
// project atau Owner/Admin workspace. Urutan: dapat melihat project (404) -> validasi role
// (400) -> izin (403) -> target anggota (404) -> target bukan Owner (409).
func (u *MemberUseCase) ChangeRole(
	ctx context.Context,
	actorID, projectID, targetUserID, role string,
) (*domain.Member, error) {
	actor, p, err := loadViewable(ctx, u.repo, u.log, actorID, projectID)
	if err != nil {
		return nil, err
	}
	newRole, ok := domain.ParseAssignableRole(role)
	if !ok {
		return nil, domain.ErrInvalidInput
	}
	if !p.CanManageRoles() {
		u.log.WarnContext(ctx, "ubah peran anggota project ditolak: peran tidak cukup",
			"project_id", p.ID, "actor_id", actor,
			"workspace_role", p.WorkspaceRole, "project_role", p.ProjectRole)
		return nil, domain.ErrForbidden
	}
	target, err := uuid.Parse(targetUserID)
	if err != nil {
		return nil, domain.ErrMemberNotFound
	}

	m, err := u.repo.ChangeMemberRole(ctx, p.ID, target, newRole)
	if err != nil {
		return nil, err
	}
	u.log.InfoContext(ctx, "peran anggota project diubah",
		"project_id", p.ID, "actor_id", actor, "member_id", target, "role", newRole)
	return m, nil
}

// TransferOwnership memindahkan kepemilikan project ke anggota project lain; Owner lama
// menjadi Admin. Hanya tingkat penuh. Urutan: dapat melihat (404) -> izin (403) -> target
// anggota (404) -> target bukan Owner saat ini (400).
func (u *MemberUseCase) TransferOwnership(ctx context.Context, actorID, projectID, targetUserID string) error {
	actor, p, err := loadViewable(ctx, u.repo, u.log, actorID, projectID)
	if err != nil {
		return err
	}
	if !p.CanManageRoles() {
		u.log.WarnContext(ctx, "transfer kepemilikan project ditolak: peran tidak cukup",
			"project_id", p.ID, "actor_id", actor,
			"workspace_role", p.WorkspaceRole, "project_role", p.ProjectRole)
		return domain.ErrForbidden
	}
	target, err := uuid.Parse(targetUserID)
	if err != nil {
		return domain.ErrMemberNotFound
	}

	if err := u.repo.TransferOwnership(ctx, p.ID, target); err != nil {
		return err
	}
	u.log.InfoContext(ctx, "kepemilikan project dipindahkan",
		"project_id", p.ID, "actor_id", actor, "to_user_id", target)
	return nil
}

// Remove menghapus anggota project, atau keluar bila target adalah pemanggil sendiri.
func (u *MemberUseCase) Remove(ctx context.Context, actorID, projectID, targetUserID string) error {
	actor, p, err := loadViewable(ctx, u.repo, u.log, actorID, projectID)
	if err != nil {
		return err
	}

	target, err := uuid.Parse(targetUserID)
	if err != nil {
		return domain.ErrMemberNotFound
	}
	targetRole, err := u.repo.FindMemberRole(ctx, p.ID, target)
	if err != nil {
		return err // termasuk domain.ErrMemberNotFound
	}

	if err := p.CheckRemoveMember(actor, targetRole, target); err != nil {
		u.log.WarnContext(ctx, "penghapusan anggota project ditolak",
			"project_id", p.ID, "actor_id", actor,
			"workspace_role", p.WorkspaceRole, "project_role", p.ProjectRole,
			"member_id", target, "member_role", targetRole, "reason", err.Error())
		return err
	}

	if err := u.repo.RemoveMember(ctx, p.ID, target, actor); err != nil {
		return err // termasuk domain.ErrMemberNotFound
	}

	u.log.InfoContext(ctx, "anggota project dihapus",
		"project_id", p.ID, "actor_id", actor, "member_id", target, "self", actor == target)
	return nil
}
