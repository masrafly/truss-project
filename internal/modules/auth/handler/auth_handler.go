// Package handler adalah adapter HTTP (gin). Tugasnya: baca request -> panggil
// use case -> ubah hasil/error menjadi respons HTTP.
package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"truss-project/internal/middleware"
	"truss-project/internal/modules/auth/domain"
	"truss-project/internal/modules/auth/usecase"
)

// authUseCase adalah interface yang dibutuhkan handler (mudah di-mock).
type authUseCase interface {
	Register(ctx context.Context, name, email, password string) (*domain.User, error)
	Login(ctx context.Context, email, password string) (*usecase.LoginResult, error)
	GetProfile(ctx context.Context, userID string) (*domain.User, error)
}

type AuthHandler struct {
	auth authUseCase
	log  *slog.Logger
}

func NewAuthHandler(auth authUseCase, log *slog.Logger) *AuthHandler {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &AuthHandler{auth: auth, log: log.With("module", "auth")}
}

// RegisterRoutes memasang route modul auth. authMW melindungi route privat.
// publicMW (opsional, mis. rate limit per IP) dipasang pada register dan login.
func (h *AuthHandler) RegisterRoutes(r gin.IRouter, authMW gin.HandlerFunc, publicMW ...gin.HandlerFunc) {
	g := r.Group("/auth")
	g.POST("/register", chain(publicMW, h.Register)...)
	g.POST("/login", chain(publicMW, h.Login)...)
	g.GET("/me", authMW, h.Me)
}

// chain menyalin middleware ke slice baru sebelum menambahkan handler, agar tidak
// pernah menulis ke array milik pemanggil. (Gin sendiri menyalin daftar handler saat
// route didaftarkan, jadi ini pengaman tambahan, bukan perbaikan bug yang terjadi.)
func chain(mw []gin.HandlerFunc, h gin.HandlerFunc) []gin.HandlerFunc {
	out := make([]gin.HandlerFunc, 0, len(mw)+1)
	out = append(out, mw...)
	return append(out, h)
}

func (h *AuthHandler) Register(c *gin.Context) {
	var req registerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Input tidak valid"})
		return
	}

	user, err := h.auth.Register(c.Request.Context(), req.Name, req.Email, req.Password)
	if err != nil {
		h.writeError(c, err)
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"message": "Akun berhasil dibuat",
		"data":    toUserResponse(user),
	})
}

func (h *AuthHandler) Login(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Input tidak valid"})
		return
	}

	result, err := h.auth.Login(c.Request.Context(), req.Email, req.Password)
	if err != nil {
		h.writeError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":      "Login berhasil",
		"access_token": result.AccessToken,
		"token_type":   "Bearer",
		"expires_in":   int(result.ExpiresIn.Seconds()),
		"data":         toUserResponse(result.User),
	})
}

func (h *AuthHandler) Me(c *gin.Context) {
	userID, ok := middleware.UserIDFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"message": "Identitas user tidak ditemukan"})
		return
	}

	user, err := h.auth.GetProfile(c.Request.Context(), userID)
	if err != nil {
		h.writeError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Token valid",
		"data":    toUserResponse(user),
	})
}

// writeError memetakan error domain ke status HTTP. Error tak terduga dicatat
// di log (dengan request_id) dan client hanya menerima pesan generik.
func (h *AuthHandler) writeError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, domain.ErrInvalidInput):
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
	case errors.Is(err, domain.ErrEmailAlreadyUsed):
		c.JSON(http.StatusConflict, gin.H{"message": err.Error()})
	case errors.Is(err, domain.ErrInvalidCredentials):
		c.JSON(http.StatusUnauthorized, gin.H{"message": err.Error()})
	case errors.Is(err, domain.ErrUserNotFound):
		c.JSON(http.StatusNotFound, gin.H{"message": err.Error()})
	case errors.Is(err, domain.ErrTooManyAttempts):
		var retry time.Duration
		var tm *domain.TooManyAttemptsError
		if errors.As(err, &tm) {
			retry = tm.RetryAfter
		}
		middleware.TooManyRequests(c, retry)
	default:
		h.log.ErrorContext(c.Request.Context(), "error tak terduga",
			"request_id", middleware.RequestIDFromContext(c),
			"error", err,
		)
		c.JSON(http.StatusInternalServerError, gin.H{"message": "Terjadi kesalahan pada server"})
	}
}
