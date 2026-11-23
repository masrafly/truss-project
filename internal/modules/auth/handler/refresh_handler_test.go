package handler_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"truss-project/internal/middleware"
	"truss-project/internal/modules/auth/domain"
	"truss-project/internal/modules/auth/handler"
	"truss-project/internal/modules/auth/usecase"
)

func samplePair() *usecase.TokenPair {
	return &usecase.TokenPair{
		AccessToken: "access-2", ExpiresIn: 15 * time.Minute,
		RefreshToken: "refresh-2", RefreshExpiresIn: 720 * time.Hour,
	}
}

// ---------- login ----------

func TestLogin_IncludesRefreshTokenOnlyWhenIssued(t *testing.T) {
	for name, tc := range map[string]struct {
		refresh string
		want    bool
	}{"ada": {"refresh-1", true}, "kosong": {"", false}} {
		t.Run(name, func(t *testing.T) {
			uc := &fakeUC{loginFn: func(context.Context, string, string) (*usecase.LoginResult, error) {
				return &usecase.LoginResult{
					User: sampleUser(), AccessToken: "access-1", ExpiresIn: 15 * time.Minute,
					RefreshToken: tc.refresh, RefreshExpiresIn: 720 * time.Hour,
				}, nil
			}}
			w := post(newRouter(uc, nil), "/auth/login", `{"email":"budi@example.com","password":"password123"}`)
			body := decode(t, w)
			_, has := body["refresh_token"]
			if w.Code != http.StatusOK || has != tc.want {
				t.Fatalf("status %d, refresh_token ada = %v, harapannya %v: %s", w.Code, has, tc.want, w.Body)
			}
			if tc.want && (body["refresh_token"] != "refresh-1" || body["refresh_expires_in"] != float64(720*3600)) {
				t.Errorf("body = %v", body)
			}
		})
	}
}

// ---------- refresh ----------

func TestRefresh_SuccessReturnsNewPair(t *testing.T) {
	uc := &fakeUC{refreshFn: func(context.Context, string) (*usecase.TokenPair, error) { return samplePair(), nil }}
	w := post(newRouter(uc, nil), "/auth/refresh", `{"refresh_token":"refresh-1"}`)

	if w.Code != http.StatusOK || uc.gotRefreshToken != "refresh-1" {
		t.Fatalf("status %d, token ke use case = %q", w.Code, uc.gotRefreshToken)
	}
	body := decode(t, w)
	if body["access_token"] != "access-2" || body["refresh_token"] != "refresh-2" || body["token_type"] != "Bearer" ||
		body["expires_in"] != float64(900) || body["refresh_expires_in"] != float64(720*3600) {
		t.Errorf("body = %v", body)
	}
}

