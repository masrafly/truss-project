package handler_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"truss-project/internal/modules/auth/domain"
)

func authed(r http.Handler, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer valid-user-7")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// ---------- PATCH /auth/me ----------

func TestUpdateMe_Success(t *testing.T) {
	uc := &fakeUC{updateFn: func(context.Context, string, string) (*domain.User, error) {
		u := sampleUser()
		u.Name = "Nama Baru"
		return u, nil
	}}
	w := authed(newRouter(uc, nil), http.MethodPatch, "/auth/me", `{"name":"Nama Baru"}`)

	if w.Code != http.StatusOK || uc.gotUserID != "user-7" || uc.gotNewName != "Nama Baru" {
		t.Fatalf("status %d user=%q name=%q", w.Code, uc.gotUserID, uc.gotNewName)
	}
	data := decode(t, w)["data"].(map[string]any)
	if data["name"] != "Nama Baru" {
		t.Errorf("data = %v", data)
	}
	assertNoSecrets(t, w)
}

func TestUpdateMe_RequiresAuthAndValidBody(t *testing.T) {
	uc := &fakeUC{updateFn: func(context.Context, string, string) (*domain.User, error) { return sampleUser(), nil }}
	r := newRouter(uc, nil)

	req := httptest.NewRequest(http.MethodPatch, "/auth/me", strings.NewReader(`{"name":"X"}`))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized || uc.updateCalls != 0 {
		t.Fatalf("tanpa token: status %d calls %d", w.Code, uc.updateCalls)
	}
	for _, body := range []string{`{}`, `{"name":""}`, `bukan json`} {
		if w := authed(r, http.MethodPatch, "/auth/me", body); w.Code != http.StatusBadRequest {
			t.Errorf("body %q: status %d", body, w.Code)
		}
	}
	if uc.updateCalls != 0 {
		t.Error("use case tidak boleh dipanggil untuk body tidak valid")
	}
}

func TestUpdateMe_ErrorsMapped(t *testing.T) {
	for name, tc := range map[string]struct {
		err  error
		want int
	}{
		"input tidak valid": {domain.ErrInvalidInput, http.StatusBadRequest},
		"user hilang":       {domain.ErrUserNotFound, http.StatusNotFound},
		"tak terduga":       {errors.New("db mati"), http.StatusInternalServerError},
	} {
		t.Run(name, func(t *testing.T) {
			uc := &fakeUC{updateFn: func(context.Context, string, string) (*domain.User, error) { return nil, tc.err }}
			if w := authed(newRouter(uc, nil), http.MethodPatch, "/auth/me", `{"name":"X"}`); w.Code != tc.want {
				t.Errorf("status %d, harapannya %d", w.Code, tc.want)
			}
		})
	}
}

// ---------- POST /auth/change-password ----------

func TestChangePassword_NoContentAndPassesBothPasswords(t *testing.T) {
	uc := &fakeUC{changePwFn: func(context.Context, string, string, string) error { return nil }}
	w := authed(newRouter(uc, nil), http.MethodPost, "/auth/change-password",
		`{"current_password":"lama12345","new_password":"baru12345"}`)

	if w.Code != http.StatusNoContent || w.Body.Len() != 0 {
		t.Fatalf("status %d body %q", w.Code, w.Body)
	}
	if uc.gotUserID != "user-7" || uc.gotCurrentPw != "lama12345" || uc.gotNewPw != "baru12345" {
		t.Errorf("argumen = %q %q %q", uc.gotUserID, uc.gotCurrentPw, uc.gotNewPw)
	}
}

func TestChangePassword_RequiresAuthAndBothFields(t *testing.T) {
	uc := &fakeUC{changePwFn: func(context.Context, string, string, string) error { return nil }}
	r := newRouter(uc, nil)

	if w := post(r, "/auth/change-password", `{"current_password":"a","new_password":"b"}`); w.Code != http.StatusUnauthorized {
		t.Fatalf("tanpa token: status %d", w.Code)
	}
	for _, body := range []string{`{}`, `{"current_password":"a"}`, `{"new_password":"b"}`, `x`} {
		if w := authed(r, http.MethodPost, "/auth/change-password", body); w.Code != http.StatusBadRequest {
			t.Errorf("body %q: status %d", body, w.Code)
		}
	}
	if uc.changePwCalls != 0 {
		t.Error("use case tidak boleh dipanggil untuk body tidak valid")
	}
}

func TestChangePassword_ErrorsMappedWithoutEchoingPasswords(t *testing.T) {
	for name, tc := range map[string]struct {
		err  error
		want int
	}{
		"password saat ini salah": {domain.ErrWrongPassword, http.StatusForbidden},
		"input tidak valid":       {domain.ErrInvalidInput, http.StatusBadRequest},
		"user hilang":             {domain.ErrUserNotFound, http.StatusNotFound},
		"tak terduga":             {errors.New("db mati: lama12345"), http.StatusInternalServerError},
	} {
		t.Run(name, func(t *testing.T) {
			uc := &fakeUC{changePwFn: func(context.Context, string, string, string) error { return tc.err }}
			w := authed(newRouter(uc, nil), http.MethodPost, "/auth/change-password",
				`{"current_password":"lama12345","new_password":"baru12345"}`)
			if w.Code != tc.want {
				t.Errorf("status %d, harapannya %d", w.Code, tc.want)
			}
			if strings.Contains(w.Body.String(), "lama12345") || strings.Contains(w.Body.String(), "baru12345") {
				t.Errorf("respons tidak boleh memuat password: %s", w.Body)
			}
		})
	}
}

func TestChangePassword_TooManyAttemptsIs429WithRetryAfter(t *testing.T) {
	uc := &fakeUC{changePwFn: func(context.Context, string, string, string) error {
		return &domain.TooManyAttemptsError{RetryAfter: 90 * time.Second}
	}}
	w := authed(newRouter(uc, nil), http.MethodPost, "/auth/change-password",
		`{"current_password":"lama12345","new_password":"baru12345"}`)
	if w.Code != http.StatusTooManyRequests || w.Header().Get("Retry-After") != "90" {
		t.Fatalf("status %d Retry-After %q", w.Code, w.Header().Get("Retry-After"))
	}
}
