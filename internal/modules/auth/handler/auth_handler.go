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
	Refresh(ctx context.Context, refreshToken string) (*usecase.TokenPair, error)
	Logout(ctx context.Context, refreshToken string) error
	LogoutAll(ctx context.Context, userID string) error
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
// publicMW (opsional, mis. rate limit per IP) dipasang pada register, login, refresh, dan logout.
func (h *AuthHandler) RegisterRoutes(r gin.IRouter, authMW gin.HandlerFunc, publicMW ...gin.HandlerFunc) {
	g := r.Group("/auth")
	g.POST("/register", chain(publicMW, h.Register)...)
	g.POST("/login", chain(publicMW, h.Login)...)
	g.POST("/refresh", chain(publicMW, h.Refresh)...)
	g.POST("/logout", chain(publicMW, h.Logout)...)
	g.POST("/logout-all", authMW, h.LogoutAll)
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

	body := gin.H{
		"message":      "Login berhasil",
		"access_token": result.AccessToken,
		"token_type":   "Bearer",
		"expires_in":   int(result.ExpiresIn.Seconds()),
		"data":         toUserResponse(result.User),
	}
	if result.RefreshToken != "" {
		body["refresh_token"] = result.RefreshToken
		body["refresh_expires_in"] = int(result.RefreshExpiresIn.Seconds())
	}
	c.JSON(http.StatusOK, body)
}

// Refresh menukar refresh token dengan pasangan token baru (token lama langsung hangus).
func (h *AuthHandler) Refresh(c *gin.Context) {
	var req refreshRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Input tidak valid"})
		return
	}

	pair, err := h.auth.Refresh(c.Request.Context(), req.RefreshToken)
	if err != nil {
		h.writeError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"access_token":       pair.AccessToken,
		"token_type":         "Bearer",
		"expires_in":         int(pair.ExpiresIn.Seconds()),
		"refresh_token":      pair.RefreshToken,
		"refresh_expires_in": int(pair.RefreshExpiresIn.Seconds()),
	})
}

// Logout mencabut sesi milik refresh token. Selalu 204 untuk token berformat benar,
// termasuk yang tidak dikenal, sehingga endpoint ini tidak bisa dipakai menguji token.
func (h *AuthHandler) Logout(c *gin.Context) {
	var req refreshRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Input tidak valid"})
		return
	}
	if err := h.auth.Logout(c.Request.Context(), req.RefreshToken); err != nil {
		h.writeError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// LogoutAll mencabut semua sesi user yang sedang login.
func (h *AuthHandler) LogoutAll(c *gin.Context) {
	userID, ok := middleware.UserIDFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"message": "Identitas user tidak ditemukan"})
		return
	}
	if err := h.auth.LogoutAll(c.Request.Context(), userID); err != nil {
		h.writeError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
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
	case errors.Is(err, domain.ErrInvalidRefreshToken):
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
