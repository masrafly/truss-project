// Package usecase berisi logika bisnis projects, termasuk otorisasi berdasarkan
// peran workspace dan peran project (lihat domain/permissions.go).
package usecase

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"truss-project/internal/modules/projects/domain"
	"truss-project/internal/platform/pagination"
)

const (
	maxNameLength        = 100  // karakter, sesuai VARCHAR(100)
	maxDescriptionLength = 2000 // karakter

	// Pagination daftar (project dan anggota project).
	defaultListLimit = 20
	maxListLimit     = 100
)

// UpdateInput: field nil berarti tidak diubah. Description "" menghapus deskripsi.
type UpdateInput struct {
	Name        *string
	Description *string
	Status      *string
}

type ProjectUseCase struct {
	repo ProjectRepository
	log  *slog.Logger
}

// log boleh nil (mis. pada unit test); dalam kasus itu log dibuang.
func NewProjectUseCase(repo ProjectRepository, log *slog.Logger) *ProjectUseCase {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &ProjectUseCase{repo: repo, log: log.With("module", "projects")}
}

// Create membuat project di workspace. Urutan pemeriksaan: keanggotaan workspace (404)
// -> validasi input (400) -> peran workspace Owner/Admin (403).
func (u *ProjectUseCase) Create(
	ctx context.Context,
	userID, workspaceID, name string,
	description *string,
) (*domain.ProjectAccess, error) {
	uid, wid, wsRole, err := u.workspaceRole(ctx, userID, workspaceID)
	if err != nil {
		return nil, err
	}

	cleanName, err := validateName(name)
	if err != nil {
		return nil, err
	}
	desc, err := validateDescription(description)
	if err != nil {
		return nil, err
	}

	if !domain.CanCreateProject(wsRole) {
		u.log.WarnContext(ctx, "pembuatan project ditolak: peran tidak cukup",
			"workspace_id", wid, "user_id", uid, "workspace_role", wsRole)
		return nil, domain.ErrForbidden
	}

	p := &domain.Project{
		WorkspaceID: wid,
		Name:        cleanName,
		Description: desc,
		Status:      domain.StatusActive,
		CreatedBy:   uid,
	}
	if err := u.repo.CreateWithOwner(ctx, p); err != nil {
		switch {
		case errors.Is(err, domain.ErrNotWorkspaceMember):
			// Pembuat dikeluarkan dari workspace di tengah proses.
			return nil, domain.ErrWorkspaceNotFound
		case errors.Is(err, domain.ErrUserNotFound):
			return nil, domain.ErrUnauthenticated
		}
		return nil, err
	}

	u.log.InfoContext(ctx, "project dibuat", "project_id", p.ID, "workspace_id", wid, "user_id", uid)

	return &domain.ProjectAccess{
		Project: *p,
		Access:  domain.Access{WorkspaceRole: wsRole, ProjectRole: domain.ProjectOwner},
	}, nil
}

// ListInWorkspace: project yang boleh dilihat user di satu workspace. Non-anggota -> 404.
func (u *ProjectUseCase) ListInWorkspace(
	ctx context.Context,
	userID, workspaceID string,
	page, limit int,
) (*pagination.Result[domain.ProjectAccess], error) {
	uid, wid, _, err := u.workspaceRole(ctx, userID, workspaceID)
	if err != nil {
		return nil, err
	}
	return u.list(ctx, uid, &wid, page, limit)
}

// ListAll: project yang boleh dilihat user di semua workspace-nya.
func (u *ProjectUseCase) ListAll(
	ctx context.Context,
	userID string,
	page, limit int,
) (*pagination.Result[domain.ProjectAccess], error) {
	uid, err := uuid.Parse(userID)
	if err != nil {
		return nil, domain.ErrUnauthenticated
	}
	return u.list(ctx, uid, nil, page, limit)
}

func (u *ProjectUseCase) list(
	ctx context.Context,
	uid uuid.UUID,
	wid *uuid.UUID,
	page, limit int,
) (*pagination.Result[domain.ProjectAccess], error) {
	p, err := pagination.New(page, limit, defaultListLimit, maxListLimit)
	if err != nil {
		return nil, domain.ErrInvalidInput
	}
	list, total, err := u.repo.ListAccessible(ctx, uid, wid, p.Limit, p.Offset)
	if err != nil {
		return nil, err
	}
	// Pertahanan berlapis: saring ulang dengan aturan domain.
	out := make([]domain.ProjectAccess, 0, len(list))
	for _, pa := range list {
		if pa.CanView() {
			out = append(out, pa)
		}
	}
	return pagination.NewResult(out, total, p), nil
}

// Get: detail project. Yang tidak berhak melihat mendapat 404.
func (u *ProjectUseCase) Get(ctx context.Context, userID, projectID string) (*domain.ProjectAccess, error) {
	_, p, err := u.viewable(ctx, userID, projectID)
	return p, err
}

