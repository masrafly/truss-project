package middleware_test

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"truss-project/internal/middleware"
)

// secured: router dengan urutan yang sama seperti main.go (header keamanan, lalu CORS, lalu
// route yang dilindungi autentikasi).
func secured(origins ...string) *gin.Engine {
	r := gin.New()
	r.Use(middleware.SecurityHeaders(), middleware.CORS(origins))
	r.GET("/data", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) })
	r.GET("/panic", func(*gin.Context) { panic("boom") })
	return r
}

func doReq(r http.Handler, method, path string, hdr map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// ---------- SecurityHeaders ----------

func TestSecurityHeaders_OnEveryResponseIncludingNotFound(t *testing.T) {
	r := secured()
	for _, path := range []string{"/data", "/tidak-ada"} {
		w := doReq(r, "GET", path, nil)
		for k, want := range map[string]string{
			"X-Content-Type-Options":  "nosniff",
			"X-Frame-Options":         "DENY",
			"Referrer-Policy":         "no-referrer",
			"Cache-Control":           "no-store",
			"Content-Security-Policy": "default-src 'none'; frame-ancestors 'none'",
		} {
			if got := w.Header().Get(k); got != want {
				t.Errorf("%s %s = %q, harapannya %q", path, k, got, want)
			}
		}
	}
}

func TestSecurityHeaders_PresentOnPanicRecoveredResponse(t *testing.T) {
	r := gin.New()
	r.Use(middleware.SecurityHeaders(), gin.Recovery())
	r.GET("/panic", func(*gin.Context) { panic("boom") })
	w := doReq(r, "GET", "/panic", nil)
	if w.Code != http.StatusInternalServerError || w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("status %d, header = %q", w.Code, w.Header().Get("X-Content-Type-Options"))
	}
}

func TestSecurityHeaders_HSTSOnlyOverHTTPS(t *testing.T) {
	r := secured()
	if got := doReq(r, "GET", "/data", nil).Header().Get("Strict-Transport-Security"); got != "" {
		t.Errorf("HTTP biasa tidak boleh mendapat HSTS, dapat %q", got)
	}
	if got := doReq(r, "GET", "/data", map[string]string{"X-Forwarded-Proto": "http"}).Header().Get("Strict-Transport-Security"); got != "" {
		t.Errorf("X-Forwarded-Proto http tidak boleh mendapat HSTS, dapat %q", got)
	}
	for _, v := range []string{"https", "HTTPS"} {
		got := doReq(r, "GET", "/data", map[string]string{"X-Forwarded-Proto": v}).Header().Get("Strict-Transport-Security")
		if got != "max-age=31536000; includeSubDomains" {
			t.Errorf("X-Forwarded-Proto %s: HSTS = %q", v, got)
		}
	}
	req := httptest.NewRequest("GET", "/data", nil)
	req.TLS = &tls.ConnectionState{}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Header().Get("Strict-Transport-Security") == "" {
		t.Error("koneksi TLS langsung harus mendapat HSTS")
	}
}

// ---------- CORS ----------

const allowedOrigin = "https://app.example.com"

func TestCORS_DisabledWhenNoOriginsConfigured(t *testing.T) {
	r := secured()
	w := doReq(r, "GET", "/data", map[string]string{"Origin": allowedOrigin})
	if w.Code != http.StatusOK || w.Header().Get("Access-Control-Allow-Origin") != "" || w.Header().Get("Vary") != "" {
		t.Fatalf("tanpa konfigurasi tidak boleh ada header CORS: status %d headers %v", w.Code, w.Header())
	}
	w = doReq(r, "OPTIONS", "/data", map[string]string{"Origin": allowedOrigin, "Access-Control-Request-Method": "GET"})
	if w.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Error("preflight tanpa konfigurasi tidak boleh diizinkan")
	}
}

func TestCORS_AllowedOriginGetsHeadersOnNormalRequest(t *testing.T) {
	w := doReq(secured(allowedOrigin), "GET", "/data", map[string]string{"Origin": allowedOrigin})
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	h := w.Header()
	if h.Get("Access-Control-Allow-Origin") != allowedOrigin {
		t.Errorf("Allow-Origin = %q (harus origin persis, bukan *)", h.Get("Access-Control-Allow-Origin"))
	}
	if h.Get("Access-Control-Expose-Headers") != "X-Request-ID, Retry-After" {
		t.Errorf("Expose-Headers = %q", h.Get("Access-Control-Expose-Headers"))
	}
	if h.Get("Access-Control-Allow-Credentials") != "" {
		t.Error("tidak boleh memakai Allow-Credentials (autentikasi lewat header, bukan cookie)")
	}
	if h.Get("Vary") != "Origin" {
		t.Errorf("Vary = %q", h.Get("Vary"))
	}
}

