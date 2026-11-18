// Package health menyediakan endpoint pemeriksaan kesehatan untuk container dan
// orkestrator (Docker HEALTHCHECK, Kubernetes probe, load balancer).
//
//   - GET /healthz : liveness. Proses hidup dan mampu melayani HTTP. TIDAK menyentuh database,
//     agar gangguan database tidak membuat container dianggap mati lalu di-restart terus.
//   - GET /readyz  : readiness. Database dapat dijangkau. Gagal = 503, dan trafik sebaiknya
//     dialihkan sementara (container tidak perlu di-restart).
//
// Kedua endpoint publik dan sengaja tidak membocorkan detail: respons gagal tidak memuat
// pesan error database (host, user, dan sebagainya); detailnya hanya ada di log.
package health

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// Pinger dipenuhi oleh *sql.DB.
type Pinger interface {
	PingContext(ctx context.Context) error
}

const (
	LivenessPath  = "/healthz"
	ReadinessPath = "/readyz"

	// pingTimeout membatasi waktu pengecekan database agar probe tidak ikut menggantung.
	pingTimeout = 2 * time.Second
)

// Register memasang kedua endpoint pada router.
func Register(r gin.IRouter, db Pinger, log *slog.Logger) {
	r.GET(LivenessPath, Liveness())
	r.GET(ReadinessPath, Readiness(db, log))
}

func Liveness() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	}
}

func Readiness(db Pinger, log *slog.Logger) gin.HandlerFunc {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), pingTimeout)
		defer cancel()

		if err := db.PingContext(ctx); err != nil {
			log.WarnContext(c.Request.Context(), "readiness gagal: database tidak terjangkau", "error", err)
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "unavailable"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ready"})
	}
}
