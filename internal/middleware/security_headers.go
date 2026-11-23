package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"
)

// SecurityHeaders menambahkan header pengerasan standar ke SETIAP respons (termasuk error
// dan panic yang dipulihkan, karena dipasang sebelum handler lain).
//
// API ini hanya mengembalikan JSON, sehingga kebijakannya ketat:
//   - nosniff: browser tidak menebak tipe konten.
//   - CSP default-src 'none' + frame-ancestors 'none' dan X-Frame-Options: respons tidak dapat
//     dimuat sebagai halaman/ditanam dalam frame (mencegah clickjacking).
//   - Referrer-Policy no-referrer.
//   - Cache-Control no-store: respons berisi data privat dan token tidak boleh disimpan cache
//     browser maupun proxy perantara.
//   - HSTS hanya pada koneksi HTTPS (langsung atau lewat proxy yang mengirim
//     X-Forwarded-Proto: https); browser mengabaikannya pada HTTP biasa.
func SecurityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.Writer.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cache-Control", "no-store")
		if c.Request.TLS != nil || strings.EqualFold(c.GetHeader("X-Forwarded-Proto"), "https") {
			h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		c.Next()
	}
}
