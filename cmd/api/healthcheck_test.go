package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func serverWith(status int, path *string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if path != nil {
			*path = r.URL.Path
		}
		w.WriteHeader(status)
	}))
}

func TestRunHealthcheck_OKReturnsZeroAndCallsHealthz(t *testing.T) {
	var path string
	srv := serverWith(http.StatusOK, &path)
	defer srv.Close()

	if code := runHealthcheck(srv.Client(), srv.URL); code != 0 {
		t.Errorf("exit code %d, harapannya 0", code)
	}
	if path != "/healthz" {
		t.Errorf("path = %q, harapannya /healthz (liveness, bukan readiness)", path)
	}
}

func TestRunHealthcheck_NonOKReturnsOne(t *testing.T) {
	for _, status := range []int{http.StatusServiceUnavailable, http.StatusInternalServerError, http.StatusNotFound, http.StatusNoContent} {
		srv := serverWith(status, nil)
		if code := runHealthcheck(srv.Client(), srv.URL); code != 1 {
			t.Errorf("status %d: exit code %d, harapannya 1", status, code)
		}
		srv.Close()
	}
}

func TestRunHealthcheck_UnreachableReturnsOne(t *testing.T) {
	srv := serverWith(http.StatusOK, nil)
	url := srv.URL
	srv.Close() // port sudah tidak mendengarkan

	if code := runHealthcheck(&http.Client{Timeout: time.Second}, url); code != 1 {
		t.Errorf("server mati: exit code %d, harapannya 1", code)
	}
}

func TestRunHealthcheck_HangingServerTimesOut(t *testing.T) {
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-block }))
	defer srv.Close()
	defer close(block)

	start := time.Now()
	code := runHealthcheck(&http.Client{Timeout: 200 * time.Millisecond}, srv.URL)
	if code != 1 || time.Since(start) > 2*time.Second {
		t.Errorf("code=%d elapsed=%v: server menggantung harus gagal cepat", code, time.Since(start))
	}
}
