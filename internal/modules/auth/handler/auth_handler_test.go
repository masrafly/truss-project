package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"truss-project/internal/middleware"
	"truss-project/internal/modules/auth/domain"
	"truss-project/internal/modules/auth/handler"
	"truss-project/internal/modules/auth/usecase"
)

func init() { gin.SetMode(gin.TestMode) }

// ---------- fakes ----------

type fakeUC struct {
	registerFn func(ctx context.Context, name, email, password string) (*domain.User, error)
	loginFn    func(ctx context.Context, email, password string) (*usecase.LoginResult, error)
	profileFn  func(ctx context.Context, userID string) (*domain.User, error)
	refreshFn  func(ctx context.Context, token string) (*usecase.TokenPair, error)
	logoutFn   func(ctx context.Context, token string) error
	logoutAll  func(ctx context.Context, userID string) error
	updateFn   func(ctx context.Context, userID, name string) (*domain.User, error)
	changePwFn func(ctx context.Context, userID, current, next string) error

	registerCalls, loginCalls, profileCalls   int
	gotName, gotEmail, gotPassword            string
	gotUserID                                 string
	refreshCalls, logoutCalls, logoutAllCalls int
	gotRefreshToken                           string
	updateCalls, changePwCalls                int
	gotNewName, gotCurrentPw, gotNewPw        string
}

func (f *fakeUC) Register(ctx context.Context, name, email, password string) (*domain.User, error) {
	f.registerCalls++
	f.gotName, f.gotEmail, f.gotPassword = name, email, password
	return f.registerFn(ctx, name, email, password)
}

func (f *fakeUC) Login(ctx context.Context, email, password string) (*usecase.LoginResult, error) {
	f.loginCalls++
	f.gotEmail, f.gotPassword = email, password
	return f.loginFn(ctx, email, password)
}

func (f *fakeUC) GetProfile(ctx context.Context, userID string) (*domain.User, error) {
	f.profileCalls++
	f.gotUserID = userID
	return f.profileFn(ctx, userID)
}

func (f *fakeUC) Refresh(ctx context.Context, token string) (*usecase.TokenPair, error) {
	f.refreshCalls++
	f.gotRefreshToken = token
	return f.refreshFn(ctx, token)
}

func (f *fakeUC) Logout(ctx context.Context, token string) error {
	f.logoutCalls++
	f.gotRefreshToken = token
	return f.logoutFn(ctx, token)
}

func (f *fakeUC) LogoutAll(ctx context.Context, userID string) error {
	f.logoutAllCalls++
	f.gotUserID = userID
	return f.logoutAll(ctx, userID)
}

func (f *fakeUC) UpdateProfile(ctx context.Context, userID, name string) (*domain.User, error) {
	f.updateCalls++
	f.gotUserID, f.gotNewName = userID, name
	return f.updateFn(ctx, userID, name)
}

func (f *fakeUC) ChangePassword(ctx context.Context, userID, current, next string) error {
	f.changePwCalls++
	f.gotUserID, f.gotCurrentPw, f.gotNewPw = userID, current, next
	return f.changePwFn(ctx, userID, current, next)
}

type fakeValidator struct{}

func (fakeValidator) Validate(token string) (string, error) {
	if strings.HasPrefix(token, "valid-") {
		return strings.TrimPrefix(token, "valid-"), nil
	}
	return "", errors.New("invalid")
}

var fixedTime = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

func sampleUser() *domain.User {
	return &domain.User{
		ID:           uuid.MustParse("3f2b8c5e-9d1a-4c7e-8a55-0d5e9f1b7a11"),
		Name:         "Budi",
		Email:        "budi@example.com",
		PasswordHash: "SHOULD-NEVER-APPEAR-IN-RESPONSE", // sengaja diisi: DTO tidak boleh membocorkannya
		CreatedAt:    fixedTime,
		UpdatedAt:    fixedTime,
	}
}

// ---------- helper ----------

