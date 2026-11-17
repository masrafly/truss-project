package handler

import (
	"time"

	"truss-project/internal/modules/workspaces/domain"
)

// DTO request/response: kontrak API, terpisah dari entity domain.

// createWorkspaceRequest sengaja hanya memuat name dan description.
// created_by dan role selalu ditentukan server dari identitas terautentikasi.
type createWorkspaceRequest struct {
	Name        string  `json:"name" binding:"required"`
	Description *string `json:"description"`
}

type workspaceResponse struct {
	ID          string    `json:"internal_id"`
	Name        string    `json:"name"`
	Description *string   `json:"description"`
	CreatedBy   string    `json:"created_by"`
	Role        string    `json:"role"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func toWorkspaceResponse(a *domain.WorkspaceAccess) workspaceResponse {
	return workspaceResponse{
		ID:          a.ID.String(),
		Name:        a.Name,
		Description: a.Description,
		CreatedBy:   a.CreatedBy.String(),
		Role:        string(a.Role),
		CreatedAt:   a.CreatedAt,
		UpdatedAt:   a.UpdatedAt,
	}
}

func toWorkspaceResponses(list []domain.WorkspaceAccess) []workspaceResponse {
	out := make([]workspaceResponse, 0, len(list)) // selalu array, tidak pernah null
	for i := range list {
		out = append(out, toWorkspaceResponse(&list[i]))
	}
	return out
}
