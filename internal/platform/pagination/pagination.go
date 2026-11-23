// Package pagination menyeragamkan pagination berbasis page/limit untuk semua endpoint daftar:
// ?page=N (mulai 1) dan ?limit=N, dengan batas atas agar satu request tidak dapat memuat
// seluruh tabel. Respons memuat "meta": {page, limit, total}.
package pagination

import (
	"errors"
	"strconv"

	"github.com/gin-gonic/gin"
)

// ErrInvalid: page/limit bukan angka, kurang dari 1, atau limit melebihi batas.
var ErrInvalid = errors.New("parameter pagination tidak valid")

// Params adalah hasil validasi yang siap dipakai repository.
type Params struct {
	Page   int
	Limit  int
	Offset int
}

// New memvalidasi page/limit. Nilai 0 berarti "tidak dikirim" -> page 1 dan defaultLimit.
func New(page, limit, defaultLimit, maxLimit int) (Params, error) {
	if page == 0 {
		page = 1
	}
	if limit == 0 {
		limit = defaultLimit
	}
	if page < 1 || limit < 1 || limit > maxLimit {
		return Params{}, ErrInvalid
	}
	// Mencegah luapan integer pada (page-1)*limit untuk page yang absurd besar.
	if page-1 > maxOffset/limit {
		return Params{}, ErrInvalid
	}
	return Params{Page: page, Limit: limit, Offset: (page - 1) * limit}, nil
}

// maxOffset: batas offset yang wajar (jauh di atas data nyata, jauh di bawah luapan int32).
const maxOffset = 1_000_000

// Result adalah satu halaman data beserta total seluruh data.
type Result[T any] struct {
	Items []T
	Total int64
	Page  int
	Limit int
}

// NewResult membuat Result; items nil diubah menjadi slice kosong agar JSON memakai [] bukan null.
func NewResult[T any](items []T, total int64, p Params) *Result[T] {
	if items == nil {
		items = []T{}
	}
	return &Result[T]{Items: items, Total: total, Page: p.Page, Limit: p.Limit}
}

// Meta untuk respons JSON.
func (r *Result[T]) Meta() gin.H {
	return gin.H{"page": r.Page, "limit": r.Limit, "total": r.Total}
}

// FromQuery membaca ?page dan ?limit. Parameter kosong -> 0 (default ditentukan use case);
// bukan angka -> ok=false (handler membalas 400).
func FromQuery(c *gin.Context) (page, limit int, ok bool) {
	page, okPage := queryInt(c, "page")
	limit, okLimit := queryInt(c, "limit")
	return page, limit, okPage && okLimit
}

func queryInt(c *gin.Context, key string) (int, bool) {
	s := c.Query(key)
	if s == "" {
		return 0, true
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, false
	}
	return n, true
}