func newLogger() (*slog.Logger, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	return slog.New(slog.NewJSONHandler(buf, nil)), buf
}

func newRouter(uc *fakeUC, log *slog.Logger) *gin.Engine {
	r := gin.New()
	r.Use(middleware.RequestID())
	handler.NewAuthHandler(uc, log).RegisterRoutes(r, middleware.AuthMiddleware(fakeValidator{}))
	return r
}

func post(r http.Handler, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func get(r http.Handler, path, authHeader string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func decode(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	// Unmarshal seluruh body: gagal jika ada lebih dari satu objek JSON (regresi bug c.JSON ganda).
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatalf("body bukan satu objek JSON yang valid: %v\nbody: %s", err, w.Body.String())
	}
	return m
}

func assertNoSecrets(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	body := w.Body.String()
	for _, s := range []string{"SHOULD-NEVER-APPEAR-IN-RESPONSE", "password_hash", "PasswordHash", "password"} {
		if strings.Contains(body, s) {
			t.Errorf("respons tidak boleh memuat %q: %s", s, body)
		}
	}
}

const validRegister = `{"name":"Budi","email":"budi@example.com","password":"password123"}`
const validLogin = `{"email":"budi@example.com","password":"password123"}`

// ---------- routes ----------

func TestRegisterRoutes(t *testing.T) {
	r := newRouter(&fakeUC{}, nil)

	got := map[string]bool{}
	for _, ri := range r.Routes() {
		got[ri.Method+" "+ri.Path] = true
	}
	for _, want := range []string{"POST /auth/register", "POST /auth/login", "POST /auth/refresh", "POST /auth/logout", "POST /auth/logout-all", "GET /auth/me", "PATCH /auth/me", "POST /auth/change-password"} {
		if !got[want] {
			t.Errorf("route %q harus terdaftar; ada: %v", want, got)
		}
	}
	if len(got) != 8 {
		t.Errorf("harapannya tepat 8 route, dapat %d: %v", len(got), got)
	}
}

func TestNewAuthHandler_NilLoggerIsSafe(t *testing.T) {
	uc := &fakeUC{registerFn: func(context.Context, string, string, string) (*domain.User, error) {
		return nil, errors.New("boom")
	}}
	r := newRouter(uc, nil) // nil logger tidak boleh panic, termasuk saat mencatat error 500

	if w := post(r, "/auth/register", validRegister); w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", w.Code)
	}
}

// ---------- Register ----------

func TestRegister_Success(t *testing.T) {
	uc := &fakeUC{registerFn: func(context.Context, string, string, string) (*domain.User, error) {
		return sampleUser(), nil
	}}

	w := post(newRouter(uc, nil), "/auth/register", validRegister)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, harapannya 201; body: %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q", ct)
	}

	body := decode(t, w)
	if body["message"] != "Akun berhasil dibuat" {
		t.Errorf("message = %v", body["message"])
	}
	data, _ := body["data"].(map[string]any)
	if data == nil {
		t.Fatalf("data harus berupa objek: %v", body)
	}
	want := map[string]any{
		"internal_id": "3f2b8c5e-9d1a-4c7e-8a55-0d5e9f1b7a11",
		"name":        "Budi",
		"email":       "budi@example.com",
		"created_at":  "2026-01-02T03:04:05Z",
		"updated_at":  "2026-01-02T03:04:05Z",
	}
	for k, v := range want {
		if data[k] != v {
			t.Errorf("data[%q] = %v, harapannya %v", k, data[k], v)
		}
	}
	if len(data) != len(want) {
		t.Errorf("DTO hanya boleh memuat %d field, dapat %d: %v", len(want), len(data), data)
	}
	assertNoSecrets(t, w)
}

