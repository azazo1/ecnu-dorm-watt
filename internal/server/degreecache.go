package server

import (
	"sync"
	"time"
)

// defaultLiveQueryTTL 是实时电量缓存的默认有效期.
const defaultLiveQueryTTL = 15 * time.Second

// liveEntry 是单个房间的实时电量缓存.
type liveEntry struct {
	degree    float64
	queriedAt time.Time
	until     time.Time
}

// liveQuery 在短时间内复用同一房间的电量查询结果, 避免重复请求上游.
type liveQuery struct {
	mu      sync.Mutex
	entries map[int64]liveEntry
}

// newLiveQuery 构造实时电量缓存.
func newLiveQuery() *liveQuery {
	return &liveQuery{entries: make(map[int64]liveEntry)}
}

// get 读取未过期的缓存.
func (c *liveQuery) get(roomID int64, now time.Time) (liveEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[roomID]
	if !ok || !now.Before(entry.until) {
		return liveEntry{}, false
	}
	return entry, true
}

// put 写入缓存, 并顺带清理过期条目.
func (c *liveQuery) put(roomID int64, entry liveEntry, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.entries) >= 64 {
		for id, expired := range c.entries {
			if !now.Before(expired.until) {
				delete(c.entries, id)
			}
		}
	}
	c.entries[roomID] = entry
}

// liveQueryTTL 规范化缓存有效期.
func liveQueryTTL(ttl time.Duration) time.Duration {
	if ttl <= 0 {
		return defaultLiveQueryTTL
	}
	return ttl
}
