package common

import (
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// visitor is a single client's token bucket. tokens refills continuously at the
// limiter's rate and is capped at burst; each request costs one token.
type visitor struct {
	tokens   float64
	lastSeen time.Time
}

// RateLimiter is a tiny in-memory, per-IP token-bucket limiter. It has no
// external dependencies and is good enough to blunt brute-force and abuse on
// sensitive endpoints (login/register/refresh). For a multi-instance
// deployment this would move to a shared store (e.g. Redis); for this capstone
// a single Render instance makes in-memory correct and simple.
type RateLimiter struct {
	mu       sync.Mutex
	visitors map[string]*visitor
	rate     float64       // tokens added per second
	burst    float64       // maximum burst / bucket size
	ttl      time.Duration // idle visitors are evicted after this
}

// NewRateLimiter builds a limiter allowing `rate` requests per second with room
// to burst up to `burst` requests. A background goroutine evicts idle visitors.
func NewRateLimiter(rate float64, burst int) *RateLimiter {
	rl := &RateLimiter{
		visitors: make(map[string]*visitor),
		rate:     rate,
		burst:    float64(burst),
		ttl:      10 * time.Minute,
	}
	go rl.cleanupLoop()
	return rl
}

// allow reports whether the given key may proceed, consuming a token if so.
func (rl *RateLimiter) allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	v, ok := rl.visitors[key]
	if !ok {
		// First request from this key starts with a full bucket minus this one.
		rl.visitors[key] = &visitor{tokens: rl.burst - 1, lastSeen: now}
		return true
	}

	// Refill based on elapsed time, capped at burst.
	elapsed := now.Sub(v.lastSeen).Seconds()
	v.tokens = min(rl.burst, v.tokens+elapsed*rl.rate)
	v.lastSeen = now

	if v.tokens < 1 {
		return false
	}
	v.tokens--
	return true
}

func (rl *RateLimiter) cleanupLoop() {
	ticker := time.NewTicker(rl.ttl)
	defer ticker.Stop()
	for range ticker.C {
		cutoff := time.Now().Add(-rl.ttl)
		rl.mu.Lock()
		for key, v := range rl.visitors {
			if v.lastSeen.Before(cutoff) {
				delete(rl.visitors, key)
			}
		}
		rl.mu.Unlock()
	}
}

// Middleware returns an http middleware enforcing the limiter, keyed by client
// IP. It relies on chi's RealIP middleware having normalised RemoteAddr from
// X-Forwarded-For, so behind Render's proxy the real client IP is limited
// rather than the proxy's.
func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !rl.allow(ClientIP(r)) {
			// Suggest a back-off roughly equal to one token's worth of time.
			retry := int(1/rl.rate) + 1
			w.Header().Set("Retry-After", strconv.Itoa(retry))
			Error(w, ErrTooManyRequests("too many requests, please slow down"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ClientIP extracts a best-effort client IP, honouring X-Forwarded-For. Shared
// by the rate limiter and by session records so both agree on "who" a caller is.
func ClientIP(r *http.Request) string {
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		return strings.TrimSpace(strings.Split(fwd, ",")[0])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