func TestRegister_PassesInputToUseCaseUnchanged(t *testing.T) {
	// Normalisasi (trim/lowercase) adalah tugas use case, bukan handler.
	uc := &fakeUC{registerFn: func(context.Context, string, string, string) (*domain.User, error) {
		return sampleUser(), nil
	}}

	post(newRouter(uc, nil), "/auth/register",
		`{"name":"  Budi ","email":"Budi@Example.com","password":"password123"}`)

	if uc.gotName != "  Budi " || uc.gotEmail != "Budi@Example.com" || uc.gotPassword != "password123" {
		t.Errorf("argumen use case = (%q, %q, %q)", uc.gotName, uc.gotEmail, uc.gotPassword)
	}
}

func TestRegister_IgnoresExtraFieldsInBody(t *testing.T) {
	// Mass assignment: id/hash/timestamps dari client tidak boleh sampai ke use case.
	var gotArgs int
	uc := &fakeUC{registerFn: func(_ context.Context, n, e, p string) (*domain.User, error) {
		gotArgs++
		return sampleUser(), nil
	}}

	w := post(newRouter(uc, nil), "/auth/register",
		`{"name":"Budi","email":"budi@example.com","password":"password123",
		  "internal_id":"00000000-0000-0000-0000-000000000000","password_hash":"x","role":"admin"}`)

	if w.Code != http.StatusCreated || gotArgs != 1 {
		t.Fatalf("status = %d, calls = %d", w.Code, gotArgs)
	}
	if w.Body.String() != "" && strings.Contains(w.Body.String(), "00000000-0000-0000-0000-000000000000") {
		t.Error("ID dari client tidak boleh muncul di respons")
	}
}

func TestRegister_InvalidBodyRejectedBeforeUseCase(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"bukan JSON", `bukan json`},
		{"body kosong", ``},
		{"JSON rusak", `{"name":"Budi",`},
		{"nama hilang", `{"email":"budi@example.com","password":"password123"}`},
		{"nama kosong", `{"name":"","email":"budi@example.com","password":"password123"}`},
		{"email hilang", `{"name":"Budi","password":"password123"}`},
		{"email tidak valid", `{"name":"Budi","email":"bukan-email","password":"password123"}`},
		{"password hilang", `{"name":"Budi","email":"budi@example.com"}`},
		{"password 7 karakter", `{"name":"Budi","email":"budi@example.com","password":"1234567"}`},
		{"tipe salah", `{"name":123,"email":"budi@example.com","password":"password123"}`},
		{"array", `[]`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			uc := &fakeUC{}
			w := post(newRouter(uc, nil), "/auth/register", tc.body)

			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, harapannya 400", w.Code)
			}
			if uc.registerCalls != 0 {
				t.Error("use case tidak boleh dipanggil untuk input tidak valid")
			}
			if msg := decode(t, w)["message"]; msg != "Input tidak valid" {
				t.Errorf("message = %v", msg)
			}
		})
	}
}

func TestRegister_ErrorMapping(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		status int
	}{
		{"input tidak valid", domain.ErrInvalidInput, http.StatusBadRequest},
		{"email dipakai", domain.ErrEmailAlreadyUsed, http.StatusConflict},
		{"kredensial salah", domain.ErrInvalidCredentials, http.StatusUnauthorized},
		{"user tidak ada", domain.ErrUserNotFound, http.StatusNotFound},
		{"error domain dibungkus", fmt.Errorf("konteks: %w", domain.ErrEmailAlreadyUsed), http.StatusConflict},
		{"error tak terduga", errors.New("connection refused: db://user:pw@10.0.0.5"), http.StatusInternalServerError},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			uc := &fakeUC{registerFn: func(context.Context, string, string, string) (*domain.User, error) {
				return nil, tc.err
			}}

			w := post(newRouter(uc, nil), "/auth/register", validRegister)

			if w.Code != tc.status {
				t.Fatalf("status = %d, harapannya %d", w.Code, tc.status)
			}
			if _, ok := decode(t, w)["message"]; !ok {
				t.Error("respons error harus memuat message")
			}
		})
	}
}

