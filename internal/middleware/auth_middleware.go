package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

const userIDKey = "user_id"

// TokenValidator dipenuhi oleh platform/security.JWTManager.
// Middleware hanya tahu "token -> userID", bukan detail JWT.
type TokenValidator interface {
	Validate(token string) (userID string, err error)
}

func AuthMiddleware(validator TokenValidator) gin.HandlerFunc {
	return func(c *gin.Context) {
		parts := strings.Fields(c.GetHeader("Authorization"))

		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"message": "Token autentikasi diperlukan",
			})
			return
		}

		userID, err := validator.Validate(parts[1])
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"message": "Token tidak valid atau sudah kedaluwarsa",
			})
			return
		}

		c.Set(userIDKey, userID)
		c.Next()
	}
}

// UserIDFromContext mengambil ID user yang sudah diautentikasi middleware.
func UserIDFromContext(c *gin.Context) (string, bool) {
	v, exists := c.Get(userIDKey)
	if !exists {
		return "", false
	}
	id, ok := v.(string)
	return id, ok && id != ""
}
