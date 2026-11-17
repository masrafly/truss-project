package middleware

import (
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

// RateLimiter dipenuhi oleh platform/ratelimit.Limiter.
type RateLimiter interface {
	Allow(key string) (ok bool, retryAfter time.Duration)
}

// RateLimitByIP membatasi request per IP client. IP berasal dari c.ClientIP(), yang
// hanya membaca X-Forwarded-For dari proxy tepercaya (lihat SetTrustedProxies di main),
// sehingga tidak bisa dipalsukan client. scope membedakan kuota antar-kelompok route.
func RateLimitByIP(l RateLimiter, scope string, log *slog.Logger) gin.HandlerFunc {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return func(c *gin.Context) {
		ok, retry := l.Allow(scope + "|" + c.ClientIP())
		if !ok {
			log.WarnContext(c.Request.Context(), "rate limit terlampaui",
				"request_id", RequestIDFromContext(c), "scope", scope, "client_ip", c.ClientIP())
			TooManyRequests(c, retry)
			return
		}
		c.Next()
	}
}

// TooManyRequests membalas 429 dengan header Retry-After (detik, dibulatkan ke atas).
func TooManyRequests(c *gin.Context, retryAfter time.Duration) {
	secs := int(math.Ceil(retryAfter.Seconds()))
	if secs < 1 {
		secs = 1
	}
	c.Header("Retry-After", strconv.Itoa(secs))
	c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
		"message": "Terlalu banyak percobaan, coba lagi nanti",
	})
}