func TestUnexpectedError_GenericBodyAndLoggedWithRequestID(t *testing.T) {
	secretErr := errors.New("connection refused: db://user:pw@10.0.0.5")
	uc := &fakeUC{registerFn: func(context.Context, string, string, string) (*domain.User, error) {
		return nil, secretErr
	}}
	log, buf := newLogger()

	req := httptest.NewRequest(http.MethodPost, "/auth/register", strings.NewReader(validRegister))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-ID", "rid-500")
	w := httptest.NewRecorder()
	newRouter(uc, log).ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", w.Code)
	}

	// Client: pesan generik, tanpa detail internal.
	if msg := decode(t, w)["message"]; msg != "Terjadi kesalahan pada server" {
		t.Errorf("message = %v", msg)
	}
	if strings.Contains(w.Body.String(), "db://") || strings.Contains(w.Body.String(), "10.0.0.5") {
		t.Errorf("detail internal bocor ke client: %s", w.Body.String())
	}

	// Log: detail error dan request_id tercatat sebagai ERROR.
	var rec map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &rec); err != nil {
		t.Fatalf("log harus satu baris JSON: %v\n%s", err, buf.String())
	}
	if rec["level"] != "ERROR" || rec["request_id"] != "rid-500" || rec["module"] != "auth" {
		t.Errorf("rekaman log tidak sesuai: %v", rec)
	}
	if e, _ := rec["error"].(string); !strings.Contains(e, "connection refused") {
		t.Errorf("error asli harus ada di log: %v", rec["error"])
	}
	if strings.Contains(buf.String(), "password123") {
		t.Error("log tidak boleh memuat password")
	}
}

func TestExpectedErrors_AreNotLoggedByHandler(t *testing.T) {
	// 4xx adalah kejadian normal; pencatatan ditangani middleware/use case, bukan handler.
	uc := &fakeUC{registerFn: func(context.Context, string, string, string) (*domain.User, error) {
		return nil, domain.ErrEmailAlreadyUsed
	}}
	log, buf := newLogger()

	post(newRouter(uc, log), "/auth/register", validRegister)

	if strings.TrimSpace(buf.String()) != "" {
		t.Errorf("handler tidak boleh mencatat error yang diharapkan: %s", buf.String())
	}
}

// ---------- Login ----------

func TestLogin_Success(t *testing.T) {
	uc := &fakeUC{loginFn: func(context.Context, string, string) (*usecase.LoginResult, error) {
		return &usecase.LoginResult{User: sampleUser(), AccessToken: "jwt-token-abc", ExpiresIn: 90 * time.Minute}, nil
	}}

	w := post(newRouter(uc, nil), "/auth/login", validLogin)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; body: %s", w.Code, w.Body.String())
	}

	body := decode(t, w) // juga membuktikan hanya ada SATU objek JSON di body
	if body["message"] != "Login berhasil" {
		t.Errorf("message = %v", body["message"])
	}
	if body["access_token"] != "jwt-token-abc" {
		t.Errorf("access_token = %v", body["access_token"])
	}
	if body["token_type"] != "Bearer" {
		t.Errorf("token_type = %v", body["token_type"])
	}
	if body["expires_in"] != float64(5400) {
		t.Errorf("expires_in = %v, harapannya 5400 detik", body["expires_in"])
	}
	data, _ := body["data"].(map[string]any)
	if data["email"] != "budi@example.com" || data["internal_id"] == "" {
		t.Errorf("data user tidak sesuai: %v", data)
	}
	if _, has := data["password_hash"]; has {
		t.Error("data tidak boleh memuat password_hash")
	}
	if strings.Contains(w.Body.String(), "SHOULD-NEVER-APPEAR-IN-RESPONSE") {
		t.Error("hash bocor di respons")
	}
}

