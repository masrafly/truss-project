package handler

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"truss-project/internal/modules/workspaces/domain"
	"truss-project/internal/platform/pagination"
)

// memberUseCase adalah interface yang dibutuhkan handler (mudah di-mock).
type memberUseCase interface {
	Add(ctx context.Context, actorID, workspaceID, email, role string) (*domain.Member, error)
	List(ctx context.Context, actorID, workspaceID string, page, limit int) (*pagination.Result[domain.Member], error)
	Remove(ctx context.Context, actorID, workspaceID, targetUserID string) error
	ChangeRole(ctx context.Context, actorID, workspaceID, targetUserID, role string) (*domain.Member, error)
	TransferOwnership(ctx context.Context, actorID, workspaceID, targetUserID string) error
}

type MemberHandler struct {
	uc  memberUseCase
	log *slog.Logger
}

func NewMemberHandler(uc memberUseCase, log *slog.Logger) *MemberHandler {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &MemberHandler{uc: uc, log: log.With("module", "workspaces")}
}

// RegisterRoutes memasang route anggota workspace. Semua route wajib autentikasi.
func (h *MemberHandler) RegisterRoutes(r gin.IRouter, authMW gin.HandlerFunc) {
	g := r.Group("/workspaces/:id/members", authMW)
	g.POST("", h.Add)
	g.GET("", h.List)
	g.PATCH("/:userId", h.ChangeRole)
	g.DELETE("/:userId", h.Remove)

	r.POST("/workspaces/:id/transfer-ownership", authMW, h.TransferOwnership)
}

func (h *MemberHandler) ChangeRole(c *gin.Context) {
	actorID, ok := currentUserID(c)
	if !ok {
		return
	}
	var req changeRoleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Input tidak valid"})
		return
	}
	m, err := h.uc.ChangeRole(c.Request.Context(), actorID, c.Param("id"), c.Param("userId"), req.Role)
	if err != nil {
		writeError(c, h.log, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Peran anggota berhasil diubah", "data": toMemberResponse(m)})
}

func (h *MemberHandler) TransferOwnership(c *gin.Context) {
	actorID, ok := currentUserID(c)
	if !ok {
		return
	}
	var req transferOwnershipRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Input tidak valid"})
		return
	}
	if err := h.uc.TransferOwnership(c.Request.Context(), actorID, c.Param("id"), req.UserID); err != nil {
		writeError(c, h.log, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Kepemilikan workspace berhasil dipindahkan"})
}

func (h *MemberHandler) Add(c *gin.Context) {
	actorID, ok := currentUserID(c)
	if !ok {
		return
	}

	var req addMemberRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Input tidak valid"})
		return
	}

	m, err := h.uc.Add(c.Request.Context(), actorID, c.Param("id"), req.Email, req.Role)
	if err != nil {
		writeError(c, h.log, err)
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"message": "Anggota berhasil ditambahkan",
		"data":    toMemberResponse(m),
	})
}

func (h *MemberHandler) List(c *gin.Context) {
	actorID, ok := currentUserID(c)
	if !ok {
		return
	}

	page, limit, okQuery := pagination.FromQuery(c)
	if !okQuery {
		writeError(c, h.log, domain.ErrInvalidInput)
		return
	}

	res, err := h.uc.List(c.Request.Context(), actorID, c.Param("id"), page, limit)
	if err != nil {
		writeError(c, h.log, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": toMemberResponses(res.Items), "meta": res.Meta()})
}

func (h *MemberHandler) Remove(c *gin.Context) {
	actorID, ok := currentUserID(c)
	if !ok {
		return
	}

	if err := h.uc.Remove(c.Request.Context(), actorID, c.Param("id"), c.Param("userId")); err != nil {
		writeError(c, h.log, err)
		return
	}

	c.Status(http.StatusNoContent)
}
