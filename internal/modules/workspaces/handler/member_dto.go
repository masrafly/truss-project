package handler

import (
	"time"

	"truss-project/internal/modules/workspaces/domain"
)

// addMemberRequest: role opsional (default "member"). Nilai yang boleh: "admin", "member".
type addMemberRequest struct {
	Email string `json:"email" binding:"required"`
	Role  string `json:"role"`
}

// changeRoleRequest: role baru, "admin" atau "member".
type changeRoleRequest struct {
	Role string `json:"role" binding:"required"`
}

// transferOwnershipRequest: ID user anggota workspace yang menjadi Owner baru.
type transferOwnershipRequest struct {
	UserID string `json:"user_id" binding:"required"`
}

type memberResponse struct {
	UserID   string    `json:"user_id"`
	Name     string    `json:"name"`
	Email    string    `json:"email"`
	Role     string    `json:"role"`
	JoinedAt time.Time `json:"joined_at"`
}

func toMemberResponse(m *domain.Member) memberResponse {
	return memberResponse{
		UserID:   m.UserID.String(),
		Name:     m.Name,
		Email:    m.Email,
		Role:     string(m.Role),
		JoinedAt: m.JoinedAt,
	}
}

func toMemberResponses(list []domain.Member) []memberResponse {
	out := make([]memberResponse, 0, len(list)) // selalu array, tidak pernah null
	for i := range list {
		out = append(out, toMemberResponse(&list[i]))
	}
	return out
}
