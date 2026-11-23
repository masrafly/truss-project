package security

import (
	"errors"
	"strings"
	"testing"
)

func TestRefreshTokenCodec_NewIsRandomAndHashMatches(t *testing.T) {
	c := NewRefreshTokenCodec()
	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		plain, hash, err := c.New()
		if err != nil {
			t.Fatal(err)
		}
		if len(plain) != 43 { // 32 byte base64url tanpa padding
			t.Fatalf("panjang token = %d, harapannya 43", len(plain))
		}
		if len(hash) != 64 || hash == plain || hash != c.Hash(plain) {
			t.Fatalf("hash tidak konsisten: %q", hash)
		}
		if seen[plain] {
			t.Fatal("token duplikat")
		}
		seen[plain] = true
	}
}

func TestRefreshTokenCodec_HashIsDeterministicAndDistinct(t *testing.T) {
	c := NewRefreshTokenCodec()
	if c.Hash("a") != c.Hash("a") || c.Hash("a") == c.Hash("b") {
		t.Error("hash harus deterministik dan berbeda untuk input berbeda")
	}
}

type failReader struct{}

func (failReader) Read([]byte) (int, error) { return 0, errors.New("rng mati") }

func TestRefreshTokenCodec_NewFailsWhenRandomSourceFails(t *testing.T) {
	c := &RefreshTokenCodec{rand: failReader{}}
	plain, hash, err := c.New()
	if err == nil || plain != "" || hash != "" || !strings.Contains(err.Error(), "refresh token") {
		t.Fatalf("plain=%q hash=%q err=%v", plain, hash, err)
	}
}
