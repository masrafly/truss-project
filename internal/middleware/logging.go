package middleware

import (
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/gin-gonic/gin"
)

// RequestLogger mencatat satu baris log per request (access log).
// Sengaja TIDAK mencatat header, body, maupun query string karena bisa
// berisi password atau token.
func RequestLogger(log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()

		c.Next()

		path := c.FullPath() // template route, mis. /tasks/:id
		if path == "" {
			path = c.Request.URL.Path
		}

		status := c.Writer.Status()
		attrs := []any{
			"request_id", RequestIDFromContext(c),
			"method", c.Request.Method,
			"path", path,
			"status", status,
			"latency_ms", time.Since(start).Milliseconds(),
			"client_ip", c.ClientIP(),
			"bytes", c.Writer.Size(),
		}
		if userID, ok := UserIDFromContext(c); ok {
			attrs = append(attrs, "user_id", userID)
		}

		switch {
		case status >= http.StatusInternalServerError:
			log.Error("request selesai", attrs...)
		case status >= http.StatusBadRequest:
			log.Warn("request selesai", attrs...)
		default:
			log.Info("request selesai", attrs...)
		}
	}
}

// Recovery menangkap panic agar server tidak jatuh, mencatat stack trace
// ke log (bukan ke client), lalu membalas 500 generik.
func Recovery(log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if rec := recover(); rec != nil {
				log.Error("panic ditangkap",
					"request_id", RequestIDFromContext(c),
					"panic", rec,
					"stack", string(debug.Stack()),
				)
				c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
					"message": "Terjadi kesalahan pada server",
				})
			}
		}()
		c.Next()
	}
}
