package middleware

import (
	"context"
	"math"
	"sync"
	"time"

	"nekopic/internal/config"
)

type client struct {
	times        []time.Time
	blockedUntil time.Time
	lastSeen     time.Time
}

// Limiter 记录每个访客 IP 最近 10 秒内的请求次数，超过上限就临时封禁一段时间。
// 封禁期间重复请求只返回剩余等待时间，不会延长封禁。
type Limiter struct {
	mu      sync.Mutex
	clients map[string]*client
	cfg     config.RateLimit
	now     func() time.Time
}

type Decision struct {
	Status     int
	RetryAfter int
}

func NewLimiter(cfg config.RateLimit) *Limiter {
	return &Limiter{clients: make(map[string]*client), cfg: cfg, now: time.Now}
}

func (l *Limiter) Allow(ip string) Decision {
	if !l.cfg.Enabled {
		return Decision{}
	}

	// 清理、判断与写入必须在同一临界区内，避免并发请求超额放行。
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	c := l.clients[ip]
	if c == nil {
		if len(l.clients) >= l.cfg.MaxClients {
			l.cleanup(now)
		}
		if len(l.clients) >= l.cfg.MaxClients {
			return Decision{Status: 503, RetryAfter: 1}
		}
		c = &client{}
		l.clients[ip] = c
	}

	c.lastSeen = now
	if now.Before(c.blockedUntil) {
		// 封禁期间只返回剩余时间，不延长封禁。
		return Decision{Status: 429, RetryAfter: max(1, int(math.Ceil(c.blockedUntil.Sub(now).Seconds())))}
	}
	if !c.blockedUntil.IsZero() {
		c.times = c.times[:0]
		c.blockedUntil = time.Time{}
	}

	cutoff := now.Add(-10 * time.Second)
	n := 0
	for n < len(c.times) && !c.times[n].After(cutoff) {
		n++
	}
	copy(c.times, c.times[n:])
	c.times = c.times[:len(c.times)-n]

	if len(c.times) >= l.cfg.RequestsPer10s {
		c.blockedUntil = now.Add(time.Duration(l.cfg.BlockDuration) * time.Second)
		return Decision{Status: 429, RetryAfter: l.cfg.BlockDuration}
	}

	c.times = append(c.times, now)
	return Decision{}
}

func (l *Limiter) cleanup(now time.Time) {
	for ip, c := range l.clients {
		if !now.Before(c.blockedUntil) && now.Sub(c.lastSeen) >= l.cfg.IdleTTL {
			delete(l.clients, ip)
		}
	}
}

func (l *Limiter) Run(ctx context.Context) {
	if !l.cfg.Enabled {
		return
	}

	ticker := time.NewTicker(l.cfg.CleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			l.mu.Lock()
			l.cleanup(l.now())
			l.mu.Unlock()
		}
	}
}