// Update mengubah sebagian field. Urutan: dapat dilihat (404) -> validasi (400) -> izin (403).
func (u *ProjectUseCase) Update(
	ctx context.Context,
	userID, projectID string,
	in UpdateInput,
) (*domain.ProjectAccess, error) {
	uid, p, err := u.viewable(ctx, userID, projectID)
	if err != nil {
		return nil, err
	}

	if in.Name == nil && in.Description == nil && in.Status == nil {
		return nil, domain.ErrInvalidInput
	}
	updated := p.Project
	if in.Name != nil {
		if updated.Name, err = validateName(*in.Name); err != nil {
			return nil, err
		}
	}
	if in.Description != nil {
		if updated.Description, err = validateDescription(in.Description); err != nil {
			return nil, err
		}
	}
	if in.Status != nil {
		s, ok := domain.ParseStatus(*in.Status)
		if !ok {
			return nil, domain.ErrInvalidInput
		}
		updated.Status = s
	}

	if !p.CanUpdate() {
		u.log.WarnContext(ctx, "perubahan project ditolak: peran tidak cukup",
			"project_id", p.ID, "user_id", uid,
			"workspace_role", p.WorkspaceRole, "project_role", p.ProjectRole)
		return nil, domain.ErrForbidden
	}

	if err := u.repo.Update(ctx, &updated); err != nil {
		return nil, err
	}

	u.log.InfoContext(ctx, "project diperbarui", "project_id", p.ID, "user_id", uid, "status", updated.Status)
	return &domain.ProjectAccess{Project: updated, Access: p.Access}, nil
}

// Delete menghapus project secara permanen (task ikut terhapus lewat ON DELETE CASCADE).
func (u *ProjectUseCase) Delete(ctx context.Context, userID, projectID string) error {
	uid, p, err := u.viewable(ctx, userID, projectID)
	if err != nil {
		return err
	}
	if !p.CanDelete() {
		u.log.WarnContext(ctx, "penghapusan project ditolak: peran tidak cukup",
			"project_id", p.ID, "user_id", uid,
			"workspace_role", p.WorkspaceRole, "project_role", p.ProjectRole)
		return domain.ErrForbidden
	}
	if err := u.repo.Delete(ctx, p.ID); err != nil {
		return err
	}
	u.log.InfoContext(ctx, "project dihapus", "project_id", p.ID, "workspace_id", p.WorkspaceID, "user_id", uid)
	return nil
}

// ---------- helpers ----------

// workspaceRole: peran pemanggil di workspace. Non-anggota/ID rusak -> ErrWorkspaceNotFound.
func (u *ProjectUseCase) workspaceRole(
	ctx context.Context,
	userID, workspaceID string,
) (uuid.UUID, uuid.UUID, domain.WorkspaceRole, error) {
	uid, err := uuid.Parse(userID)
	if err != nil {
		return uuid.Nil, uuid.Nil, "", domain.ErrUnauthenticated
	}
	wid, err := uuid.Parse(workspaceID)
	if err != nil {
		return uuid.Nil, uuid.Nil, "", domain.ErrWorkspaceNotFound
	}
	role, err := u.repo.FindWorkspaceRole(ctx, wid, uid)
	if err != nil {
		if errors.Is(err, domain.ErrNotWorkspaceMember) {
			u.log.WarnContext(ctx, "akses workspace ditolak", "workspace_id", wid, "user_id", uid)
			return uuid.Nil, uuid.Nil, "", domain.ErrWorkspaceNotFound
		}
		return uuid.Nil, uuid.Nil, "", err
	}
	return uid, wid, role, nil
}

func (u *ProjectUseCase) viewable(
	ctx context.Context,
	userID, projectID string,
) (uuid.UUID, *domain.ProjectAccess, error) {
	return loadViewable(ctx, u.repo, u.log, userID, projectID)
}

// loadViewable memuat project dan memastikan pemanggil boleh melihatnya; jika tidak -> 404.
// Dipakai bersama oleh ProjectUseCase dan MemberUseCase.
func loadViewable(
	ctx context.Context,
	finder AccessFinder,
	log *slog.Logger,
	userID, projectID string,
) (uuid.UUID, *domain.ProjectAccess, error) {
	uid, err := uuid.Parse(userID)
	if err != nil {
		return uuid.Nil, nil, domain.ErrUnauthenticated
	}
	pid, err := uuid.Parse(projectID)
	if err != nil {
		return uuid.Nil, nil, domain.ErrProjectNotFound
	}
	p, err := finder.FindWithAccess(ctx, pid, uid)
	if err != nil {
		return uuid.Nil, nil, err // termasuk domain.ErrProjectNotFound
	}
	if !p.CanView() {
		log.WarnContext(ctx, "akses project ditolak", "project_id", pid, "user_id", uid)
		return uuid.Nil, nil, domain.ErrProjectNotFound
	}
	return uid, p, nil
}

func validateName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > maxNameLength {
		return "", domain.ErrInvalidInput
	}
	return name, nil
}

// validateDescription: nil tetap nil; string kosong (setelah trim) menjadi nil.
func validateDescription(description *string) (*string, error) {
	if description == nil {
		return nil, nil
	}
	d := strings.TrimSpace(*description)
	if utf8.RuneCountInString(d) > maxDescriptionLength {
		return nil, domain.ErrInvalidInput
	}
	if d == "" {
		return nil, nil
	}
	return &d, nil
}
