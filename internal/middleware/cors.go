package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

const (
	corsAllowMethods = "GET, POST, PATCH, DELETE, OPTIONS"
	corsAllowHeaders = "Authorization, Content-Type, X-Request-ID"
	corsExpose       = "X-Request-ID, Retry-After"
	corsMaxAge       = "600"
)

// CORS mengizinkan browser dari origin yang terdaftar (persis, tanpa wildcard) memanggil API.
//
//   - allowed kosong: middleware tidak melakukan apa pun (hanya same-origin).
//   - Origin terdaftar: header Access-Control-Allow-Origin diisi dengan origin itu; preflight
//     dijawab 204 tanpa meneruskan ke handler (jadi tidak butuh Authorization).
//   - Origin tidak terdaftar: tidak ada header CORS, sehingga browser memblokir pembacaan
//     respons; preflight dijawab 403.
//
// Tidak memakai Access-Control-Allow-Credentials: autentikasi memakai header Authorization,
// bukan cookie. Vary: Origin selalu ditambahkan agar cache tidak mencampur respons antar origin.
func CORS(allowed []string) gin.HandlerFunc {
	if len(allowed) == 0 {
		return func(c *gin.Context) { c.Next() }
	}
	set := make(map[string]bool, len(allowed))
	for _, o := range allowed {
		set[o] = true
	}

	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if origin == "" {
			c.Next()
			return
		}

		h := c.Writer.Header()
		h.Add("Vary", "Origin")
		preflight := c.Request.Method == http.MethodOptions && c.GetHeader("Access-Control-Request-Method") != ""
		if preflight {
			h.Add("Vary", "Access-Control-Request-Method")
			h.Add("Vary", "Access-Control-Request-Headers")
		}

		if !set[origin] {
			if preflight {
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"message": "Origin tidak diizinkan"})
				return
			}
			c.Next()
			return
		}

		h.Set("Access-Control-Allow-Origin", origin)
		h.Set("Access-Control-Expose-Headers", corsExpose)
		if preflight {
			h.Set("Access-Control-Allow-Methods", corsAllowMethods)
			h.Set("Access-Control-Allow-Headers", corsAllowHeaders)
			h.Set("Access-Control-Max-Age", corsMaxAge)
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}
