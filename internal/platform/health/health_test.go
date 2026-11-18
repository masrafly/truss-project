package health_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"truss-project/internal/platform/health"
)

func init() { gin.SetMode(gin.TestMode) }

type fakePinger struct {
	err   error
	calls int
	// block, bila true, menunggu sampai context dibatalkan (mensimulasikan database menggantung).
	block       bool
	hadDeadline bool
}

func (f *fakePinger) PingContext(ctx context.Context) error {
	f.calls++
	_, f.hadDeadline = ctx.Deadline()
	if f.block {
		<-ctx.Done()
		return ctx.Err()
	}
	return f.err
}

func get(r http.Handler, path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
	return w
}

func router(p health.Pinger, log *slog.Logger) *gin.Engine {
	r := gin.New()
	health.Register(r, p, log)
	return r
}

func TestLiveness_DoesNotTouchDatabase(t *testing.T) {
	p := &fakePinger{err: errors.New("db mati")}
	w := get(router(p, nil), "/healthz")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"status":"ok"`) {
		t.Fatalf("status %d body %s", w.Code, w.Body)
	}
	if p.calls != 0 {
		t.Error("liveness tidak boleh menyentuh database (gangguan DB tidak boleh memicu restart container)")
	}
}

func TestReadiness_OKWhenDatabaseReachable(t *testing.T) {
	p := &fakePinger{}
	w := get(router(p, nil), "/readyz")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"status":"ready"`) {
		t.Fatalf("status %d body %s", w.Code, w.Body)
	}
	if p.calls != 1 || !p.hadDeadline {
		t.Errorf("harus ping sekali dengan batas waktu: calls=%d deadline=%v", p.calls, p.hadDeadline)
	}
}

func TestReadiness_503WithoutLeakingErrorDetail(t *testing.T) {
	buf := &bytes.Buffer{}
	p := &fakePinger{err: errors.New("dial tcp 10.1.2.3:5432: password authentication failed for user rahasia")}

	w := get(router(p, slog.New(slog.NewTextHandler(buf, nil))), "/readyz")

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status %d, harapannya 503", w.Code)
	}
	for _, leak := range []string{"rahasia", "10.1.2.3", "password", "dial"} {
		if strings.Contains(w.Body.String(), leak) {
			t.Errorf("respons tidak boleh memuat %q: %s", leak, w.Body)
		}
	}
	if !strings.Contains(buf.String(), "rahasia") {
		t.Error("detail error harus ada di log")
	}
}

func TestReadiness_TimesOutInsteadOfHanging(t *testing.T) {
	p := &fakePinger{block: true}
	start := time.Now()
	w := get(router(p, nil), "/readyz")
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status %d, harapannya 503", w.Code)
	}
	if elapsed := time.Since(start); elapsed > 4*time.Second {
		t.Errorf("probe menggantung %v; harus dibatasi timeout", elapsed)
	}
}

func TestRoutesRegistered(t *testing.T) {
	got := map[string]bool{}
	for _, ri := range router(&fakePinger{}, nil).Routes() {
		got[ri.Method+" "+ri.Path] = true
	}
	if !got["GET /healthz"] || !got["GET /readyz"] || len(got) != 2 {
		t.Errorf("routes = %v", got)
	}
}
