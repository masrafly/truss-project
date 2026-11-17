package usecase

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"truss-project/internal/modules/workspaces/domain"
)

const maxEmailLength = 255 // sesuai VARCHAR(255)

type MemberUseCase struct {
	members MemberRepository
	users   UserDirectory
	log     *slog.Logger
}

// log boleh nil (mis. pada unit test); dalam kasus itu log dibuang.
func NewMemberUseCase(members MemberRepository, users UserDirectory, log *slog.Logger) *MemberUseCase {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &MemberUseCase{members: members, users: users, log: log.With("module", "workspaces")}
}

// callerRole menentukan peran pemanggil di workspace. Non-anggota (atau workspace
// yang tidak ada, atau ID yang bukan UUID) menghasilkan ErrWorkspaceNotFound (404).
func (u *MemberUseCase) callerRole(
	ctx context.Context,
	actorID, workspaceID string,
) (actor, workspace uuid.UUID, role domain.Role, err error) {
	actor, err = uuid.Parse(actorID)
	if err != nil {
		return uuid.Nil, uuid.Nil, "", domain.ErrUnauthenticated
	}
	workspace, err = uuid.Parse(workspaceID)
	if err != nil {
		return uuid.Nil, uuid.Nil, "", domain.ErrWorkspaceNotFound
	}

	role, err = u.members.FindMemberRole(ctx, workspace, actor)
	if err != nil {
		if errors.Is(err, domain.ErrNotMember) {
			u.log.WarnContext(ctx, "akses workspace ditolak", "workspace_id", workspace, "user_id", actor)
			return uuid.Nil, uuid.Nil, "", domain.ErrWorkspaceNotFound
		}
		return uuid.Nil, uuid.Nil, "", err
	}
	return actor, workspace, role, nil
}

// Add menambahkan user terdaftar (berdasarkan email) sebagai anggota.
// Urutan pemeriksaan: keanggotaan pemanggil (404) -> validasi input (400) ->
// izin peran (403) -> user terdaftar (404) -> belum menjadi anggota (409).
func (u *MemberUseCase) Add(
	ctx context.Context,
	actorID, workspaceID, email, role string,
) (*domain.Member, error) {
	actor, wid, actorRole, err := u.callerRole(ctx, actorID, workspaceID)
	if err != nil {
		return nil, err
	}

	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" || utf8.RuneCountInString(email) > maxEmailLength {
		return nil, domain.ErrInvalidInput
	}
	if role == "" {
		role = string(domain.RoleMember) // default
	}
	newRole, ok := domain.ParseAssignableRole(role)
	if !ok {
		return nil, domain.ErrInvalidInput
	}

	if !domain.CanAddMember(actorRole, newRole) {
		u.log.WarnContext(ctx, "penambahan anggota ditolak: peran tidak cukup",
			"workspace_id", wid, "user_id", actor, "actor_role", actorRole, "requested_role", newRole)
		return nil, domain.ErrForbidden
	}

	target, err := u.users.FindByEmail(ctx, email)
	if err != nil {
		return nil, err // domain.ErrUserNotFound diteruskan apa adanya
	}

	joinedAt, err := u.members.AddMember(ctx, wid, target.ID, newRole)
	if err != nil {
		return nil, err // termasuk domain.ErrAlreadyMember
	}

	u.log.InfoContext(ctx, "anggota ditambahkan",
		"workspace_id", wid, "actor_id", actor, "member_id", target.ID, "role", newRole)

	return &domain.Member{
		UserID:   target.ID,
		Name:     target.Name,
		Email:    target.Email,
		Role:     newRole,
		JoinedAt: joinedAt,
	}, nil
}

// List menampilkan anggota workspace. Semua anggota boleh melihat; non-anggota mendapat 404.
func (u *MemberUseCase) List(ctx context.Context, actorID, workspaceID string) ([]domain.Member, error) {
	_, wid, _, err := u.callerRole(ctx, actorID, workspaceID)
	if err != nil {
		return nil, err
	}

	list, err := u.members.ListMembers(ctx, wid)
	if err != nil {
		return nil, err
	}
	if list == nil {
		list = []domain.Member{}
	}
	return list, nil
}

// Remove menghapus anggota (atau keluar dari workspace jika target adalah pemanggil sendiri).
func (u *MemberUseCase) Remove(ctx context.Context, actorID, workspaceID, targetUserID string) error {
	actor, wid, actorRole, err := u.callerRole(ctx, actorID, workspaceID)
	if err != nil {
		return err
	}

	target, err := uuid.Parse(targetUserID)
	if err != nil {
		return domain.ErrMemberNotFound
	}

	targetRole, err := u.members.FindMemberRole(ctx, wid, target)
	if err != nil {
		if errors.Is(err, domain.ErrNotMember) {
			return domain.ErrMemberNotFound
		}
		return err
	}

	if err := domain.CheckRemoveMember(actorRole, actor, targetRole, target); err != nil {
		u.log.WarnContext(ctx, "penghapusan anggota ditolak",
			"workspace_id", wid, "actor_id", actor, "actor_role", actorRole,
			"member_id", target, "member_role", targetRole, "reason", err.Error())
		return err
	}

	if err := u.members.RemoveMember(ctx, wid, target, actor); err != nil {
		if errors.Is(err, domain.ErrMemberOwnsProjects) {
			u.log.WarnContext(ctx, "penghapusan anggota ditolak: masih owner project",
				"workspace_id", wid, "actor_id", actor, "member_id", target)
		}
		return err
	}

	u.log.InfoContext(ctx, "anggota dihapus",
		"workspace_id", wid, "actor_id", actor, "member_id", target, "self", actor == target)
	return nil
}
