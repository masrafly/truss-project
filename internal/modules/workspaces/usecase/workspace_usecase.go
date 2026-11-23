// Package usecase berisi logika bisnis workspaces, termasuk otorisasi:
// hanya anggota yang boleh melihat sebuah workspace.
package usecase

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"truss-project/internal/modules/workspaces/domain"
	"truss-project/internal/platform/pagination"
)

const (
	maxNameLength        = 100  // karakter, sesuai VARCHAR(100)
	maxDescriptionLength = 2000 // karakter
)

type WorkspaceUseCase struct {
	repo WorkspaceRepository
	log  *slog.Logger
}

// log boleh nil (mis. pada unit test); dalam kasus itu log dibuang.
func NewWorkspaceUseCase(repo WorkspaceRepository, log *slog.Logger) *WorkspaceUseCase {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &WorkspaceUseCase{repo: repo, log: log.With("module", "workspaces")}
}

// Create membuat workspace; pembuatnya otomatis menjadi Owner.
// userID diambil dari identitas terautentikasi, bukan dari body request.
func (u *WorkspaceUseCase) Create(
	ctx context.Context,
	userID string,
	name string,
	description *string,
) (*domain.WorkspaceAccess, error) {
	creator, err := uuid.Parse(userID)
	if err != nil {
		return nil, domain.ErrUnauthenticated
	}

	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > maxNameLength {
		return nil, domain.ErrInvalidInput
	}

	var desc *string
	if description != nil {
		d := strings.TrimSpace(*description)
		if utf8.RuneCountInString(d) > maxDescriptionLength {
			return nil, domain.ErrInvalidInput
		}
		if d != "" {
			desc = &d
		}
	}

	w := &domain.Workspace{Name: name, Description: desc, CreatedBy: creator}
	if err := u.repo.CreateWithOwner(ctx, w); err != nil {
		if errors.Is(err, domain.ErrUserNotFound) {
			// User pada token sudah tidak ada di database.
			return nil, domain.ErrUnauthenticated
		}
		return nil, err
	}

	u.log.InfoContext(ctx, "workspace dibuat", "workspace_id", w.ID, "user_id", creator)

	return &domain.WorkspaceAccess{Workspace: *w, Role: domain.RoleOwner}, nil
}

// List mengembalikan hanya workspace tempat user menjadi anggota, satu halaman per panggilan.
func (u *WorkspaceUseCase) List(
	ctx context.Context,
	userID string,
	page, limit int,
) (*pagination.Result[domain.WorkspaceAccess], error) {
	uid, err := uuid.Parse(userID)
	if err != nil {
		return nil, domain.ErrUnauthenticated
	}
	p, err := pagination.New(page, limit, defaultListLimit, maxListLimit)
	if err != nil {
		return nil, domain.ErrInvalidInput
	}

	list, total, err := u.repo.ListByMember(ctx, uid, p.Limit, p.Offset)
	if err != nil {
		return nil, err
	}
	return pagination.NewResult(list, total, p), nil
}

// Get mengembalikan detail workspace untuk anggotanya. Non-anggota, workspace yang
// tidak ada, dan ID yang bukan UUID semuanya menghasilkan ErrWorkspaceNotFound.
func (u *WorkspaceUseCase) Get(
	ctx context.Context,
	userID string,
	workspaceID string,
) (*domain.WorkspaceAccess, error) {
	uid, err := uuid.Parse(userID)
	if err != nil {
		return nil, domain.ErrUnauthenticated
	}
	wid, err := uuid.Parse(workspaceID)
	if err != nil {
		return nil, domain.ErrWorkspaceNotFound
	}

	// Otorisasi lebih dulu: cek keanggotaan sebelum membaca data workspace.
	role, err := u.repo.FindMemberRole(ctx, wid, uid)
	if err != nil {
		if errors.Is(err, domain.ErrNotMember) {
			u.log.WarnContext(ctx, "akses workspace ditolak", "workspace_id", wid, "user_id", uid)
			return nil, domain.ErrWorkspaceNotFound
		}
		return nil, err
	}

	w, err := u.repo.FindByID(ctx, wid)
	if err != nil {
		return nil, err
	}

	return &domain.WorkspaceAccess{Workspace: *w, Role: role}, nil
}
