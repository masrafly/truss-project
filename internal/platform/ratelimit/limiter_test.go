package ratelimit

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time          { return c.t }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

func newTest(limit int, window time.Duration) (*Limiter, *fakeClock) {
	c := &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	return New(limit, window).WithClock(c.now), c
}

func TestAllow_LimitPerKeyAndRetryAfter(t *testing.T) {
	l, c := newTest(3, time.Minute)
	for i := 1; i <= 3; i++ {
		if ok, _ := l.Allow("a"); !ok {
			t.Fatalf("percobaan %d harus diizinkan", i)
		}
	}
	c.advance(20 * time.Second)
	ok, retry := l.Allow("a")
	if ok {
		t.Fatal("percobaan ke-4 harus ditolak")
	}
	if retry != 40*time.Second {
		t.Errorf("retryAfter = %v, harapannya 40s", retry)
	}
	if ok, _ := l.Allow("b"); !ok {
		t.Error("key lain tidak boleh terpengaruh")
	}
}

func TestAllow_WindowResets(t *testing.T) {
	l, c := newTest(1, time.Minute)
	l.Allow("a")
	if ok, _ := l.Allow("a"); ok {
		t.Fatal("harus ditolak dalam jendela yang sama")
	}
	c.advance(time.Minute)
	if ok, _ := l.Allow("a"); !ok {
		t.Error("jendela baru harus mengizinkan lagi")
	}
}

func TestBlockedAddReset(t *testing.T) {
	l, c := newTest(2, time.Minute)
	if b, _ := l.Blocked("a"); b {
		t.Fatal("belum ada kejadian")
	}
	l.Add("a")
	if b, _ := l.Blocked("a"); b {
		t.Fatal("1 dari 2 belum terblokir")
	}
	l.Add("a")
	b, retry := l.Blocked("a")
	if !b || retry != time.Minute {
		t.Fatalf("2 dari 2 harus terblokir: %v %v", b, retry)
	}
	// Blocked tidak boleh menambah hitungan.
	for i := 0; i < 5; i++ {
		l.Blocked("a")
	}
	l.Reset("a")
	if b, _ := l.Blocked("a"); b {
		t.Error("Reset harus menghapus hitungan")
	}
	l.Add("a")
	l.Add("a")
	c.advance(time.Minute)
	if b, _ := l.Blocked("a"); b {
		t.Error("blokir harus berakhir saat jendela berganti")
	}
}

func TestSweepRemovesExpiredKeys(t *testing.T) {
	l, c := newTest(5, time.Minute)
	for i := 0; i < 1000; i++ {
		l.Allow(fmt.Sprintf("ip-%d", i))
	}
	if l.size() != 1000 {
		t.Fatalf("size = %d", l.size())
	}
	c.advance(time.Minute)
	l.Allow("baru")
	if l.size() != 1 {
		t.Errorf("entri kedaluwarsa harus dibuang, size = %d", l.size())
	}
}

func TestConcurrentAllowNeverExceedsLimit(t *testing.T) {
	l := New(50, time.Hour)
	var wg sync.WaitGroup
	var mu sync.Mutex
	allowed := 0
	for i := 0; i < 500; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if ok, _ := l.Allow("x"); ok {
				mu.Lock()
				allowed++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if allowed != 50 {
		t.Errorf("tepat 50 harus diizinkan, dapat %d", allowed)
	}
}
