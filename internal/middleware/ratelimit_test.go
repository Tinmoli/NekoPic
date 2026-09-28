package middleware

import (
	"context"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"nekopic/internal/config"
)

func TestWindowAndBlockBoundaries(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	l := NewLimiter(config.Defaults().RateLimit)
	l.now = func() time.Time { return now }
	for i := 0; i < 20; i++ {
		if d := l.Allow("a"); d.Status != 0 {
			t.Fatalf("request %d blocked", i+1)
		}
	}
	if d := l.Allow("b"); d.Status != 0 {
		t.Fatal("different IP shares quota")
	}
	if d := l.Allow("a"); d.Status != 429 || d.RetryAfter != 30 {
		t.Fatalf("21st request: %+v", d)
	}
	now = now.Add(29001 * time.Millisecond)
	if d := l.Allow("a"); d.Status != 429 || d.RetryAfter != 1 {
		t.Fatalf("retry-after: %+v", d)
	}
	now = now.Add(999 * time.Millisecond)
	if d := l.Allow("a"); d.Status != 0 {
		t.Fatalf("block was extended: %+v", d)
	}
	for i := 1; i < 20; i++ {
		l.Allow("a")
	}
	now = now.Add(10 * time.Second)
	if d := l.Allow("a"); d.Status != 0 {
		t.Fatalf("timestamp at exact cutoff retained: %+v", d)
	}
}

func TestConcurrentQuota(t *testing.T) {
	l := NewLimiter(config.Defaults().RateLimit)
	now := time.Now()
	l.now = func() time.Time { return now }
	var allowed atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Go(func() {
			if l.Allow("same").Status == 0 {
				allowed.Add(1)
			}
		})
	}
	wg.Wait()
	if allowed.Load() != 20 {
		t.Fatalf("allowed %d concurrent requests", allowed.Load())
	}
}

func TestCapacityCleanupAndDisabled(t *testing.T) {
	cfg := config.Defaults().RateLimit
	cfg.MaxClients = 1
	l := NewLimiter(cfg)
	now := time.Now()
	l.now = func() time.Time { return now }
	l.Allow("a")
	if d := l.Allow("b"); d.Status != 503 || d.RetryAfter != 1 {
		t.Fatalf("capacity: %+v", d)
	}
	now = now.Add(cfg.IdleTTL)
	if d := l.Allow("b"); d.Status != 0 {
		t.Fatalf("expired client not cleaned: %+v", d)
	}
	cfg.Enabled = false
	disabled := NewLimiter(cfg)
	for i := 0; i < 100; i++ {
		if d := disabled.Allow("a"); d.Status != 0 {
			t.Fatal("disabled limiter blocked")
		}
	}
	if len(disabled.clients) != 0 {
		t.Fatal("disabled limiter allocates clients")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan struct{})
	go func() { l.Run(ctx); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cleanup task did not stop")
	}
}

func TestClientIP(t *testing.T) {
	for _, tc := range []struct {
		name, peer, header, want string
		trusted                  []string
	}{
		{"untrusted", "192.0.2.1:123", "198.51.100.1", "192.0.2.1", nil},
		{"mapped", "[::ffff:192.0.2.1]:123", "", "192.0.2.1", nil},
		{"trusted", "127.0.0.1:123", "198.51.100.1", "198.51.100.1", []string{"127.0.0.1/32"}},
		{"chain", "127.0.0.1:123", "1.1.1.1, 198.51.100.2, 10.1.2.3", "198.51.100.2", []string{"127.0.0.1/32", "10.0.0.0/8"}},
		{"malformed", "127.0.0.1:123", "invalid, 198.51.100.2", "127.0.0.1", []string{"127.0.0.1/32"}},
		{"empty", "127.0.0.1:123", "", "127.0.0.1", []string{"127.0.0.1/32"}},
		{"ipv6", "[::1]:123", "2001:db8::1", "2001:db8::1", []string{"::1/128"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resolver, err := NewIPResolver(tc.trusted)
			if err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest("GET", "/", nil)
			req.RemoteAddr = tc.peer
			req.Header.Set("X-Forwarded-For", tc.header)
			req.Header.Set("X-Real-IP", "203.0.113.1")
			if got := resolver.ClientIP(req); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}
