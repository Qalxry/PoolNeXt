package middleware

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"poolx/internal/pkg/common"
)

const rateLimitSweepInterval = 1024

var redisRateLimitScript = redis.NewScript(`
local current_time = redis.call("TIME")
local now_micros = tonumber(current_time[1]) * 1000000 + tonumber(current_time[2])
local cutoff_micros = now_micros - tonumber(ARGV[2])

redis.call("ZREMRANGEBYSCORE", KEYS[1], "-inf", cutoff_micros)
if redis.call("ZCARD", KEYS[1]) >= tonumber(ARGV[1]) then
  redis.call("PEXPIRE", KEYS[1], ARGV[3])
  redis.call("PEXPIRE", KEYS[2], ARGV[3])
  return 0
end

local sequence = redis.call("INCR", KEYS[2])
redis.call("ZADD", KEYS[1], now_micros, tostring(now_micros) .. ":" .. tostring(sequence))
redis.call("PEXPIRE", KEYS[1], ARGV[3])
redis.call("PEXPIRE", KEYS[2], ARGV[3])
return 1
`)

type rateLimitBucket struct {
	timestamps []int64
	expiresAt  int64
}

type slidingWindowLimiter struct {
	mu           sync.Mutex
	buckets      map[string]rateLimitBucket
	requestCount uint64
}

var inMemoryRateLimiter = newSlidingWindowLimiter()

func newSlidingWindowLimiter() *slidingWindowLimiter {
	return &slidingWindowLimiter{buckets: make(map[string]rateLimitBucket)}
}

func (l *slidingWindowLimiter) allow(key string, maxRequestNum int, window time.Duration, retention time.Duration, now time.Time) bool {
	if maxRequestNum <= 0 || window <= 0 {
		return false
	}
	if retention < window {
		retention = window
	}

	nowNanos := now.UnixNano()
	cutoff := now.Add(-window).UnixNano()

	l.mu.Lock()
	defer l.mu.Unlock()

	l.requestCount++
	if l.requestCount%rateLimitSweepInterval == 0 {
		for existingKey, bucket := range l.buckets {
			if bucket.expiresAt <= nowNanos {
				delete(l.buckets, existingKey)
			}
		}
	}

	bucket := l.buckets[key]
	firstActive := sort.Search(len(bucket.timestamps), func(i int) bool {
		return bucket.timestamps[i] > cutoff
	})
	if firstActive > 0 {
		active := make([]int64, len(bucket.timestamps)-firstActive)
		copy(active, bucket.timestamps[firstActive:])
		bucket.timestamps = active
	}
	expiresAt := now.Add(retention).UnixNano()
	if expiresAt > bucket.expiresAt {
		bucket.expiresAt = expiresAt
	}

	if len(bucket.timestamps) >= maxRequestNum {
		l.buckets[key] = bucket
		return false
	}

	insertAt := sort.Search(len(bucket.timestamps), func(i int) bool {
		return bucket.timestamps[i] > nowNanos
	})
	bucket.timestamps = append(bucket.timestamps, 0)
	copy(bucket.timestamps[insertAt+1:], bucket.timestamps[insertAt:])
	bucket.timestamps[insertAt] = nowNanos
	l.buckets[key] = bucket
	return true
}

func redisSlidingWindowAllow(ctx context.Context, client redis.Scripter, key string, maxRequestNum int, window time.Duration, retention time.Duration) (bool, error) {
	if client == nil {
		return false, fmt.Errorf("redis rate limiter is not initialized")
	}
	if maxRequestNum <= 0 || window <= 0 {
		return false, nil
	}
	if retention < window {
		retention = window
	}

	windowMicros := window.Microseconds()
	retentionMillis := retention.Milliseconds()
	if retentionMillis <= 0 {
		retentionMillis = 1
	}
	sequenceKey := key + ":sequence"
	result, err := redisRateLimitScript.Run(
		ctx,
		client,
		[]string{key, sequenceKey},
		maxRequestNum,
		windowMicros,
		retentionMillis,
	).Int64()
	if err != nil {
		return false, err
	}
	return result == 1, nil
}

