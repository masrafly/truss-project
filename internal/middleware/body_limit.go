package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// DefaultMaxBodyBytes cukup untuk semua payload API (komentar terpanjang 5000 karakter
// ≈ 20 KB UTF-8) dengan ruang lebar, tetapi mencegah body raksasa dibaca ke memori.
const DefaultMaxBodyBytes int64 = 1 << 20 // 1 MiB

// BodyLimit membatasi ukuran body request.
//   - Content-Length melebihi batas -> langsung 413 tanpa membaca body.
//   - Tanpa Content-Length (chunked) -> pembacaan dihentikan oleh http.MaxBytesReader
//     setelah batas, sehingga parsing JSON gagal (400) tanpa menghabiskan memori.
func BodyLimit(maxBytes int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.ContentLength > maxBytes {
			c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, gin.H{
				"message": "Ukuran request terlalu besar",
			})
			return
		}
		if c.Request.Body != nil {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes)
		}
		c.Next()
	}
}
