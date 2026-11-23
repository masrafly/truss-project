package handler

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"truss-project/internal/modules/projects/domain"
	"truss-project/internal/platform/pagination"
)

// memberUseCase adalah interface yang dibutuhkan handler (mudah di-mock).
type memberUseCase interface {
	Add(ctx context.Context, actorID, projectID, email, role string) (*domain.Member, error)
	List(ctx context.Context, actorID, projectID string, page, limit int) (*pagination.Result[domain.Member], error)
	Remove(ctx context.Context, actorID, projectID, targetUserID string) error
}

type MemberHandler struct {
	uc  memberUseCase
	log *slog.Logger
}

func NewMemberHandler(uc memberUseCase, log *slog.Logger) *MemberHandler {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &MemberHandler{uc: uc, log: log.With("module", "projects")}
}

// RegisterRoutes memasang route anggota project. Semua route wajib autentikasi (authMW).
func (h *MemberHandler) RegisterRoutes(r gin.IRouter, authMW gin.HandlerFunc) {
	g := r.Group("/projects/:id/members", authMW)
	g.POST("", h.Add)
	g.GET("", h.List)
	g.DELETE("/:userId", h.Remove)
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
	c.JSON(http.StatusCreated, gin.H{"message": "Anggota project berhasil ditambahkan", "data": toMemberResponse(m)})
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
