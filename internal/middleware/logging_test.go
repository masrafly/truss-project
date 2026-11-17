package middleware_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"truss-project/internal/middleware"
)

// ---------- helper ----------

func newBufLogger() (*slog.Logger, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	return slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})), buf
}

// records mengurai setiap baris log JSON menjadi map.
func records(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("baris log bukan JSON: %q (%v)", line, err)
		}
		out = append(out, m)
	}
	return out
}

func serve(r http.Handler, method, target string, hdr map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, nil)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// ---------- RequestID ----------

func requestIDRouter() *gin.Engine {
	r := gin.New()
	r.Use(middleware.RequestID())
	r.GET("/ping", func(c *gin.Context) {
		c.String(http.StatusOK, middleware.RequestIDFromContext(c))
	})
	return r
}

func TestRequestID_GeneratedWhenAbsent(t *testing.T) {
	w := serve(requestIDRouter(), http.MethodGet, "/ping", nil)

	id := w.Header().Get("X-Request-ID")
	if id == "" {
		t.Fatal("header X-Request-ID harus ada di respons")
	}
	if w.Body.String() != id {
		t.Errorf("ID di context (%q) harus sama dengan header (%q)", w.Body.String(), id)
	}
}

func TestRequestID_UniquePerRequest(t *testing.T) {
	r := requestIDRouter()
	a := serve(r, http.MethodGet, "/ping", nil).Header().Get("X-Request-ID")
	b := serve(r, http.MethodGet, "/ping", nil).Header().Get("X-Request-ID")
	if a == b {
		t.Errorf("ID harus unik, dua-duanya %q", a)
	}
}

func TestRequestID_SafeClientIDIsReused(t *testing.T) {
	w := serve(requestIDRouter(), http.MethodGet, "/ping", map[string]string{
		"X-Request-ID": "client-abc_123",
	})
	if got := w.Header().Get("X-Request-ID"); got != "client-abc_123" {
		t.Errorf("ID aman dari client harus dipakai, dapat %q", got)
	}
}

func TestRequestID_UnsafeClientIDIsReplaced(t *testing.T) {
	unsafe := map[string]string{
		"mengandung spasi":      "abc def",
		"newline (log forging)": "abc\nlevel=ERROR forged",
		"carriage return":       "abc\rdef",
		"kutip":                 `abc"def`,
		"tag HTML":              "<script>",
		"lebih dari 64 char":    strings.Repeat("a", 65),
		"unicode":               "idé",
	}

	for name, bad := range unsafe {
		t.Run(name, func(t *testing.T) {
			w := serve(requestIDRouter(), http.MethodGet, "/ping", map[string]string{"X-Request-ID": bad})

			got := w.Header().Get("X-Request-ID")
			if got == bad || got == "" {
				t.Fatalf("ID tidak aman harus diganti, dapat %q", got)
			}
			if len(got) != 36 {
				t.Errorf("pengganti harus UUID (36 karakter), dapat %q", got)
			}
		})
	}
}

func TestRequestID_MaxLengthBoundaryAccepted(t *testing.T) {
	id := strings.Repeat("a", 64)
	w := serve(requestIDRouter(), http.MethodGet, "/ping", map[string]string{"X-Request-ID": id})
	if got := w.Header().Get("X-Request-ID"); got != id {
		t.Errorf("64 karakter harus diterima, dapat %q", got)
	}
}

func TestRequestIDFromContext_Absent(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	if id := middleware.RequestIDFromContext(c); id != "" {
		t.Errorf("harapannya kosong, dapat %q", id)
	}
}

// ---------- RequestLogger ----------

func loggerRouter(log *slog.Logger) *gin.Engine {
	r := gin.New()
	r.Use(middleware.RequestID(), middleware.RequestLogger(log))
	r.GET("/items/:id", func(c *gin.Context) { c.Status(http.StatusOK) })
	r.GET("/bad", func(c *gin.Context) { c.Status(http.StatusBadRequest) })
	r.GET("/boom", func(c *gin.Context) { c.Status(http.StatusInternalServerError) })
	return r
}

