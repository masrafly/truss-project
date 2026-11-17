// Package ratelimit menyediakan pembatas laju in-memory (fixed window per key).
//
// Batasan: state disimpan di memori proses, sehingga batas berlaku PER INSTANCE
// aplikasi dan hilang saat restart. Untuk banyak instance, ganti implementasinya
// dengan penyimpanan bersama (mis. Redis) tanpa mengubah pemakainya.
package ratelimit

import (
	"sync"
	"time"
)

type entry struct {
	count   int
	resetAt time.Time
}

// Limiter mengizinkan paling banyak `limit` kejadian per key dalam satu `window`.
type Limiter struct {
	mu        sync.Mutex
	limit     int
	window    time.Duration
	entries   map[string]*entry
	now       func() time.Time
	lastSweep time.Time
}

func New(limit int, window time.Duration) *Limiter {
	if limit < 1 {
		limit = 1
	}
	return &Limiter{limit: limit, window: window, entries: map[string]*entry{}, now: time.Now}
}

// WithClock mengganti sumber waktu (untuk test).
func (l *Limiter) WithClock(now func() time.Time) *Limiter {
	l.now = now
	return l
}

// current mengembalikan entri aktif untuk key (membuat baru bila belum ada atau
// jendelanya sudah lewat). Harus dipanggil dengan mu terkunci.
func (l *Limiter) current(key string, now time.Time) *entry {
	l.sweep(now)
	e, ok := l.entries[key]
	if !ok || !now.Before(e.resetAt) {
		e = &entry{resetAt: now.Add(l.window)}
		l.entries[key] = e
	}
	return e
}

// sweep membuang entri kedaluwarsa paling sering sekali per window, agar memori
// tidak tumbuh tanpa batas oleh key unik (mis. banyak IP/email berbeda).
func (l *Limiter) sweep(now time.Time) {
	if now.Sub(l.lastSweep) < l.window {
		return
	}
	for k, e := range l.entries {
		if !now.Before(e.resetAt) {
			delete(l.entries, k)
		}
	}
	l.lastSweep = now
}

// Allow mencatat satu kejadian dan melaporkan apakah masih dalam batas. Bila tidak,
// retryAfter adalah sisa waktu sampai jendela berganti.
func (l *Limiter) Allow(key string) (ok bool, retryAfter time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	e := l.current(key, now)
	e.count++
	if e.count > l.limit {
		return false, e.resetAt.Sub(now)
	}
	return true, 0
}

// Blocked melaporkan apakah key sudah mencapai batas, TANPA mencatat kejadian baru.
func (l *Limiter) Blocked(key string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	e := l.current(key, now)
	if e.count >= l.limit {
		return true, e.resetAt.Sub(now)
	}
	return false, 0
}

// Add mencatat satu kejadian tanpa memeriksa batas.
func (l *Limiter) Add(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.current(key, l.now()).count++
}

// Reset menghapus hitungan key (mis. setelah login berhasil).
func (l *Limiter) Reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.entries, key)
}

// size: jumlah key yang sedang dilacak (untuk test).
func (l *Limiter) size() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.entries)
}