func TestCORS_PreflightFromAllowedOriginIs204WithoutReachingHandler(t *testing.T) {
	reached := false
	r := gin.New()
	r.Use(middleware.CORS([]string{allowedOrigin}))
	r.POST("/data", func(c *gin.Context) { reached = true })
	w := doReq(r, "OPTIONS", "/data", map[string]string{
		"Origin": allowedOrigin, "Access-Control-Request-Method": "POST",
		"Access-Control-Request-Headers": "authorization,content-type",
	})
	if w.Code != http.StatusNoContent || reached || w.Body.Len() != 0 {
		t.Fatalf("status %d reached=%v body=%q", w.Code, reached, w.Body)
	}
	h := w.Header()
	if h.Get("Access-Control-Allow-Origin") != allowedOrigin ||
		h.Get("Access-Control-Allow-Methods") != "GET, POST, PATCH, DELETE, OPTIONS" ||
		h.Get("Access-Control-Allow-Headers") != "Authorization, Content-Type, X-Request-ID" ||
		h.Get("Access-Control-Max-Age") != "600" {
		t.Errorf("header preflight = %v", h)
	}
	if v := h.Values("Vary"); len(v) != 3 {
		t.Errorf("Vary harus memuat Origin dan header Access-Control-Request-*, dapat %v", v)
	}
}

func TestCORS_PreflightIsAnsweredEvenForRouteRequiringAuth(t *testing.T) {
	// Preflight tidak membawa Authorization; CORS harus menjawabnya sebelum autentikasi.
	r := gin.New()
	r.Use(middleware.CORS([]string{allowedOrigin}))
	r.POST("/secret", func(c *gin.Context) { c.AbortWithStatus(http.StatusUnauthorized) })
	w := doReq(r, "OPTIONS", "/secret", map[string]string{"Origin": allowedOrigin, "Access-Control-Request-Method": "POST"})
	if w.Code != http.StatusNoContent {
		t.Fatalf("status %d, harapannya 204", w.Code)
	}
}

func TestCORS_UnlistedOrigin(t *testing.T) {
	r := secured(allowedOrigin)
	for _, origin := range []string{
		"https://evil.example.com",
		"http://app.example.com",       // skema berbeda
		"https://app.example.com:8443", // port berbeda
		"https://app.example.com.evil.com",
		"https://sub.app.example.com",
		"null",
	} {
		w := doReq(r, "GET", "/data", map[string]string{"Origin": origin})
		if w.Code != http.StatusOK || w.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Errorf("origin %q: status %d, Allow-Origin %q (tidak boleh ada header CORS)", origin, w.Code, w.Header().Get("Access-Control-Allow-Origin"))
		}
		w = doReq(r, "OPTIONS", "/data", map[string]string{"Origin": origin, "Access-Control-Request-Method": "GET"})
		if w.Code != http.StatusForbidden || w.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Errorf("preflight origin %q: status %d, harapannya 403 tanpa header CORS", origin, w.Code)
		}
	}
}

func TestCORS_RequestWithoutOriginIsUntouched(t *testing.T) {
	w := doReq(secured(allowedOrigin), "GET", "/data", nil)
	if w.Code != http.StatusOK || w.Header().Get("Access-Control-Allow-Origin") != "" || w.Header().Get("Vary") != "" {
		t.Fatalf("status %d headers %v", w.Code, w.Header())
	}
}

func TestCORS_OptionsWithoutRequestMethodIsNotPreflight(t *testing.T) {
	w := doReq(secured(allowedOrigin), "OPTIONS", "/data", map[string]string{"Origin": allowedOrigin})
	if w.Code == http.StatusNoContent || w.Header().Get("Access-Control-Allow-Methods") != "" {
		t.Errorf("OPTIONS tanpa Access-Control-Request-Method bukan preflight: status %d", w.Code)
	}
}

func TestCORS_MultipleAllowedOriginsEachEchoedExactly(t *testing.T) {
	r := secured("https://a.example.com", "http://localhost:3000")
	for _, o := range []string{"https://a.example.com", "http://localhost:3000"} {
		if got := doReq(r, "GET", "/data", map[string]string{"Origin": o}).Header().Get("Access-Control-Allow-Origin"); got != o {
			t.Errorf("origin %q: Allow-Origin = %q", o, got)
		}
	}
}
