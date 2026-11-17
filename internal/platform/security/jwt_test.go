package security_test

import (
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"truss-project/internal/platform/security"
)

const (
	testSecret = "test-secret-yang-cukup-panjang-untuk-hs256"
	testUserID = "3f2b8c5e-9d1a-4c7e-8a55-0d5e9f1b7a11"
)

func newManager(t *testing.T, ttl time.Duration) *security.JWTManager {
	t.Helper()
	m, err := security.NewJWTManager(testSecret, ttl)
	if err != nil {
		t.Fatalf("NewJWTManager: %v", err)
	}
	return m
}

// sign membuat token dengan claims dan metode tertentu (untuk skenario serangan).
func sign(t *testing.T, method jwt.SigningMethod, key any, claims jwt.RegisteredClaims) string {
	t.Helper()
	s, err := jwt.NewWithClaims(method, claims).SignedString(key)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return s
}

func validClaims() jwt.RegisteredClaims {
	now := time.Now()
	return jwt.RegisteredClaims{
		Subject:   testUserID,
		Issuer:    "truss",
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
	}
}

func TestNewJWTManager_EmptySecretRejected(t *testing.T) {
	if _, err := security.NewJWTManager("", time.Hour); err == nil {
		t.Fatal("secret kosong harus ditolak")
	}
}

func TestIssue_ReturnsTokenAndTTL(t *testing.T) {
	m := newManager(t, 2*time.Hour)

	token, ttl, err := m.Issue(testUserID)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if token == "" {
		t.Fatal("token tidak boleh kosong")
	}
	if ttl != 2*time.Hour {
		t.Errorf("ttl = %v, harapannya 2h", ttl)
	}
	if strings.Count(token, ".") != 2 {
		t.Errorf("token JWT harus 3 bagian, dapat %q", token)
	}
}

func TestIssue_ClaimsContent(t *testing.T) {
	m := newManager(t, time.Hour)
	before := time.Now().Add(-time.Second)

	token, _, err := m.Issue(testUserID)
	if err != nil {
		t.Fatal(err)
	}

	claims := &jwt.RegisteredClaims{}
	parsed, err := jwt.ParseWithClaims(token, claims, func(*jwt.Token) (any, error) {
		return []byte(testSecret), nil
	})
	if err != nil || !parsed.Valid {
		t.Fatalf("token harus bisa di-parse: %v", err)
	}

	if parsed.Method.Alg() != "HS256" {
		t.Errorf("algoritma = %s, harapannya HS256", parsed.Method.Alg())
	}
	if claims.Subject != testUserID {
		t.Errorf("subject = %q", claims.Subject)
	}
	if claims.Issuer != "truss" {
		t.Errorf("issuer = %q", claims.Issuer)
	}
	if claims.IssuedAt == nil || claims.IssuedAt.Before(before) {
		t.Errorf("iat tidak wajar: %v", claims.IssuedAt)
	}
	if claims.ExpiresAt == nil {
		t.Fatal("exp wajib ada")
	}
	if d := claims.ExpiresAt.Sub(claims.IssuedAt.Time); d < 59*time.Minute || d > 61*time.Minute {
		t.Errorf("exp - iat = %v, harapannya sekitar 1 jam", d)
	}
}

func TestIssue_DoesNotContainSensitiveClaims(t *testing.T) {
	m := newManager(t, time.Hour)
	token, _, _ := m.Issue(testUserID)

	// Payload JWT hanya di-encode (bukan dienkripsi). Pastikan hanya claim standar.
	claims := jwt.MapClaims{}
	_, _, err := jwt.NewParser().ParseUnverified(token, claims)
	if err != nil {
		t.Fatal(err)
	}
	allowed := map[string]bool{"sub": true, "iss": true, "iat": true, "exp": true}
	for k := range claims {
		if !allowed[k] {
			t.Errorf("claim tak terduga di token: %q", k)
		}
	}
}

func TestValidate_RoundTrip(t *testing.T) {
	m := newManager(t, time.Hour)

	token, _, err := m.Issue(testUserID)
	if err != nil {
		t.Fatal(err)
	}

	got, err := m.Validate(token)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if got != testUserID {
		t.Errorf("userID = %q, harapannya %q", got, testUserID)
	}
}

