package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/azazo1/ecnu-dorm-watt/internal/ecnu"
	"github.com/azazo1/ecnu-dorm-watt/internal/protocol"
	"github.com/azazo1/ecnu-dorm-watt/internal/server"
	"github.com/azazo1/ecnu-dorm-watt/internal/store"
)

// CatalogSource 把 ECNU 目录接口适配为服务端目录来源, 并带短期缓存.
type CatalogSource struct {
	DB     store.DB
	Client *ecnu.Client
	TTL    time.Duration
	Now    func() time.Time

	mu    sync.Mutex
	cache map[string]catalogEntry
}

type catalogEntry struct {
	value   any
	expires time.Time
}

// NewCatalogSource 构造目录来源.
func NewCatalogSource(db store.DB, client *ecnu.Client, ttl time.Duration) *CatalogSource {
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	return &CatalogSource{
		DB:     db,
		Client: client,
		TTL:    ttl,
		Now:    time.Now,
		cache:  make(map[string]catalogEntry),
	}
}

func (c *CatalogSource) session(ctx context.Context) (ecnu.Session, error) {
	session, err := c.DB.GetSession(ctx)
	if err != nil {
		if err == store.ErrNotFound {
			return ecnu.Session{}, fmt.Errorf("%w: 没有可用会话", ecnu.ErrNotLoggedIn)
		}
		return ecnu.Session{}, err
	}
	result := ecnu.Session{
		JSESSIONID: session.JSESSIONID,
		Cookie:     session.Cookie,
		CSRFToken:  session.CSRFToken,
	}
	if !result.Complete() {
		return ecnu.Session{}, fmt.Errorf("%w: 会话字段不完整", ecnu.ErrNotLoggedIn)
	}
	return result, nil
}

func sessionKey(s ecnu.Session) string {
	sum := sha256.Sum256([]byte(s.JSESSIONID + "\x00" + s.Cookie + "\x00" + s.CSRFToken))
	return hex.EncodeToString(sum[:8])
}

func (c *CatalogSource) cached(key string, load func() (any, error)) (any, error) {
	now := c.Now()
	c.mu.Lock()
	if entry, ok := c.cache[key]; ok && now.Before(entry.expires) {
		c.mu.Unlock()
		return entry.value, nil
	}
	c.mu.Unlock()

	value, err := load()
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.cache[key] = catalogEntry{value: value, expires: now.Add(c.TTL)}
	c.mu.Unlock()
	return value, nil
}

// Districts 实现 server.CatalogSource.
func (c *CatalogSource) Districts(ctx context.Context) (protocol.Districts, error) {
	session, err := c.session(ctx)
	if err != nil {
		return protocol.Districts{}, err
	}
	value, err := c.cached("districts:"+sessionKey(session), func() (any, error) {
		return c.Client.Districts(ctx, session)
	})
	if err != nil {
		return protocol.Districts{}, err
	}
	return value.(protocol.Districts), nil
}

// Buildings 实现 server.CatalogSource.
func (c *CatalogSource) Buildings(ctx context.Context, areaID, districtID string) (protocol.Buildings, error) {
	session, err := c.session(ctx)
	if err != nil {
		return protocol.Buildings{}, err
	}
	key := "buildings:" + sessionKey(session) + ":" + areaID + ":" + districtID
	value, err := c.cached(key, func() (any, error) {
		return c.Client.Buildings(ctx, session, areaID, districtID)
	})
	if err != nil {
		return protocol.Buildings{}, err
	}
	return value.(protocol.Buildings), nil
}

// Floors 实现 server.CatalogSource.
func (c *CatalogSource) Floors(ctx context.Context, areaID, districtID, buildingID string) (protocol.Floors, error) {
	session, err := c.session(ctx)
	if err != nil {
		return protocol.Floors{}, err
	}
	key := "floors:" + sessionKey(session) + ":" + areaID + ":" + districtID + ":" + buildingID
	value, err := c.cached(key, func() (any, error) {
		return c.Client.Floors(ctx, session, areaID, districtID, buildingID)
	})
	if err != nil {
		return protocol.Floors{}, err
	}
	return value.(protocol.Floors), nil
}

// Rooms 实现 server.CatalogSource.
func (c *CatalogSource) Rooms(ctx context.Context, areaID, districtID, buildingID, floorID string) (protocol.CatalogRooms, error) {
	session, err := c.session(ctx)
	if err != nil {
		return protocol.CatalogRooms{}, err
	}
	key := "rooms:" + sessionKey(session) + ":" + areaID + ":" + districtID + ":" + buildingID + ":" + floorID
	value, err := c.cached(key, func() (any, error) {
		return c.Client.Rooms(ctx, session, areaID, districtID, buildingID, floorID)
	})
	if err != nil {
		return protocol.CatalogRooms{}, err
	}
	return value.(protocol.CatalogRooms), nil
}

// RoomInfo 实现 server.CatalogSource.
func (c *CatalogSource) RoomInfo(ctx context.Context, room store.Room) (server.RoomInfo, error) {
	session, err := c.session(ctx)
	if err != nil {
		return server.RoomInfo{}, err
	}
	info, err := c.Client.RoomInfo(ctx, ecnu.Room{
		RoomNo:  room.RoomNo,
		Elcarea: room.Elcarea,
		Elcbuis: room.Elcbuis,
	}, session)
	if err != nil {
		return server.RoomInfo{}, err
	}
	return server.RoomInfo{
		Area:     info.Area,
		District: info.District,
		Building: info.Building,
		Floor:    info.Floor,
		Room:     info.Room,
	}, nil
}