func TestRefresh_InvalidTokenIs401AndBadBodyIs400(t *testing.T) {
	uc := &fakeUC{refreshFn: func(context.Context, string) (*usecase.TokenPair, error) {
		return nil, domain.ErrInvalidRefreshToken
	}}
	r := newRouter(uc, nil)

	w := post(r, "/auth/refresh", `{"refresh_token":"x"}`)
	if w.Code != http.StatusUnauthorized || decode(t, w)["message"] != "refresh token tidak valid" {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	before := uc.refreshCalls
	for _, body := range []string{`{}`, `{"refresh_token":""}`, `bukan json`} {
		if w := post(r, "/auth/refresh", body); w.Code != http.StatusBadRequest {
			t.Errorf("body %q: status %d, harapannya 400", body, w.Code)
		}
	}
	if uc.refreshCalls != before {
		t.Error("use case tidak boleh dipanggil untuk body tidak valid")
	}
}

func TestRefresh_UnexpectedErrorIs500WithoutDetail(t *testing.T) {
	uc := &fakeUC{refreshFn: func(context.Context, string) (*usecase.TokenPair, error) {
		return nil, errors.New("koneksi db: password=rahasia")
	}}
	w := post(newRouter(uc, nil), "/auth/refresh", `{"refresh_token":"x"}`)
	if w.Code != http.StatusInternalServerError || strings.Contains(w.Body.String(), "rahasia") {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
}

// ---------- logout ----------

func TestLogout_Returns204AndPassesToken(t *testing.T) {
	uc := &fakeUC{logoutFn: func(context.Context, string) error { return nil }}
	w := post(newRouter(uc, nil), "/auth/logout", `{"refresh_token":"refresh-1"}`)
	if w.Code != http.StatusNoContent || w.Body.Len() != 0 || uc.gotRefreshToken != "refresh-1" {
		t.Fatalf("status %d body %q token %q", w.Code, w.Body, uc.gotRefreshToken)
	}
}

func TestLogout_BadBodyIs400AndErrorsMapped(t *testing.T) {
	uc := &fakeUC{logoutFn: func(context.Context, string) error { return domain.ErrInvalidInput }}
	r := newRouter(uc, nil)
	if w := post(r, "/auth/logout", `{}`); w.Code != http.StatusBadRequest || uc.logoutCalls != 0 {
		t.Errorf("body kosong: status %d, calls %d", w.Code, uc.logoutCalls)
	}
	if w := post(r, "/auth/logout", `{"refresh_token":"x"}`); w.Code != http.StatusBadRequest {
		t.Errorf("ErrInvalidInput harus 400, dapat %d", w.Code)
	}
	uc.logoutFn = func(context.Context, string) error { return errors.New("db mati") }
	if w := post(r, "/auth/logout", `{"refresh_token":"x"}`); w.Code != http.StatusInternalServerError {
		t.Errorf("error tak terduga harus 500, dapat %d", w.Code)
	}
}

// ---------- logout-all ----------

func TestLogoutAll_RequiresAuthAndUsesTokenIdentity(t *testing.T) {
	uc := &fakeUC{logoutAll: func(context.Context, string) error { return nil }}
	r := newRouter(uc, nil)

	if w := post(r, "/auth/logout-all", ""); w.Code != http.StatusUnauthorized || uc.logoutAllCalls != 0 {
		t.Fatalf("tanpa token: status %d, calls %d", w.Code, uc.logoutAllCalls)
	}

	req := httptest.NewRequest(http.MethodPost, "/auth/logout-all", nil)
	req.Header.Set("Authorization", "Bearer valid-user-42")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent || uc.gotUserID != "user-42" {
		t.Fatalf("status %d, user = %q", w.Code, uc.gotUserID)
	}
}

func TestLogoutAll_ErrorsMapped(t *testing.T) {
	uc := &fakeUC{logoutAll: func(context.Context, string) error { return errors.New("db mati") }}
	req := httptest.NewRequest(http.MethodPost, "/auth/logout-all", nil)
	req.Header.Set("Authorization", "Bearer valid-u")
	w := httptest.NewRecorder()
	newRouter(uc, nil).ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status %d", w.Code)
	}
}

// ---------- middleware publik ----------

// Rate limit per IP harus melindungi refresh dan logout (endpoint tanpa JWT), tetapi tidak
// logout-all yang sudah dilindungi JWT.
func TestRegisterRoutes_PublicMiddlewareCoversRefreshAndLogout(t *testing.T) {
	var hits []string
	mark := func(c *gin.Context) { hits = append(hits, c.FullPath()); c.Next() }
	uc := &fakeUC{
		refreshFn: func(context.Context, string) (*usecase.TokenPair, error) { return samplePair(), nil },
		logoutFn:  func(context.Context, string) error { return nil },
		logoutAll: func(context.Context, string) error { return nil },
	}
	r := gin.New()
	handler.NewAuthHandler(uc, nil).RegisterRoutes(r, middleware.AuthMiddleware(fakeValidator{}), mark)

	post(r, "/auth/refresh", `{"refresh_token":"x"}`)
	post(r, "/auth/logout", `{"refresh_token":"x"}`)
	req := httptest.NewRequest(http.MethodPost, "/auth/logout-all", nil)
	req.Header.Set("Authorization", "Bearer valid-u")
	r.ServeHTTP(httptest.NewRecorder(), req)

	if strings.Join(hits, ",") != "/auth/refresh,/auth/logout" {
		t.Errorf("middleware publik dijalankan pada: %v", hits)
	}
}