func TestValidate_Rejected(t *testing.T) {
	m := newManager(t, time.Hour)
	secret := []byte(testSecret)

	expired := validClaims()
	expired.IssuedAt = jwt.NewNumericDate(time.Now().Add(-2 * time.Hour))
	expired.ExpiresAt = jwt.NewNumericDate(time.Now().Add(-time.Hour))

	wrongIssuer := validClaims()
	wrongIssuer.Issuer = "penyerang"

	noIssuer := validClaims()
	noIssuer.Issuer = ""

	noExp := validClaims()
	noExp.ExpiresAt = nil

	noSubject := validClaims()
	noSubject.Subject = ""

	notYetValid := validClaims()
	notYetValid.NotBefore = jwt.NewNumericDate(time.Now().Add(time.Hour))

	valid, _, _ := m.Issue(testUserID)
	parts := strings.Split(valid, ".")
	tamperedSig := parts[0] + "." + parts[1] + "." + flipFirstChar(parts[2])

	// Payload diganti (subject user lain) dengan signature lama.
	otherPayload := sign(t, jwt.SigningMethodHS256, secret, func() jwt.RegisteredClaims {
		c := validClaims()
		c.Subject = "user-lain"
		return c
	}())
	forgedParts := strings.Split(otherPayload, ".")
	forgedPayload := parts[0] + "." + forgedParts[1] + "." + parts[2]

	tests := []struct {
		name  string
		token string
	}{
		{"string kosong", ""},
		{"bukan JWT", "ini-bukan-jwt"},
		{"dua bagian saja", "aaa.bbb"},
		{"secret berbeda", sign(t, jwt.SigningMethodHS256, []byte("secret-lain"), validClaims())},
		{"kedaluwarsa", sign(t, jwt.SigningMethodHS256, secret, expired)},
		{"issuer salah", sign(t, jwt.SigningMethodHS256, secret, wrongIssuer)},
		{"issuer kosong", sign(t, jwt.SigningMethodHS256, secret, noIssuer)},
		{"tanpa exp", sign(t, jwt.SigningMethodHS256, secret, noExp)},
		{"tanpa subject", sign(t, jwt.SigningMethodHS256, secret, noSubject)},
		{"belum berlaku (nbf)", sign(t, jwt.SigningMethodHS256, secret, notYetValid)},
		{"signature diubah", tamperedSig},
		{"payload diganti, signature lama", forgedPayload},
		{"algoritma HS512", sign(t, jwt.SigningMethodHS512, secret, validClaims())},
		{"algoritma none", sign(t, jwt.SigningMethodNone, jwt.UnsafeAllowNoneSignatureType, validClaims())},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			id, err := m.Validate(tc.token)
			if err == nil {
				t.Fatalf("token harus ditolak, tetapi lolos dengan userID %q", id)
			}
			if id != "" {
				t.Errorf("userID harus kosong saat gagal, dapat %q", id)
			}
		})
	}
}

func TestValidate_TokenFromAnotherManagerSecret(t *testing.T) {
	a := newManager(t, time.Hour)
	b, err := security.NewJWTManager("secret-manager-lain-yang-panjang", time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	token, _, _ := a.Issue(testUserID)
	if _, err := b.Validate(token); err == nil {
		t.Fatal("token dari secret lain harus ditolak")
	}
}

func TestValidate_ExpiryIsEnforcedFromIssueTTL(t *testing.T) {
	m := newManager(t, time.Second)

	token, _, err := m.Issue(testUserID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Validate(token); err != nil {
		t.Fatalf("token baru harus valid: %v", err)
	}

	// jwt/v5 membandingkan dalam detik; tunggu hingga lewat.
	time.Sleep(2100 * time.Millisecond)
	if _, err := m.Validate(token); err == nil {
		t.Fatal("token harus ditolak setelah TTL lewat")
	}
}

// flipFirstChar mengganti karakter PERTAMA signature agar pasti berbeda dari aslinya.
// Jangan memakai karakter terakhir: signature HS256 (32 byte) menjadi 43 karakter base64
// dan karakter terakhir hanya membawa 4 bit bermakna, sehingga mengganti karakter itu
// kadang menghasilkan byte signature yang sama persis (test menjadi acak/flaky).
func flipFirstChar(s string) string {
	if s == "" {
		return "x"
	}
	repl := byte('A')
	if s[0] == 'A' {
		repl = 'B'
	}
	return string(repl) + s[1:]
}
