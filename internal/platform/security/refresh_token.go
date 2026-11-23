package security

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
)

// refreshTokenBytes: 32 byte acak (256 bit). Entropi setinggi ini membuat hash cepat
// (SHA-256) aman untuk penyimpanan; bcrypt hanya diperlukan untuk secret berentropi rendah.
const refreshTokenBytes = 32

// RefreshTokenCodec membuat refresh token opaque dan menghitung hash untuk disimpan.
type RefreshTokenCodec struct {
	rand io.Reader
}

func NewRefreshTokenCodec() *RefreshTokenCodec { return &RefreshTokenCodec{rand: rand.Reader} }

// New mengembalikan token asli (dikirim ke client sekali) dan hash-nya (disimpan).
func (c *RefreshTokenCodec) New() (plain, hash string, err error) {
	b := make([]byte, refreshTokenBytes)
	if _, err := io.ReadFull(c.rand, b); err != nil {
		return "", "", fmt.Errorf("gagal membuat refresh token: %w", err)
	}
	plain = base64.RawURLEncoding.EncodeToString(b)
	return plain, c.Hash(plain), nil
}

// Hash: SHA-256 heksadesimal (64 karakter), sesuai kolom token_hash CHAR(64).
func (*RefreshTokenCodec) Hash(plain string) string {
	sum := sha256.Sum256([]byte(plain))
	return hex.EncodeToString(sum[:])
}
