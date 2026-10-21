package app

import (
	"context"
	"fmt"

	"github.com/azazo1/ecnu-dorm-watt/internal/ecnu"
	"github.com/azazo1/ecnu-dorm-watt/internal/store"
)

// DegreeSource 把 ECNU 客户端适配为服务端的实时电量来源.
type DegreeSource struct {
	Client *ecnu.Client
}

// Query 实现 server.DegreeSource.
func (d DegreeSource) Query(ctx context.Context, room store.Room, session store.Session) (float64, error) {
	if d.Client == nil {
		return 0, fmt.Errorf("ECNU 客户端未初始化")
	}
	return d.Client.QueryDegree(ctx, ecnu.Room{
		RoomNo:  room.RoomNo,
		Elcarea: room.Elcarea,
		Elcbuis: room.Elcbuis,
	}, ecnu.Session{
		JSESSIONID: session.JSESSIONID,
		Cookie:     session.Cookie,
		CSRFToken:  session.CSRFToken,
	})
}
