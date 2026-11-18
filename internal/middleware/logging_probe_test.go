package middleware_test

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"truss-project/internal/middleware"
)

func probeRouter(buf *bytes.Buffer, probeStatus int) *gin.Engine {
	r := gin.New()
	r.Use(middleware.RequestID(), middleware.RequestLogger(slog.New(slog.NewTextHandler(buf, nil))))
	r.GET("/healthz", func(c *gin.Context) { c.Status(probeStatus) })
	r.GET("/readyz", func(c *gin.Context) { c.Status(probeStatus) })
	r.GET("/projects", func(c *gin.Context) { c.Status(http.StatusOK) })
	return r
}

func call(r http.Handler, path string) {
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", path, nil))
}

func TestRequestLogger_SuccessfulProbesAreNotLogged(t *testing.T) {
	buf := &bytes.Buffer{}
	r := probeRouter(buf, http.StatusOK)
	call(r, "/healthz")
	call(r, "/readyz")
	if buf.Len() != 0 {
		t.Errorf("probe sukses tidak boleh dicatat: %s", buf)
	}
}

func TestRequestLogger_FailedProbesAreStillLogged(t *testing.T) {
	buf := &bytes.Buffer{}
	call(probeRouter(buf, http.StatusServiceUnavailable), "/readyz")
	if !strings.Contains(buf.String(), "status=503") || !strings.Contains(buf.String(), "/readyz") {
		t.Errorf("probe gagal harus tetap tercatat: %q", buf)
	}
}

func TestRequestLogger_OtherPathsStillLogged(t *testing.T) {
	buf := &bytes.Buffer{}
	call(probeRouter(buf, http.StatusOK), "/projects")
	if !strings.Contains(buf.String(), "path=/projects") {
		t.Errorf("request biasa harus tercatat: %q", buf)
	}
}