func redisRateLimiter(c *gin.Context, maxRequestNum int, duration int64, mark string) {
	clientIP := c.ClientIP()
	key := "rateLimit:{" + mark + ":" + clientIP + "}:events"
	allowed, err := redisSlidingWindowAllow(
		c.Request.Context(),
		common.RDB,
		key,
		maxRequestNum,
		time.Duration(duration)*time.Second,
		common.RateLimitKeyExpirationDuration,
	)
	if err != nil {
		slog.ErrorContext(c.Request.Context(), "redis rate limiter failed", "error", err)
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}
	if !allowed {
		c.AbortWithStatus(http.StatusTooManyRequests)
	}
}

func memoryRateLimiter(c *gin.Context, maxRequestNum int, duration int64, mark string) {
	key := mark + ":" + c.ClientIP()
	if !inMemoryRateLimiter.allow(
		key,
		maxRequestNum,
		time.Duration(duration)*time.Second,
		common.RateLimitKeyExpirationDuration,
		time.Now(),
	) {
		c.AbortWithStatus(http.StatusTooManyRequests)
	}
}

func rateLimitFactory(maxRequestNum int, duration int64, mark string) gin.HandlerFunc {
	if common.RedisEnabled {
		return func(c *gin.Context) {
			redisRateLimiter(c, maxRequestNum, duration, mark)
		}
	}
	return func(c *gin.Context) {
		memoryRateLimiter(c, maxRequestNum, duration, mark)
	}
}

func GlobalWebRateLimit() gin.HandlerFunc {
	return rateLimitFactory(common.GlobalWebRateLimitNum, common.GlobalWebRateLimitDuration, "GW")
}

func GlobalAPIRateLimit() gin.HandlerFunc {
	return rateLimitFactory(common.GlobalApiRateLimitNum, common.GlobalApiRateLimitDuration, "GA")
}

func CriticalRateLimit() gin.HandlerFunc {
	return rateLimitFactory(common.CriticalRateLimitNum, common.CriticalRateLimitDuration, "CT")
}

func DownloadRateLimit() gin.HandlerFunc {
	return rateLimitFactory(common.DownloadRateLimitNum, common.DownloadRateLimitDuration, "DW")
}

func UploadRateLimit() gin.HandlerFunc {
	return rateLimitFactory(common.UploadRateLimitNum, common.UploadRateLimitDuration, "UP")
}

// ExpensiveOperationGate caps process-wide concurrency for operations that can
// spawn subprocesses, perform large imports, run node probes, or replace
// binaries. It rejects excess work instead of allowing an unbounded waiter
// queue to consume memory.
func ExpensiveOperationGate(maxConcurrent int) gin.HandlerFunc {
	if maxConcurrent <= 0 {
		maxConcurrent = 1
	}
	slots := make(chan struct{}, maxConcurrent)

	return func(c *gin.Context) {
		if !isExpensiveOperation(c.Request.Method, c.Request.URL.Path) {
			c.Next()
			return
		}

		select {
		case <-c.Request.Context().Done():
			c.Abort()
			return
		case slots <- struct{}{}:
			defer func() { <-slots }()
			c.Next()
		default:
			c.Header("Retry-After", "1")
			c.AbortWithStatus(http.StatusTooManyRequests)
		}
	}
}

func isExpensiveOperation(method string, requestPath string) bool {
	if method != http.MethodPost {
		return false
	}

	switch requestPath {
	case "/api/option/geoip/preview",
		"/api/update/manual-upload",
		"/api/update/manual-upgrade",
		"/api/update/upgrade",
		"/api/kernel/mihomo/inspect",
		"/api/kernel/mihomo/upload",
		"/api/kernel/mihomo/download",
		"/api/source-configs/parse",
		"/api/source-configs/parse-url",
		"/api/source-configs/test",
		"/api/source-configs/import",
		"/api/proxy-nodes/test",
		"/api/runtime/start",
		"/api/runtime/reload":
		return true
	default:
		return strings.HasPrefix(requestPath, "/api/port-profiles/") &&
			strings.HasSuffix(requestPath, "/runtime/save")
	}
}
