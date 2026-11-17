package handler

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"truss-project/internal/modules/tasks/domain"
	"truss-project/internal/modules/tasks/usecase"
)

// commentUseCase adalah interface yang dibutuhkan handler (mudah di-mock).
type commentUseCase interface {
	Create(ctx context.Context, userID, taskID, content string) (*domain.Comment, error)
	List(ctx context.Context, userID, taskID string, page, limit int) (*usecase.CommentListResult, error)
	Update(ctx context.Context, userID, taskID, commentID, content string) (*domain.Comment, error)
	Delete(ctx context.Context, userID, taskID, commentID string) error
}

type CommentHandler struct {
	uc  commentUseCase
	log *slog.Logger
}

func NewCommentHandler(uc commentUseCase, log *slog.Logger) *CommentHandler {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &CommentHandler{uc: uc, log: log.With("module", "tasks")}
}

// RegisterRoutes memasang route komentar. Semua route wajib autentikasi (authMW).
func (h *CommentHandler) RegisterRoutes(r gin.IRouter, authMW gin.HandlerFunc) {
	g := r.Group("/tasks/:id/comments", authMW)
	g.POST("", h.Create)
	g.GET("", h.List)
	g.PATCH("/:commentId", h.Update)
	g.DELETE("/:commentId", h.Delete)
}

// commentRequest sengaja hanya memuat content: penulis selalu dari token.
type commentRequest struct {
	Content string `json:"content" binding:"required"`
}

type commentResponse struct {
	ID        string           `json:"internal_id"`
	TaskID    string           `json:"task_id"`
	Author    *userRefResponse `json:"author"`
	Content   string           `json:"content"`
	Edited    bool             `json:"edited"`
	CreatedAt time.Time        `json:"created_at"`
	UpdatedAt time.Time        `json:"updated_at"`
}

func toCommentResponse(c *domain.Comment) commentResponse {
	return commentResponse{
		ID:        c.ID.String(),
		TaskID:    c.TaskID.String(),
		Author:    toUserRef(c.Author),
		Content:   c.Content,
		Edited:    c.UpdatedAt.After(c.CreatedAt),
		CreatedAt: c.CreatedAt,
		UpdatedAt: c.UpdatedAt,
	}
}

func (h *CommentHandler) Create(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	var req commentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c)
		return
	}
	cm, err := h.uc.Create(c.Request.Context(), userID, c.Param("id"), req.Content)
	if err != nil {
		writeError(c, h.log, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"message": "Komentar berhasil dibuat", "data": toCommentResponse(cm)})
}

func (h *CommentHandler) List(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	page, okPage := queryInt(c, "page")
	limit, okLimit := queryInt(c, "limit")
	if !okPage || !okLimit {
		badRequest(c)
		return
	}
	res, err := h.uc.List(c.Request.Context(), userID, c.Param("id"), page, limit)
	if err != nil {
		writeError(c, h.log, err)
		return
	}
	out := make([]commentResponse, 0, len(res.Items))
	for i := range res.Items {
		out = append(out, toCommentResponse(&res.Items[i]))
	}
	c.JSON(http.StatusOK, gin.H{
		"data": out,
		"meta": gin.H{"page": res.Page, "limit": res.Limit, "total": res.Total},
	})
}

func (h *CommentHandler) Update(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	var req commentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c)
		return
	}
	cm, err := h.uc.Update(c.Request.Context(), userID, c.Param("id"), c.Param("commentId"), req.Content)
	if err != nil {
		writeError(c, h.log, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Komentar berhasil diperbarui", "data": toCommentResponse(cm)})
}

func (h *CommentHandler) Delete(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	if err := h.uc.Delete(c.Request.Context(), userID, c.Param("id"), c.Param("commentId")); err != nil {
		writeError(c, h.log, err)
		return
	}
	c.Status(http.StatusNoContent)
}
