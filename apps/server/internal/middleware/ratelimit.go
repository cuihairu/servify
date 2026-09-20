package middleware

import (
	"net/http"
	"strings"
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
	ratePerSec float64 // tokens per second
	burst      float64
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
		ratePerSec: float64(rpm) / 60.0,
		burst:      float64(burst),
	}
}

func (b *tokenBucket) allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
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

// RateLimitMiddlewareFromConfig selects per-path limits if configured, otherwise falls back to global.
// Matching is done by the first Paths entry whose Prefix matches the request path prefix.
func RateLimitMiddlewareFromConfig(cfg *config.Config) gin.HandlerFunc {
	rl := cfg.Security.RateLimiting
	if !rl.Enabled {
		return func(c *gin.Context) { c.Next() }
	}
	// helpers for key extraction and whitelist checks
	extractKey := func(c *gin.Context) string {
		if rl.KeyHeader != "" {
			hVal := c.GetHeader(rl.KeyHeader)
			if hVal != "" {
				// If X-Forwarded-For, take the first IP
				if strings.EqualFold(rl.KeyHeader, "X-Forwarded-For") {
					parts := strings.Split(hVal, ",")
					if len(parts) > 0 {
						return strings.TrimSpace(parts[0])
					}
				}
				return hVal
			}
		}
		ip := c.ClientIP()
		if ip == "" {
			ip = "unknown"
		}
		return ip
	}
	inStrings := func(needle string, hay []string) bool {
		for _, s := range hay {
			if needle == s {
				return true
			}
		}
		return false
	}
	type limiter struct {
		mu      sync.Mutex
		buckets map[string]*tokenBucket // ip -> bucket
		cfg     config.PathRateLimitConfig
	}
	var (
		anyPathOverrides = len(rl.Paths) > 0
		pathLimiters     []limiter
		globalLimiter    limiter
	)
	if anyPathOverrides {
		for _, p := range rl.Paths {
			if !p.Enabled || p.RequestsPerMinute <= 0 {
				continue
			}
			pathLimiters = append(pathLimiters, limiter{
				buckets: make(map[string]*tokenBucket),
				cfg:     p,
			})
		}
	}
	// global
	if rl.RequestsPerMinute > 0 {
		globalLimiter = limiter{
			buckets: make(map[string]*tokenBucket),
			cfg: config.PathRateLimitConfig{
				Enabled:           true,
				Prefix:            "",
				RequestsPerMinute: rl.RequestsPerMinute,
				Burst:             rl.Burst,
			},
		}
	}
	get := func(l *limiter, ip string) *tokenBucket {
		l.mu.Lock()
		defer l.mu.Unlock()
		if b, ok := l.buckets[ip]; ok {
			return b
		}
		b := newBucket(l.cfg.RequestsPerMinute, l.cfg.Burst)
		l.buckets[ip] = b
		return b
	}
	// rl 是构造时 cfg.Security.RateLimiting 的值拷贝，构造处（本函数开头）
	// 已拒绝 !rl.Enabled，拷贝不会再变化，故 handler 内无需重复检查。
	return func(c *gin.Context) {
		key := extractKey(c)
		// whitelist checks
		if rl.KeyHeader != "" && inStrings(key, rl.WhitelistKeys) {
			c.Next()
			return
		}
		// If keyHeader used, we still can fallback to client IP whitelist when header not set
		if (rl.KeyHeader == "" || key == "unknown") && inStrings(c.ClientIP(), rl.WhitelistIPs) {
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
					if !get(pl, key).allow() {
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
			if !get(&globalLimiter, key).allow() {
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
