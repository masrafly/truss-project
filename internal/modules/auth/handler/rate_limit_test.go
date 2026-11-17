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

func TestLogin_TooManyAttemptsMapsTo429WithRetryAfter(t *testing.T) {
	uc := &fakeUC{loginFn: func(context.Context, string, string) (*usecase.LoginResult, error) {
		return nil, &domain.TooManyAttemptsError{RetryAfter: 90*time.Second + time.Millisecond}
	}}

	w := post(newRouter(uc, nil), "/auth/login", `{"email":"budi@example.com","password":"password123"}`)

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("status %d, harapannya 429", w.Code)
	}
	if ra := w.Header().Get("Retry-After"); ra != "91" {
		t.Errorf("Retry-After = %q, harapannya 91 (dibulatkan ke atas)", ra)
	}
	if strings.Contains(w.Body.String(), "budi") {
		t.Error("respons tidak boleh memuat email")
	}
}

func TestLogin_PlainErrTooManyAttemptsStillMapsTo429(t *testing.T) {
	uc := &fakeUC{loginFn: func(context.Context, string, string) (*usecase.LoginResult, error) {
		return nil, domain.ErrTooManyAttempts
	}}
	w := post(newRouter(uc, nil), "/auth/login", `{"email":"a@b.com","password":"password123"}`)
	if w.Code != http.StatusTooManyRequests || w.Header().Get("Retry-After") == "" {
		t.Fatalf("status %d, Retry-After %q", w.Code, w.Header().Get("Retry-After"))
	}
}

// publicMW hanya dipasang pada register & login, tidak pada /auth/me.
func TestRegisterRoutes_PublicMiddlewareOnlyOnRegisterAndLogin(t *testing.T) {
	var hits []string
	mark := func(c *gin.Context) { hits = append(hits, c.FullPath()); c.Next() }

	uc := &fakeUC{
		loginFn: func(context.Context, string, string) (*usecase.LoginResult, error) {
			return nil, domain.ErrInvalidCredentials
		},
	}
	r := gin.New()
	handler.NewAuthHandler(uc, nil).RegisterRoutes(r, middleware.AuthMiddleware(fakeValidator{}), mark)

	post(r, "/auth/login", `{"email":"a@b.com","password":"password123"}`)
	post(r, "/auth/register", `{}`)
	req := httptest.NewRequest("GET", "/auth/me", nil)
	r.ServeHTTP(httptest.NewRecorder(), req)

	if strings.Join(hits, ",") != "/auth/login,/auth/register" {
		t.Errorf("middleware publik dijalankan pada: %v", hits)
	}
}

// Slice middleware dengan sisa kapasitas tidak boleh membuat /auth/register
// menjalankan handler login: setiap route harus memakai handler-nya sendiri.
func TestRegisterRoutes_SpareCapacityDoesNotMixHandlers(t *testing.T) {
	loginCalled := false
	uc := &fakeUC{
		loginFn: func(context.Context, string, string) (*usecase.LoginResult, error) {
			loginCalled = true
			return nil, errors.New("tidak boleh dipanggil")
		},
	}
	mw := make([]gin.HandlerFunc, 1, 10) // sisa kapasitas besar
	mw[0] = func(c *gin.Context) { c.Next() }

	r := gin.New()
	handler.NewAuthHandler(uc, nil).RegisterRoutes(r, middleware.AuthMiddleware(fakeValidator{}), mw...)
	w := post(r, "/auth/register", `{"name":"B"}`) // body tidak valid -> 400 dari handler Register

	if loginCalled {
		t.Fatal("/auth/register menjalankan handler Login")
	}
	if w.Code != http.StatusBadRequest {
		t.Errorf("status %d, harapannya 400 dari handler Register", w.Code)
	}
}
