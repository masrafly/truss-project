// Package handler adalah adapter HTTP (gin) untuk modul workspaces.
package handler

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"truss-project/internal/modules/workspaces/domain"
)

// workspaceUseCase adalah interface yang dibutuhkan handler (mudah di-mock).
type workspaceUseCase interface {
	Create(ctx context.Context, userID, name string, description *string) (*domain.WorkspaceAccess, error)
	List(ctx context.Context, userID string) ([]domain.WorkspaceAccess, error)
	Get(ctx context.Context, userID, workspaceID string) (*domain.WorkspaceAccess, error)
}

type WorkspaceHandler struct {
	uc  workspaceUseCase
	log *slog.Logger
}

func NewWorkspaceHandler(uc workspaceUseCase, log *slog.Logger) *WorkspaceHandler {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &WorkspaceHandler{uc: uc, log: log.With("module", "workspaces")}
}

// RegisterRoutes memasang route workspaces. Semua route wajib autentikasi (authMW).
func (h *WorkspaceHandler) RegisterRoutes(r gin.IRouter, authMW gin.HandlerFunc) {
	g := r.Group("/workspaces", authMW)
	g.POST("", h.Create)
	g.GET("", h.List)
	g.GET("/:id", h.Get)
}

func (h *WorkspaceHandler) Create(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}

	var req createWorkspaceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Input tidak valid"})
		return
	}

	ws, err := h.uc.Create(c.Request.Context(), userID, req.Name, req.Description)
	if err != nil {
		writeError(c, h.log, err)
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"message": "Workspace berhasil dibuat",
		"data":    toWorkspaceResponse(ws),
	})
}

func (h *WorkspaceHandler) List(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}

	list, err := h.uc.List(c.Request.Context(), userID)
	if err != nil {
		writeError(c, h.log, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": toWorkspaceResponses(list)})
}

func (h *WorkspaceHandler) Get(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}

	ws, err := h.uc.Get(c.Request.Context(), userID, c.Param("id"))
	if err != nil {
		writeError(c, h.log, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": toWorkspaceResponse(ws)})
}
