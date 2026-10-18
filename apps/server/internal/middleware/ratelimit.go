package middleware

import (
	"net"
	"net/http"
	"net/netip"
	"sync"
	"time"

	"servify/apps/server/internal/config"

	"github.com/gin-gonic/gin"
	appmetrics "servify/apps/server/internal/metrics"
)

// tokenBucket is a simple leaky bucket (token bucket) implementation for rate limiting.
type tokenBucket struct {
	mu         sync.Mutex
	tokens     float64
	lastRefill time.Time
	lastSeen   time.Time
	ratePerSec float64 // tokens per second
	burst      float64
}

const (
	rateLimitMaxBuckets  = 4096
	rateLimitIdleTTL     = 10 * time.Minute
	rateLimitSweepPeriod = time.Minute
)

type rateLimiter struct {
	mu          sync.Mutex
	buckets     map[string]*tokenBucket
	overflow    *tokenBucket
	nextCleanup time.Time
	cfg         config.PathRateLimitConfig
}

func newRateLimiter(cfg config.PathRateLimitConfig) rateLimiter {
	return rateLimiter{buckets: make(map[string]*tokenBucket), cfg: cfg}
}

// get reclaims idle buckets and bounds memory use. Once the per-limiter cap is
// reached, new identities share an overflow bucket rather than allocating more.
func (l *rateLimiter) get(key string) *tokenBucket {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	if !now.Before(l.nextCleanup) {
		cutoff := now.Add(-rateLimitIdleTTL)
		for bucketKey, bucket := range l.buckets {
			if bucket.idleBefore(cutoff) {
				delete(l.buckets, bucketKey)
			}
		}
		l.nextCleanup = now.Add(rateLimitSweepPeriod)
	}
	if b, ok := l.buckets[key]; ok {
		return b
	}
	if len(l.buckets) >= rateLimitMaxBuckets {
		if l.overflow == nil {
			l.overflow = newBucket(l.cfg.RequestsPerMinute, l.cfg.Burst)
		}
		return l.overflow
	}
	b := newBucket(l.cfg.RequestsPerMinute, l.cfg.Burst)
	l.buckets[key] = b
	return b
}

func newBucket(rpm, burst int) *tokenBucket {
	if rpm <= 0 {
		rpm = 60
	}
	if burst <= 0 {
		burst = rpm // default burst equals a minute worth
	}
	return &tokenBucket{
		tokens:     float64(burst),
		lastRefill: time.Now(),
		lastSeen:   time.Now(),
		ratePerSec: float64(rpm) / 60.0,
		burst:      float64(burst),
	}
}

func (b *tokenBucket) allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	b.lastSeen = now
	elapsed := now.Sub(b.lastRefill).Seconds()
	if elapsed > 0 {
		b.tokens += elapsed * b.ratePerSec
		if b.tokens > b.burst {
			b.tokens = b.burst
		}
		b.lastRefill = now
	}
	if b.tokens >= 1 {
		b.tokens -= 1
		return true
	}
	return false
}

func (b *tokenBucket) idleBefore(cutoff time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.lastSeen.Before(cutoff)
}

// remotePeerIP intentionally uses the socket peer, not forwarded headers:
// this router does not configure a trusted-proxy boundary for client IPs.
func remotePeerIP(c *gin.Context) string {
	addr := c.Request.RemoteAddr
	host, _, err := net.SplitHostPort(addr)
	if err == nil {
		addr = host
	}
	if ip, err := netip.ParseAddr(addr); err == nil {
		return ip.Unmap().String()
	}
	return "unknown"
}

// RateLimitMiddlewareFromConfig selects per-path limits if configured, otherwise falls back to global.
// Matching is done by the first Paths entry whose Prefix matches the request path prefix.
func RateLimitMiddlewareFromConfig(cfg *config.Config) gin.HandlerFunc {
	rl := cfg.Security.RateLimiting
	if !rl.Enabled {
		return func(c *gin.Context) { c.Next() }
	}
	inStrings := func(needle string, hay []string) bool {
		for _, s := range hay {
			if needle == s {
				return true
			}
		}
		return false
	}
	var (
		anyPathOverrides = len(rl.Paths) > 0
		pathLimiters     []rateLimiter
		globalLimiter    rateLimiter
	)
	if anyPathOverrides {
		for _, p := range rl.Paths {
			if !p.Enabled || p.RequestsPerMinute <= 0 {
				continue
			}
			pathLimiters = append(pathLimiters, newRateLimiter(p))
		}
	}
	// global
	if rl.RequestsPerMinute > 0 {
		globalLimiter = newRateLimiter(config.PathRateLimitConfig{
			Enabled:           true,
			Prefix:            "",
			RequestsPerMinute: rl.RequestsPerMinute,
			Burst:             rl.Burst,
		})
	}
	// rl 是构造时 cfg.Security.RateLimiting 的值拷贝，构造处（本函数开头）
	// 已拒绝 !rl.Enabled，拷贝不会再变化，故 handler 内无需重复检查。
	return func(c *gin.Context) {
		key := remotePeerIP(c)
		// Only the socket peer can be whitelisted; request headers are untrusted.
		if key != "unknown" && inStrings(key, rl.WhitelistIPs) {
			c.Next()
			return
		}
		// per-path first
		if len(pathLimiters) > 0 {
			path := c.FullPath()
			if path == "" {
				path = c.Request.URL.Path
			}
			for i := range pathLimiters {
				pl := &pathLimiters[i]
				if pl.cfg.Prefix != "" && len(path) >= len(pl.cfg.Prefix) && path[:len(pl.cfg.Prefix)] == pl.cfg.Prefix {
					if !pl.get(key).allow() {
						appmetrics.IncRateLimitDrop(pl.cfg.Prefix)
						c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
							"error":   "Too Many Requests",
							"message": "rate limit exceeded (path)",
						})
						return
					}
					c.Next()
					return
				}
			}
		}
		// fallback to global
		if globalLimiter.cfg.Enabled && globalLimiter.cfg.RequestsPerMinute > 0 {
			if !globalLimiter.get(key).allow() {
				appmetrics.IncRateLimitDrop("global")
				c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
					"error":   "Too Many Requests",
					"message": "rate limit exceeded",
				})
				return
			}
		}
		c.Next()
	}
}
