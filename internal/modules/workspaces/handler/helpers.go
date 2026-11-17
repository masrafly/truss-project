package handler

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"truss-project/internal/middleware"
	"truss-project/internal/modules/workspaces/domain"
)

// currentUserID mengambil ID user dari middleware auth. Jika tidak ada (mis. route
// lupa dipasangi middleware), request ditolak dengan 401.
func currentUserID(c *gin.Context) (string, bool) {
	id, ok := middleware.UserIDFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"message": "Identitas user tidak ditemukan"})
		return "", false
	}
	return id, true
}

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
	case errors.Is(err, domain.ErrWorkspaceNotFound),
		errors.Is(err, domain.ErrUserNotFound),
		errors.Is(err, domain.ErrMemberNotFound):
		c.JSON(http.StatusNotFound, gin.H{"message": err.Error()})
	case errors.Is(err, domain.ErrAlreadyMember),
		errors.Is(err, domain.ErrCannotRemoveOwner),
		errors.Is(err, domain.ErrMemberOwnsProjects):
		c.JSON(http.StatusConflict, gin.H{"message": err.Error()})
	default:
		log.ErrorContext(c.Request.Context(), "error tak terduga",
			"request_id", middleware.RequestIDFromContext(c),
			"error", err,
		)
		c.JSON(http.StatusInternalServerError, gin.H{"message": "Terjadi kesalahan pada server"})
	}
}