func TestLogin_ResponseIsExactlyOneJSONDocument(t *testing.T) {
	// Regresi: versi lama memanggil c.JSON dua kali sehingga body berisi dua objek.
	uc := &fakeUC{loginFn: func(context.Context, string, string) (*usecase.LoginResult, error) {
		return &usecase.LoginResult{User: sampleUser(), AccessToken: "t", ExpiresIn: time.Hour}, nil
	}}

	w := post(newRouter(uc, nil), "/auth/login", validLogin)

	dec := json.NewDecoder(w.Body)
	var first map[string]any
	if err := dec.Decode(&first); err != nil {
		t.Fatal(err)
	}
	if dec.More() {
		t.Error("body memuat lebih dari satu dokumen JSON")
	}
}

func TestLogin_PassesCredentialsToUseCase(t *testing.T) {
	uc := &fakeUC{loginFn: func(context.Context, string, string) (*usecase.LoginResult, error) {
		return &usecase.LoginResult{User: sampleUser(), AccessToken: "t", ExpiresIn: time.Hour}, nil
	}}

	post(newRouter(uc, nil), "/auth/login", `{"email":"Budi@Example.com","password":"password123"}`)

	if uc.gotEmail != "Budi@Example.com" || uc.gotPassword != "password123" {
		t.Errorf("argumen = (%q, %q)", uc.gotEmail, uc.gotPassword)
	}
}

func TestLogin_InvalidBodyRejectedBeforeUseCase(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"bukan JSON", `x`},
		{"body kosong", ``},
		{"email hilang", `{"password":"password123"}`},
		{"email tidak valid", `{"email":"bukan-email","password":"password123"}`},
		{"password hilang", `{"email":"budi@example.com"}`},
		{"password kosong", `{"email":"budi@example.com","password":""}`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			uc := &fakeUC{}
			w := post(newRouter(uc, nil), "/auth/login", tc.body)

			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, harapannya 400", w.Code)
			}
			if uc.loginCalls != 0 {
				t.Error("use case tidak boleh dipanggil")
			}
		})
	}
}

func TestLogin_ShortPasswordIsPassedToUseCase(t *testing.T) {
	// Aturan minimal 8 karakter hanya untuk register; akun lama harus tetap bisa
	// sampai ke use case dan gagal sebagai "kredensial salah", bukan 400.
	uc := &fakeUC{loginFn: func(context.Context, string, string) (*usecase.LoginResult, error) {
		return nil, domain.ErrInvalidCredentials
	}}

	w := post(newRouter(uc, nil), "/auth/login", `{"email":"budi@example.com","password":"pendek"}`)

	if uc.loginCalls != 1 || w.Code != http.StatusUnauthorized {
		t.Fatalf("calls = %d, status = %d", uc.loginCalls, w.Code)
	}
}

func TestLogin_ErrorMapping(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		status int
	}{
		{"kredensial salah", domain.ErrInvalidCredentials, http.StatusUnauthorized},
		{"error domain dibungkus", fmt.Errorf("x: %w", domain.ErrInvalidCredentials), http.StatusUnauthorized},
		{"input tidak valid", domain.ErrInvalidInput, http.StatusBadRequest},
		{"error tak terduga", errors.New("db mati"), http.StatusInternalServerError},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			uc := &fakeUC{loginFn: func(context.Context, string, string) (*usecase.LoginResult, error) {
				return nil, tc.err
			}}

			w := post(newRouter(uc, nil), "/auth/login", validLogin)

			if w.Code != tc.status {
				t.Fatalf("status = %d, harapannya %d", w.Code, tc.status)
			}
			if strings.Contains(w.Body.String(), "access_token") {
				t.Error("respons error tidak boleh memuat access_token")
			}
		})
	}
}

func TestLogin_FailureResponsesAreIndistinguishable(t *testing.T) {
	// Handler tidak boleh membedakan "email tak terdaftar" vs "password salah":
	// keduanya datang sebagai ErrInvalidCredentials dan menghasilkan body identik.
	uc := &fakeUC{loginFn: func(context.Context, string, string) (*usecase.LoginResult, error) {
		return nil, domain.ErrInvalidCredentials
	}}
	r := newRouter(uc, nil)

	a := post(r, "/auth/login", `{"email":"ada@example.com","password":"salah-salah"}`)
	b := post(r, "/auth/login", `{"email":"tidakada@example.com","password":"salah-salah"}`)

	if a.Code != b.Code || a.Body.String() != b.Body.String() {
		t.Errorf("respons berbeda:\n%d %s\n%d %s", a.Code, a.Body.String(), b.Code, b.Body.String())
	}
}

