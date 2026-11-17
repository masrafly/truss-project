// Package handler adalah adapter HTTP (gin) untuk modul tasks.
package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"truss-project/internal/middleware"
	"truss-project/internal/modules/tasks/domain"
	"truss-project/internal/modules/tasks/usecase"
)

// taskUseCase adalah interface yang dibutuhkan handler (mudah di-mock).
type taskUseCase interface {
	Create(ctx context.Context, userID, projectID string, in usecase.CreateInput) (*domain.TaskView, error)
	List(ctx context.Context, userID, projectID string, in usecase.ListInput) (*usecase.ListResult, error)
	Get(ctx context.Context, userID, taskID string) (*domain.TaskView, error)
	Update(ctx context.Context, userID, taskID string, in usecase.UpdateInput) (*domain.TaskView, error)
	Delete(ctx context.Context, userID, taskID string) error
	Activities(ctx context.Context, userID, taskID string) ([]domain.Activity, error)
	Summary(ctx context.Context, userID, projectID string) (*domain.ProjectSummary, error)
}

type TaskHandler struct {
	uc  taskUseCase
	log *slog.Logger
}

func NewTaskHandler(uc taskUseCase, log *slog.Logger) *TaskHandler {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &TaskHandler{uc: uc, log: log.With("module", "tasks")}
}

// RegisterRoutes memasang route tasks. Semua route wajib autentikasi (authMW).
func (h *TaskHandler) RegisterRoutes(r gin.IRouter, authMW gin.HandlerFunc) {
	p := r.Group("/projects/:id/tasks", authMW)
	p.POST("", h.Create)
	p.GET("", h.List)
	r.GET("/projects/:id/summary", authMW, h.Summary)

	t := r.Group("/tasks", authMW)
	t.GET("/:id", h.Get)
	t.PATCH("/:id", h.Update)
	t.DELETE("/:id", h.Delete)
	t.GET("/:id/activities", h.Activities)
}

func (h *TaskHandler) Create(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	var req createTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c)
		return
	}
	v, err := h.uc.Create(c.Request.Context(), userID, c.Param("id"), usecase.CreateInput{
		Title:       req.Title,
		Description: req.Description,
		Status:      req.Status,
		Priority:    req.Priority,
		AssigneeID:  req.AssigneeID,
		DueDate:     req.DueDate,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"message": "Task berhasil dibuat", "data": toTaskResponse(v)})
}

// List mendukung query: status, priority (dipisah koma atau diulang), assignee
// (me | none | UUID), q (cari judul), page, limit.
func (h *TaskHandler) List(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	page, okPage := queryInt(c, "page")
	limit, okLimit := queryInt(c, "limit")
	overdue, okOverdue := queryBool(c, "overdue")
	if !okPage || !okLimit || !okOverdue {
		badRequest(c)
		return
	}
	res, err := h.uc.List(c.Request.Context(), userID, c.Param("id"), usecase.ListInput{
		Overdue:    overdue,
		Statuses:   queryList(c, "status"),
		Priorities: queryList(c, "priority"),
		Assignee:   c.Query("assignee"),
		Query:      c.Query("q"),
		Page:       page,
		Limit:      limit,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"data": toTaskResponses(res.Items),
		"meta": gin.H{"page": res.Page, "limit": res.Limit, "total": res.Total},
	})
}

func (h *TaskHandler) Get(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	v, err := h.uc.Get(c.Request.Context(), userID, c.Param("id"))
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": toTaskResponse(v)})
}

func (h *TaskHandler) Update(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	var req updateTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c)
		return
	}
	v, err := h.uc.Update(c.Request.Context(), userID, c.Param("id"), usecase.UpdateInput{
		Title:       req.Title,
		Description: req.Description.toUsecase(),
		Status:      req.Status,
		Priority:    req.Priority,
		AssigneeID:  req.AssigneeID.toUsecase(),
		DueDate:     req.DueDate.toUsecase(),
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Task berhasil diperbarui", "data": toTaskResponse(v)})
}

func (h *TaskHandler) Delete(c *gin.Context) {
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

func (h *TaskHandler) Activities(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	list, err := h.uc.Activities(c.Request.Context(), userID, c.Param("id"))
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": toActivityResponses(list)})
}

// Summary: ringkasan project untuk dashboard (PRD FR-09).
func (h *TaskHandler) Summary(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	s, err := h.uc.Summary(c.Request.Context(), userID, c.Param("id"))
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": toSummaryResponse(s)})
}

// ---------- helpers ----------

// queryBool: kosong -> false; hanya "true"/"false" yang diterima.
func queryBool(c *gin.Context, key string) (bool, bool) {
	switch c.Query(key) {
	case "":
		return false, true
	case "true":
		return true, true
	case "false":
		return false, true
	}
	return false, false
}

func currentUserID(c *gin.Context) (string, bool) {
	id, ok := middleware.UserIDFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"message": "Identitas user tidak ditemukan"})
		return "", false
	}
	return id, true
}

func badRequest(c *gin.Context) {
	c.JSON(http.StatusBadRequest, gin.H{"message": "Input tidak valid"})
}

// queryInt: parameter kosong -> 0 (default use case); bukan angka -> ok=false.
func queryInt(c *gin.Context, key string) (int, bool) {
	s := c.Query(key)
	if s == "" {
		return 0, true
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, false
	}
	return n, true
}

// queryList menerima ?status=a,b maupun ?status=a&status=b; nilai kosong diabaikan.
func queryList(c *gin.Context, key string) []string {
	var out []string
	for _, v := range c.QueryArray(key) {
		for _, part := range strings.Split(v, ",") {
			if p := strings.TrimSpace(part); p != "" {
				out = append(out, p)
			}
		}
	}
	return out
}

// writeError memetakan error domain ke status HTTP. Error tak terduga dicatat di log
// (dengan request_id) dan client hanya menerima pesan generik.
func (h *TaskHandler) writeError(c *gin.Context, err error) { writeError(c, h.log, err) }

func writeError(c *gin.Context, log *slog.Logger, err error) {
	switch {
	case errors.Is(err, domain.ErrInvalidInput),
		errors.Is(err, domain.ErrAssigneeNotMember):
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
	case errors.Is(err, domain.ErrUnauthenticated):
		c.JSON(http.StatusUnauthorized, gin.H{"message": err.Error()})
	case errors.Is(err, domain.ErrForbidden):
		c.JSON(http.StatusForbidden, gin.H{"message": err.Error()})
	case errors.Is(err, domain.ErrProjectNotFound),
		errors.Is(err, domain.ErrTaskNotFound),
		errors.Is(err, domain.ErrCommentNotFound):
		c.JSON(http.StatusNotFound, gin.H{"message": err.Error()})
	case errors.Is(err, domain.ErrProjectArchived):
		c.JSON(http.StatusConflict, gin.H{"message": err.Error()})
	default:
		log.ErrorContext(c.Request.Context(), "error tak terduga",
			"request_id", middleware.RequestIDFromContext(c),
			"error", err,
		)
		c.JSON(http.StatusInternalServerError, gin.H{"message": "Terjadi kesalahan pada server"})
	}
}
