package server

import (
	"sync"
	"time"
)

// bucket 是单个键的令牌桶状态.
type bucket struct {
	tokens float64
	last   time.Time
}

// limiter 是进程内的简单令牌桶限速器.
//
// 状态只保存在内存中, 重启后清零; 空闲桶会被定期回收.
type limiter struct {
	mu      sync.Mutex
	rate    float64 // 每秒补充的令牌数
	burst   float64 // 桶容量
	buckets map[string]*bucket
	lastGC  time.Time
	now     func() time.Time
}

// newLimiter 按每分钟次数构造限速器, burst 小于 1 时退化为每分钟次数.
func newLimiter(perMinute, burst int) *limiter {
	if perMinute <= 0 {
		perMinute = 1
	}
	if burst <= 0 {
		burst = perMinute
	}
	if burst < perMinute {
		burst = perMinute
	}
	return &limiter{
		rate:    float64(perMinute) / 60,
		burst:   float64(burst),
		buckets: make(map[string]*bucket),
		now:     time.Now,
	}
}

// available 判断键当前是否还有令牌, 但不消耗令牌.
func (l *limiter) available(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	b := l.refill(key, now)
	return b.tokens >= 1
}

// take 消耗一个令牌, 桶为空时返回 false.
func (l *limiter) take(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	b := l.refill(key, now)
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// retryAfter 返回需要等待多久才能再获得一个令牌.
func (l *limiter) retryAfter(key string) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	b := l.refill(key, now)
	if b.tokens >= 1 {
		return 0
	}
	missing := 1 - b.tokens
	seconds := missing / l.rate
	if seconds < 1 {
		seconds = 1
	}
	return time.Duration(seconds * float64(time.Second))
}

// refill 按流逝时间补充令牌并返回桶状态, 调用方必须持有锁.
func (l *limiter) refill(key string, now time.Time) *bucket {
	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{tokens: l.burst, last: now}
		l.buckets[key] = b
	} else {
		elapsed := now.Sub(b.last).Seconds()
		if elapsed > 0 {
			b.tokens += elapsed * l.rate
			if b.tokens > l.burst {
				b.tokens = l.burst
			}
			b.last = now
		}
	}
	l.gc(now)
	return b
}

// gc 回收长时间空闲的桶, 避免内存无界增长.
func (l *limiter) gc(now time.Time) {
	if len(l.buckets) < 1024 {
		return
	}
	if now.Sub(l.lastGC) < 10*time.Minute {
		return
	}
	l.lastGC = now
	for key, b := range l.buckets {
		if now.Sub(b.last) > 10*time.Minute {
			delete(l.buckets, key)
		}
	}
}