// ---------- Me ----------

func TestMe_Success(t *testing.T) {
	uc := &fakeUC{profileFn: func(context.Context, string) (*domain.User, error) { return sampleUser(), nil }}

	w := get(newRouter(uc, nil), "/auth/me", "Bearer valid-user-42")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; body: %s", w.Code, w.Body.String())
	}
	if uc.gotUserID != "user-42" {
		t.Errorf("use case harus menerima user ID dari token, dapat %q", uc.gotUserID)
	}

	body := decode(t, w)
	if body["message"] != "Token valid" {
		t.Errorf("message = %v", body["message"])
	}
	data, _ := body["data"].(map[string]any)
	if data["email"] != "budi@example.com" || data["name"] != "Budi" {
		t.Errorf("data tidak sesuai: %v", data)
	}
	assertNoSecrets(t, w)
}

func TestMe_RejectedWithoutValidToken(t *testing.T) {
	for name, header := range map[string]string{
		"tanpa header":   "",
		"token salah":    "Bearer salah",
		"bukan Bearer":   "Basic valid-user-42",
		"hanya scheme":   "Bearer",
		"token berlebih": "Bearer valid-a valid-b",
	} {
		t.Run(name, func(t *testing.T) {
			uc := &fakeUC{profileFn: func(context.Context, string) (*domain.User, error) { return sampleUser(), nil }}

			w := get(newRouter(uc, nil), "/auth/me", header)

			if w.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, harapannya 401", w.Code)
			}
			if uc.profileCalls != 0 {
				t.Error("use case tidak boleh dipanggil tanpa autentikasi")
			}
		})
	}
}

func TestMe_ErrorMapping(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		status int
	}{
		{"user sudah tidak ada", domain.ErrUserNotFound, http.StatusNotFound},
		{"error tak terduga", errors.New("db mati"), http.StatusInternalServerError},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			uc := &fakeUC{profileFn: func(context.Context, string) (*domain.User, error) { return nil, tc.err }}

			w := get(newRouter(uc, nil), "/auth/me", "Bearer valid-user-42")

			if w.Code != tc.status {
				t.Fatalf("status = %d, harapannya %d", w.Code, tc.status)
			}
		})
	}
}

func TestMe_WithoutAuthMiddlewareContextIsRejected(t *testing.T) {
	// Pertahanan berlapis: jika route lupa dipasangi AuthMiddleware,
	// handler tetap menolak karena tidak ada identitas di context.
	uc := &fakeUC{profileFn: func(context.Context, string) (*domain.User, error) { return sampleUser(), nil }}
	h := handler.NewAuthHandler(uc, nil)

	r := gin.New()
	r.GET("/me-tanpa-middleware", h.Me)

	w := get(r, "/me-tanpa-middleware", "")

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, harapannya 401", w.Code)
	}
	if uc.profileCalls != 0 {
		t.Error("use case tidak boleh dipanggil")
	}
}

func TestContextIsPropagatedToUseCase(t *testing.T) {
	type ctxKey struct{}
	var seen any
	uc := &fakeUC{registerFn: func(ctx context.Context, _, _, _ string) (*domain.User, error) {
		seen = ctx.Value(ctxKey{})
		return sampleUser(), nil
	}}

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), ctxKey{}, "dari-request"))
		c.Next()
	})
	handler.NewAuthHandler(uc, nil).RegisterRoutes(r, middleware.AuthMiddleware(fakeValidator{}))

	post(r, "/auth/register", validRegister)

	if seen != "dari-request" {
		t.Errorf("context request harus diteruskan ke use case, dapat %v", seen)
	}
}
