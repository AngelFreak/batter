package middleware

import (
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// rateLimiter is a fixed-window, per-key request counter. It is intended to
// throttle unauthenticated endpoints (login, refresh) against brute-force and
// credential-stuffing, not as a general-purpose QoS limiter.
type rateLimiter struct {
	mu       sync.Mutex
	counts   map[string]*windowCount
	limit    int
	window   time.Duration
	lastSeen time.Time // used to amortize pruning of stale keys
}

type windowCount struct {
	count       int
	windowStart time.Time
}

func newRateLimiter(limit int, window time.Duration) *rateLimiter {
	return &rateLimiter{
		counts: make(map[string]*windowCount),
		limit:  limit,
		window: window,
	}
}

// allow reports whether a request from key is permitted right now, and if not,
// how long the caller should wait before retrying.
func (rl *rateLimiter) allow(key string, now time.Time) (bool, time.Duration) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	// Amortized prune: at most once per window, drop entries whose window has
	// fully elapsed so the map can't grow unboundedly from one-off IPs.
	if now.Sub(rl.lastSeen) >= rl.window {
		for k, wc := range rl.counts {
			if now.Sub(wc.windowStart) >= rl.window {
				delete(rl.counts, k)
			}
		}
		rl.lastSeen = now
	}

	wc, ok := rl.counts[key]
	if !ok || now.Sub(wc.windowStart) >= rl.window {
		rl.counts[key] = &windowCount{count: 1, windowStart: now}
		return true, 0
	}

	if wc.count >= rl.limit {
		return false, rl.window - now.Sub(wc.windowStart)
	}
	wc.count++
	return true, 0
}

// RateLimit returns middleware that allows at most `limit` requests per `window`
// from a single client IP. Exceeding the limit yields 429 with a Retry-After
// header. State is in-memory and per-process (sufficient for the single-binary
// deployment; revisit if Batter is ever horizontally scaled).
func RateLimit(limit int, window time.Duration) gin.HandlerFunc {
	rl := newRateLimiter(limit, window)

	return func(c *gin.Context) {
		ok, retryAfter := rl.allow(c.ClientIP(), time.Now())
		if !ok {
			seconds := int(retryAfter.Seconds()) + 1
			c.Header("Retry-After", itoa(seconds))
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error": "too many requests, please try again later",
			})
			return
		}
		c.Next()
	}
}

// itoa avoids pulling in strconv for a single small positive int.
func itoa(n int) string {
	if n <= 0 {
		return "1"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
