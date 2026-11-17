package middleware_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"truss-project/internal/middleware"
)

func init() { gin.SetMode(gin.TestMode) }

type fakeValidator struct {
	validToken string
	userID     string
	calls      int
	lastToken  string
}

func (v *fakeValidator) Validate(token string) (string, error) {
	v.calls++
	v.lastToken = token
	if token == v.validToken {
		return v.userID, nil
	}
	return "", errors.New("token tidak valid")
}

// newRouter membuat route /private yang dilindungi AuthMiddleware.
// reached menandai apakah handler dijangkau.
func newRouter(v middleware.TokenValidator, reached *bool, gotUserID *string) *gin.Engine {
	r := gin.New()
	r.GET("/private", middleware.AuthMiddleware(v), func(c *gin.Context) {
		*reached = true
		if id, ok := middleware.UserIDFromContext(c); ok {
			*gotUserID = id
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
	return r
}

func do(r http.Handler, header string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/private", nil)
	if header != "" {
		req.Header.Set("Authorization", header)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestAuthMiddleware_ValidToken(t *testing.T) {
	v := &fakeValidator{validToken: "good", userID: "user-123"}
	var reached bool
	var uid string
	r := newRouter(v, &reached, &uid)

	w := do(r, "Bearer good")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, harapannya 200", w.Code)
	}
	if !reached {
		t.Error("handler harus dijangkau")
	}
	if uid != "user-123" {
		t.Errorf("user_id di context = %q", uid)
	}
	if v.lastToken != "good" {
		t.Errorf("validator menerima %q, harapannya token tanpa prefix Bearer", v.lastToken)
	}
}

func TestAuthMiddleware_SchemeIsCaseInsensitive(t *testing.T) {
	for _, scheme := range []string{"bearer", "BEARER", "BeArEr"} {
		t.Run(scheme, func(t *testing.T) {
			v := &fakeValidator{validToken: "good", userID: "u1"}
			var reached bool
			var uid string

			w := do(newRouter(v, &reached, &uid), scheme+" good")

			if w.Code != http.StatusOK || !reached {
				t.Fatalf("status = %d, reached = %v", w.Code, reached)
			}
		})
	}
}

func TestAuthMiddleware_ExtraWhitespaceTolerated(t *testing.T) {
	v := &fakeValidator{validToken: "good", userID: "u1"}
	var reached bool
	var uid string

	w := do(newRouter(v, &reached, &uid), "Bearer    good")

	if w.Code != http.StatusOK || !reached {
		t.Fatalf("status = %d, reached = %v", w.Code, reached)
	}
}

func TestAuthMiddleware_MissingOrMalformedHeader(t *testing.T) {
	tests := []struct {
		name   string
		header string
	}{
		{"tanpa header", ""},
		{"hanya scheme", "Bearer"},
		{"hanya token", "good"},
		{"scheme Basic", "Basic good"},
		{"scheme Token", "Token good"},
		{"token berlebih", "Bearer good extra"},
		{"hanya spasi", "   "},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			v := &fakeValidator{validToken: "good", userID: "u1"}
			var reached bool
			var uid string

			w := do(newRouter(v, &reached, &uid), tc.header)

			if w.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, harapannya 401", w.Code)
			}
			if reached {
				t.Error("handler tidak boleh dijangkau")
			}
			if v.calls != 0 {
				t.Error("validator tidak boleh dipanggil untuk header yang bentuknya salah")
			}
			if ct := w.Header().Get("Content-Type"); ct == "" {
				t.Error("respons error harus berupa JSON")
			}
		})
	}
}

func TestAuthMiddleware_InvalidToken(t *testing.T) {
	v := &fakeValidator{validToken: "good", userID: "u1"}
	var reached bool
	var uid string

	w := do(newRouter(v, &reached, &uid), "Bearer salah")

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, harapannya 401", w.Code)
	}
	if reached {
		t.Error("handler tidak boleh dijangkau")
	}
	if uid != "" {
		t.Error("user_id tidak boleh terisi")
	}
	if v.calls != 1 {
		t.Errorf("validator dipanggil %d kali, harapannya 1", v.calls)
	}
}

func TestAuthMiddleware_ErrorBodyDoesNotLeakDetails(t *testing.T) {
	v := &fakeValidator{validToken: "good", userID: "u1"}
	var reached bool
	var uid string

	w := do(newRouter(v, &reached, &uid), "Bearer tok-rahasia-xyz")

	body := w.Body.String()
	if strings.Contains(body, "tok-rahasia-xyz") {
		t.Errorf("body tidak boleh memantulkan token: %s", body)
	}
	if strings.Contains(body, "token tidak valid\"") {
		t.Errorf("body tidak boleh membocorkan pesan error internal validator: %s", body)
	}
}

func TestAuthMiddleware_EmptySubjectIsNotAuthenticated(t *testing.T) {
	// Validator lolos tetapi mengembalikan userID kosong: UserIDFromContext harus false.
	v := &fakeValidator{validToken: "good", userID: ""}
	var reached bool
	var uid string

	_ = do(newRouter(v, &reached, &uid), "Bearer good")

	if uid != "" {
		t.Errorf("userID kosong tidak boleh dianggap terautentikasi, dapat %q", uid)
	}
}

func TestUserIDFromContext(t *testing.T) {
	t.Run("tidak ada", func(t *testing.T) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		if id, ok := middleware.UserIDFromContext(c); ok || id != "" {
			t.Errorf("harapannya (\"\", false), dapat (%q, %v)", id, ok)
		}
	})

	t.Run("tipe salah", func(t *testing.T) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Set("user_id", 12345)
		if id, ok := middleware.UserIDFromContext(c); ok || id != "" {
			t.Errorf("harapannya (\"\", false), dapat (%q, %v)", id, ok)
		}
	})

	t.Run("kosong", func(t *testing.T) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Set("user_id", "")
		if _, ok := middleware.UserIDFromContext(c); ok {
			t.Error("string kosong tidak boleh dianggap ada")
		}
	})
}