func TestRequestLogger_Fields(t *testing.T) {
	log, buf := newBufLogger()

	serve(loggerRouter(log), http.MethodGet, "/items/42", map[string]string{"X-Request-ID": "rid-1"})

	recs := records(t, buf)
	if len(recs) != 1 {
		t.Fatalf("harapannya 1 baris log per request, dapat %d", len(recs))
	}
	rec := recs[0]

	if rec["level"] != "INFO" {
		t.Errorf("level = %v, harapannya INFO", rec["level"])
	}
	if rec["request_id"] != "rid-1" {
		t.Errorf("request_id = %v", rec["request_id"])
	}
	if rec["method"] != "GET" {
		t.Errorf("method = %v", rec["method"])
	}
	if rec["path"] != "/items/:id" {
		t.Errorf("path harus berupa template route, dapat %v", rec["path"])
	}
	if rec["status"] != float64(200) {
		t.Errorf("status = %v", rec["status"])
	}
	for _, k := range []string{"latency_ms", "client_ip", "bytes"} {
		if _, ok := rec[k]; !ok {
			t.Errorf("field %q harus ada", k)
		}
	}
	if _, ok := rec["user_id"]; ok {
		t.Error("user_id tidak boleh ada untuk request tanpa autentikasi")
	}
}

func TestRequestLogger_LevelByStatus(t *testing.T) {
	tests := []struct {
		target string
		level  string
	}{
		{"/items/1", "INFO"},
		{"/bad", "WARN"},
		{"/boom", "ERROR"},
	}

	for _, tc := range tests {
		t.Run(tc.target, func(t *testing.T) {
			log, buf := newBufLogger()
			serve(loggerRouter(log), http.MethodGet, tc.target, nil)

			recs := records(t, buf)
			if len(recs) != 1 || recs[0]["level"] != tc.level {
				t.Fatalf("harapannya level %s, dapat %v", tc.level, recs)
			}
		})
	}
}

func TestRequestLogger_UnknownRouteFallsBackToRawPath(t *testing.T) {
	log, buf := newBufLogger()
	serve(loggerRouter(log), http.MethodGet, "/tidak-ada", nil)

	recs := records(t, buf)
	if len(recs) != 1 {
		t.Fatalf("dapat %d baris", len(recs))
	}
	if recs[0]["path"] != "/tidak-ada" || recs[0]["status"] != float64(404) {
		t.Errorf("rekaman tidak sesuai: %v", recs[0])
	}
}

func TestRequestLogger_NeverLogsSecrets(t *testing.T) {
	log, buf := newBufLogger()

	req := httptest.NewRequest(http.MethodGet, "/items/42?token=QUERY-SECRET&password=P4SSW0RD", nil)
	req.Header.Set("Authorization", "Bearer HEADER-SECRET")
	req.Header.Set("Cookie", "session=COOKIE-SECRET")
	loggerRouter(log).ServeHTTP(httptest.NewRecorder(), req)

	out := buf.String()
	for _, secret := range []string{"QUERY-SECRET", "P4SSW0RD", "HEADER-SECRET", "COOKIE-SECRET", "Bearer"} {
		if strings.Contains(out, secret) {
			t.Errorf("log tidak boleh memuat %q:\n%s", secret, out)
		}
	}
}

func TestRequestLogger_IncludesUserIDWhenAuthenticated(t *testing.T) {
	log, buf := newBufLogger()
	v := &fakeValidator{validToken: "good", userID: "user-777"}

	r := gin.New()
	r.Use(middleware.RequestLogger(log))
	r.GET("/private", middleware.AuthMiddleware(v), func(c *gin.Context) { c.Status(http.StatusOK) })

	serve(r, http.MethodGet, "/private", map[string]string{"Authorization": "Bearer good"})

	recs := records(t, buf)
	if len(recs) != 1 || recs[0]["user_id"] != "user-777" {
		t.Fatalf("user_id harus tercatat, dapat %v", recs)
	}
}

func TestRequestLogger_LogsRejectedAuthAsWarn(t *testing.T) {
	log, buf := newBufLogger()
	v := &fakeValidator{validToken: "good", userID: "u"}

	r := gin.New()
	r.Use(middleware.RequestLogger(log))
	r.GET("/private", middleware.AuthMiddleware(v), func(c *gin.Context) { c.Status(http.StatusOK) })

	serve(r, http.MethodGet, "/private", nil)

	recs := records(t, buf)
	if len(recs) != 1 || recs[0]["level"] != "WARN" || recs[0]["status"] != float64(401) {
		t.Fatalf("request ditolak harus tercatat sebagai WARN 401, dapat %v", recs)
	}
}

