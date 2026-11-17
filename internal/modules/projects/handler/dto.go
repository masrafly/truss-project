package handler

import (
	"time"

	"truss-project/internal/modules/projects/domain"
)

// DTO request/response: kontrak API, terpisah dari entity domain.

// createProjectRequest sengaja tidak memuat workspace_id, created_by, atau status:
// workspace dari path, pembuat dari token, status awal selalu "active".
type createProjectRequest struct {
	Name        string  `json:"name" binding:"required"`
	Description *string `json:"description"`
}

// updateProjectRequest: field yang tidak dikirim (atau null) tidak diubah.
// description "" menghapus deskripsi.
type updateProjectRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
	Status      *string `json:"status"`
}

type projectResponse struct {
	ID            string    `json:"internal_id"`
	WorkspaceID   string    `json:"workspace_id"`
	Name          string    `json:"name"`
	Description   *string   `json:"description"`
	Status        string    `json:"status"`
	CreatedBy     string    `json:"created_by"`
	Role          *string   `json:"role"` // peran di project; null bila bukan anggota project
	WorkspaceRole string    `json:"workspace_role"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func toProjectResponse(p *domain.ProjectAccess) projectResponse {
	var role *string
	if p.ProjectRole != "" {
		r := string(p.ProjectRole)
		role = &r
	}
	return projectResponse{
		ID:            p.ID.String(),
		WorkspaceID:   p.WorkspaceID.String(),
		Name:          p.Name,
		Description:   p.Description,
		Status:        string(p.Status),
		CreatedBy:     p.CreatedBy.String(),
		Role:          role,
		WorkspaceRole: string(p.WorkspaceRole),
		CreatedAt:     p.CreatedAt,
		UpdatedAt:     p.UpdatedAt,
	}
}

func toProjectResponses(list []domain.ProjectAccess) []projectResponse {
	out := make([]projectResponse, 0, len(list)) // selalu array, tidak pernah null
	for i := range list {
		out = append(out, toProjectResponse(&list[i]))
	}
	return out
}
