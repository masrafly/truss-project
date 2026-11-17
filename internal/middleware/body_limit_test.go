package middleware_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"truss-project/internal/middleware"
)

// bodyRouter: handler membaca body sepenuhnya dan melaporkan berapa byte yang terbaca.
func bodyRouter(limit int64) (*gin.Engine, *int64) {
	read := new(int64)
	r := gin.New()
	r.Use(middleware.BodyLimit(limit))
	r.POST("/x", func(c *gin.Context) {
		n, err := io.Copy(io.Discard, c.Request.Body)
		*read = n
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": "Input tidak valid"})
			return
		}
		c.Status(http.StatusNoContent)
	})
	return r, read
}

func TestBodyLimit_AllowsBodyWithinLimit(t *testing.T) {
	r, read := bodyRouter(100)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("POST", "/x", strings.NewReader(strings.Repeat("a", 100))))
	if w.Code != http.StatusNoContent || *read != 100 {
		t.Fatalf("status %d, terbaca %d", w.Code, *read)
	}
}

func TestBodyLimit_RejectsLargeContentLengthWithoutReading(t *testing.T) {
	r, read := bodyRouter(100)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("POST", "/x", strings.NewReader(strings.Repeat("a", 101))))
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status %d, harapannya 413", w.Code)
	}
	if *read != 0 {
		t.Errorf("handler tidak boleh dijalankan, terbaca %d", *read)
	}
}

func TestBodyLimit_StopsChunkedBodyAtLimit(t *testing.T) {
	r, read := bodyRouter(100)
	req := httptest.NewRequest("POST", "/x", io.NopCloser(strings.NewReader(strings.Repeat("a", 10_000))))
	req.ContentLength = -1 // tidak diketahui (chunked)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status %d, harapannya 400", w.Code)
	}
	if *read > 100 {
		t.Errorf("pembacaan harus berhenti di batas, terbaca %d", *read)
	}
}

func TestBodyLimit_GetWithoutBody(t *testing.T) {
	r := gin.New()
	r.Use(middleware.BodyLimit(10))
	r.GET("/x", func(c *gin.Context) { c.Status(http.StatusOK) })
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/x", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
}