// ---------- Recovery ----------

func recoveryRouter(log *slog.Logger) *gin.Engine {
	r := gin.New()
	r.Use(middleware.RequestID(), middleware.Recovery(log))
	r.GET("/panic", func(c *gin.Context) { panic("rahasia-internal: koneksi db://user:pw@host") })
	r.GET("/ok", func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	return r
}

func TestRecovery_PanicBecomes500WithGenericBody(t *testing.T) {
	log, _ := newBufLogger()

	w := serve(recoveryRouter(log), http.MethodGet, "/panic", nil)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, harapannya 500", w.Code)
	}
	body := w.Body.String()
	if strings.Contains(body, "rahasia-internal") || strings.Contains(body, "db://") {
		t.Errorf("detail panic tidak boleh bocor ke client: %s", body)
	}
	if strings.Contains(body, "goroutine") || strings.Contains(body, ".go:") {
		t.Errorf("stack trace tidak boleh bocor ke client: %s", body)
	}
	if !strings.Contains(body, "message") {
		t.Errorf("body harus JSON dengan field message: %s", body)
	}
}

func TestRecovery_LogsPanicWithStackAndRequestID(t *testing.T) {
	log, buf := newBufLogger()

	serve(recoveryRouter(log), http.MethodGet, "/panic", map[string]string{"X-Request-ID": "rid-panic"})

	recs := records(t, buf)
	if len(recs) != 1 {
		t.Fatalf("harapannya 1 log, dapat %d", len(recs))
	}
	rec := recs[0]
	if rec["level"] != "ERROR" {
		t.Errorf("level = %v", rec["level"])
	}
	if rec["request_id"] != "rid-panic" {
		t.Errorf("request_id = %v", rec["request_id"])
	}
	if s, _ := rec["stack"].(string); !strings.Contains(s, "goroutine") {
		t.Errorf("stack trace harus ada di log, dapat %q", rec["stack"])
	}
	if p, _ := rec["panic"].(string); !strings.Contains(p, "rahasia-internal") {
		t.Errorf("detail panic harus ada di log internal, dapat %v", rec["panic"])
	}
}

func TestRecovery_ServerKeepsServingAfterPanic(t *testing.T) {
	log, _ := newBufLogger()
	r := recoveryRouter(log)

	if w := serve(r, http.MethodGet, "/panic", nil); w.Code != http.StatusInternalServerError {
		t.Fatalf("panic harus 500, dapat %d", w.Code)
	}
	w := serve(r, http.MethodGet, "/ok", nil)
	if w.Code != http.StatusOK || w.Body.String() != "ok" {
		t.Errorf("request berikutnya harus normal, dapat %d %q", w.Code, w.Body.String())
	}
}

func TestRecovery_NoLogWhenNoPanic(t *testing.T) {
	log, buf := newBufLogger()

	serve(recoveryRouter(log), http.MethodGet, "/ok", nil)

	if strings.TrimSpace(buf.String()) != "" {
		t.Errorf("tidak boleh ada log tanpa panic, dapat: %s", buf.String())
	}
}

func TestLoggerAndRecovery_PanicIsLoggedAsError500(t *testing.T) {
	// Urutan seperti di main.go: RequestID -> RequestLogger -> Recovery.
	log, buf := newBufLogger()
	r := gin.New()
	r.Use(middleware.RequestID(), middleware.RequestLogger(log), middleware.Recovery(log))
	r.GET("/panic", func(c *gin.Context) { panic("x") })

	w := serve(r, http.MethodGet, "/panic", nil)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", w.Code)
	}
	var access map[string]any
	for _, rec := range records(t, buf) {
		if rec["msg"] == "request selesai" {
			access = rec
		}
	}
	if access == nil {
		t.Fatal("access log harus tetap tercatat setelah panic")
	}
	if access["status"] != float64(500) || access["level"] != "ERROR" {
		t.Errorf("access log harus 500/ERROR, dapat %v", access)
	}
}
