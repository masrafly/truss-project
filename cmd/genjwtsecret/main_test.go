package main

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"regexp"
	"testing"

	"truss-project/internal/config"
)

var safeChars = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func TestGenerate_LengthAndCharset(t *testing.T) {
	s, err := generate(rand.Reader, defaultBytes)
	if err != nil {
		t.Fatal(err)
	}
	if len(s) != 64 {
		t.Errorf("48 byte harus menjadi 64 karakter, dapat %d", len(s))
	}
	if len(s) < config.MinJWTSecretLength {
		t.Errorf("hasil harus lolos validasi config (>= %d), dapat %d", config.MinJWTSecretLength, len(s))
	}
	if !safeChars.MatchString(s) {
		t.Errorf("hanya karakter aman untuk .env yang boleh muncul: %q", s)
	}
}

func TestGenerate_EncodesExactlyTheRandomBytes(t *testing.T) {
	src := bytes.Repeat([]byte{0xfb, 0xff}, 16) // byte yang di base64 biasa menjadi '+' dan '/'
	s, err := generate(bytes.NewReader(src), 32)
	if err != nil {
		t.Fatal(err)
	}
	got, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil || !bytes.Equal(got, src) {
		t.Fatalf("decode harus mengembalikan byte asli: %v", err)
	}
	if !safeChars.MatchString(s) {
		t.Errorf("tidak boleh ada '+', '/', atau '=': %q", s)
	}
}

func TestGenerate_Unique(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		s, err := generate(rand.Reader, 32)
		if err != nil {
			t.Fatal(err)
		}
		if seen[s] {
			t.Fatal("secret berulang: sumber acak tidak benar")
		}
		seen[s] = true
	}
}

func TestGenerate_RejectsTooFewBytes(t *testing.T) {
	if _, err := generate(rand.Reader, config.MinJWTSecretLength-1); err == nil {
		t.Error("kurang dari minimal harus ditolak")
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("sumber acak rusak") }

func TestGenerate_PropagatesReaderError(t *testing.T) {
	if _, err := generate(failingReader{}, 32); err == nil {
		t.Error("kegagalan sumber acak tidak boleh menghasilkan secret")
	}
	// Bacaan sebagian (sumber habis) juga harus gagal, bukan secret yang lebih lemah.
	if _, err := generate(bytes.NewReader(make([]byte, 10)), 32); err == nil {
		t.Error("bacaan sebagian harus gagal")
	}
}
