package middleware

import (
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/cuihairu/courier/gateway/aggregation"
)

// RateLimiter 进程内令牌桶限流(单机;多实例协同与基础限流配置随批次 3 联调)。
type RateLimiter struct {
	mu      sync.Mutex
	rate    float64 // 每秒令牌数
	burst   float64 // 桶容量
	buckets map[string]*bucket
}

type bucket struct {
	tokens float64
	last   time.Time
}

// maxBuckets 上限:超限清退闲置(>10 分钟)的桶,防内存无界增长。
const maxBuckets = 10000

// NewRateLimiter 令牌桶:每分钟 perMinute 个令牌,桶容量 burst。
func NewRateLimiter(perMinute, burst int) *RateLimiter {
	if perMinute <= 0 || burst <= 0 {
		return nil // 非法参数 = 不限流(与 nil 一致)
	}
	return &RateLimiter{
		rate:    float64(perMinute) / 60.0,
		burst:   float64(burst),
		buckets: make(map[string]*bucket),
	}
}

// Allow 取一个令牌;返回 (false, retryAfter) 表示应等待的时长。
func (l *RateLimiter) Allow(key string) (bool, time.Duration) {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()

	b, ok := l.buckets[key]
	if !ok {
		if len(l.buckets) >= maxBuckets {
			l.prune(now)
		}
		b = &bucket{tokens: l.burst, last: now}
		l.buckets[key] = b
	}
	// 补充流逝时间对应的令牌
	b.tokens += now.Sub(b.last).Seconds() * l.rate
	if b.tokens > l.burst {
		b.tokens = l.burst
	}
	b.last = now

	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}
	need := (1 - b.tokens) / l.rate
	return false, time.Duration(need * float64(time.Second))
}

// prune 清退闲置桶。
func (l *RateLimiter) prune(now time.Time) {
	for k, b := range l.buckets {
		if now.Sub(b.last) > 10*time.Minute {
			delete(l.buckets, k)
		}
	}
}

// RateLimit 限流中间件:key 取客户端 IP;超出 → 429 RATE_LIMITED + Retry-After
// (errors.md:retryable=true,遵守 Retry-After)。limiter 为 nil 直接放行。
func RateLimit(limiter *RateLimiter) Middleware {
	if limiter == nil {
		return func(next http.Handler) http.Handler { return next }
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ok, retry := limiter.Allow(clientIP(r))
			if !ok {
				seconds := int(retry.Seconds())
				if seconds < 1 {
					seconds = 1
				}
				w.Header().Set("Retry-After", strconv.Itoa(seconds))
				aggregation.WriteError(w, TraceID(r.Context()), aggregation.CodeRateLimited, "rate limited")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// clientIP 提取客户端 IP(RemoteAddr host 部分;测试环境为 127.0.0.1)。
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
