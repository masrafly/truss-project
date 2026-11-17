package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"truss-project/internal/middleware"
	"truss-project/internal/platform/ratelimit"
)

func limitedRouter(l middleware.RateLimiter, trusted []string) *gin.Engine {
	r := gin.New()
	_ = r.SetTrustedProxies(trusted)
	r.POST("/login", middleware.RateLimitByIP(l, "login", nil), func(c *gin.Context) { c.Status(http.StatusOK) })
	return r
}

func hit(r http.Handler, remoteAddr, xff string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", "/login", nil)
	req.RemoteAddr = remoteAddr
	if xff != "" {
		req.Header.Set("X-Forwarded-For", xff)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestRateLimitByIP_BlocksAfterLimitWithRetryAfter(t *testing.T) {
	r := limitedRouter(ratelimit.New(2, time.Minute), nil)
	for i := 0; i < 2; i++ {
		if w := hit(r, "1.1.1.1:1000", ""); w.Code != http.StatusOK {
			t.Fatalf("request %d: status %d", i+1, w.Code)
		}
	}
	w := hit(r, "1.1.1.1:1000", "")
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("status %d, harapannya 429", w.Code)
	}
	if ra := w.Header().Get("Retry-After"); ra != "60" {
		t.Errorf("Retry-After = %q, harapannya 60", ra)
	}
	if w := hit(r, "2.2.2.2:1000", ""); w.Code != http.StatusOK {
		t.Error("IP lain tidak boleh ikut terblokir")
	}
}

// Tanpa proxy tepercaya, X-Forwarded-For palsu tidak bisa dipakai untuk menghindari limit.
func TestRateLimitByIP_SpoofedForwardedForIgnoredWithoutTrustedProxy(t *testing.T) {
	r := limitedRouter(ratelimit.New(1, time.Minute), nil)
	hit(r, "1.1.1.1:1000", "9.9.9.1")
	if w := hit(r, "1.1.1.1:1000", "9.9.9.2"); w.Code != http.StatusTooManyRequests {
		t.Fatalf("mengganti X-Forwarded-For tidak boleh mereset kuota, status %d", w.Code)
	}
}

// Di belakang proxy tepercaya, IP asli client dari X-Forwarded-For yang dipakai.
func TestRateLimitByIP_UsesForwardedForFromTrustedProxy(t *testing.T) {
	r := limitedRouter(ratelimit.New(1, time.Minute), []string{"10.0.0.1"})
	if w := hit(r, "10.0.0.1:1000", "5.5.5.5"); w.Code != http.StatusOK {
		t.Fatal(w.Code)
	}
	if w := hit(r, "10.0.0.1:1000", "6.6.6.6"); w.Code != http.StatusOK {
		t.Error("client berbeda di balik proxy yang sama harus punya kuota sendiri")
	}
	if w := hit(r, "10.0.0.1:1000", "5.5.5.5"); w.Code != http.StatusTooManyRequests {
		t.Error("client yang sama harus terblokir")
	}
}

type fixedLimiter struct{ retry time.Duration }

func (f fixedLimiter) Allow(string) (bool, time.Duration) { return false, f.retry }

func TestTooManyRequests_RetryAfterRoundsUp(t *testing.T) {
	for retry, want := range map[time.Duration]string{
		1500 * time.Millisecond: "2",
		0:                       "1",
		10 * time.Minute:        "600",
	} {
		w := hit(limitedRouter(fixedLimiter{retry}, nil), "1.1.1.1:1", "")
		if got := w.Header().Get("Retry-After"); got != want {
			t.Errorf("retry %v: Retry-After = %q, harapannya %q", retry, got, want)
		}
	}
}
