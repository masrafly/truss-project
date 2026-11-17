// Package handler adalah adapter HTTP (gin) untuk modul projects.
package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"truss-project/internal/middleware"
	"truss-project/internal/modules/projects/domain"
	"truss-project/internal/modules/projects/usecase"
)

// projectUseCase adalah interface yang dibutuhkan handler (mudah di-mock).
type projectUseCase interface {
	Create(ctx context.Context, userID, workspaceID, name string, description *string) (*domain.ProjectAccess, error)
	ListInWorkspace(ctx context.Context, userID, workspaceID string) ([]domain.ProjectAccess, error)
	ListAll(ctx context.Context, userID string) ([]domain.ProjectAccess, error)
	Get(ctx context.Context, userID, projectID string) (*domain.ProjectAccess, error)
	Update(ctx context.Context, userID, projectID string, in usecase.UpdateInput) (*domain.ProjectAccess, error)
	Delete(ctx context.Context, userID, projectID string) error
}

type ProjectHandler struct {
	uc  projectUseCase
	log *slog.Logger
}

func NewProjectHandler(uc projectUseCase, log *slog.Logger) *ProjectHandler {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &ProjectHandler{uc: uc, log: log.With("module", "projects")}
}

// RegisterRoutes memasang route projects. Semua route wajib autentikasi (authMW).
func (h *ProjectHandler) RegisterRoutes(r gin.IRouter, authMW gin.HandlerFunc) {
	ws := r.Group("/workspaces/:id/projects", authMW)
	ws.POST("", h.Create)
	ws.GET("", h.ListInWorkspace)

	p := r.Group("/projects", authMW)
	p.GET("", h.ListAll)
	p.GET("/:id", h.Get)
	p.PATCH("/:id", h.Update)
	p.DELETE("/:id", h.Delete)
}

func (h *ProjectHandler) Create(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	var req createProjectRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Input tidak valid"})
		return
	}

	p, err := h.uc.Create(c.Request.Context(), userID, c.Param("id"), req.Name, req.Description)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"message": "Project berhasil dibuat", "data": toProjectResponse(p)})
}

func (h *ProjectHandler) ListInWorkspace(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	list, err := h.uc.ListInWorkspace(c.Request.Context(), userID, c.Param("id"))
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": toProjectResponses(list)})
}

func (h *ProjectHandler) ListAll(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	list, err := h.uc.ListAll(c.Request.Context(), userID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": toProjectResponses(list)})
}

func (h *ProjectHandler) Get(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	p, err := h.uc.Get(c.Request.Context(), userID, c.Param("id"))
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": toProjectResponse(p)})
}

func (h *ProjectHandler) Update(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	var req updateProjectRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Input tidak valid"})
		return
	}

	p, err := h.uc.Update(c.Request.Context(), userID, c.Param("id"), usecase.UpdateInput{
		Name:        req.Name,
		Description: req.Description,
		Status:      req.Status,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Project berhasil diperbarui", "data": toProjectResponse(p)})
}

func (h *ProjectHandler) Delete(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	if err := h.uc.Delete(c.Request.Context(), userID, c.Param("id")); err != nil {
		h.writeError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// ---------- helpers ----------

func currentUserID(c *gin.Context) (string, bool) {
	id, ok := middleware.UserIDFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"message": "Identitas user tidak ditemukan"})
		return "", false
	}
	return id, true
}

func (h *ProjectHandler) writeError(c *gin.Context, err error) { writeError(c, h.log, err) }

// writeError memetakan error domain ke status HTTP. Error tak terduga dicatat di log
// (dengan request_id) dan client hanya menerima pesan generik.
func writeError(c *gin.Context, log *slog.Logger, err error) {
	switch {
	case errors.Is(err, domain.ErrInvalidInput):
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
	case errors.Is(err, domain.ErrUnauthenticated):
		c.JSON(http.StatusUnauthorized, gin.H{"message": err.Error()})
	case errors.Is(err, domain.ErrForbidden):
		c.JSON(http.StatusForbidden, gin.H{"message": err.Error()})
	case errors.Is(err, domain.ErrProjectNotFound),
		errors.Is(err, domain.ErrWorkspaceNotFound),
		errors.Is(err, domain.ErrMemberNotFound),
		errors.Is(err, domain.ErrWorkspaceUserNotFound):
		c.JSON(http.StatusNotFound, gin.H{"message": err.Error()})
	case errors.Is(err, domain.ErrAlreadyMember),
		errors.Is(err, domain.ErrCannotRemoveOwner):
		c.JSON(http.StatusConflict, gin.H{"message": err.Error()})
	default:
		log.ErrorContext(c.Request.Context(), "error tak terduga",
			"request_id", middleware.RequestIDFromContext(c),
			"error", err,
		)
		c.JSON(http.StatusInternalServerError, gin.H{"message": "Terjadi kesalahan pada server"})
	}
}
