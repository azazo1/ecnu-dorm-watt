package ecnu

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"
)

const (
	pathQueryDegree = "/epaycas/electric/queryelectricbill"
	pathQueryAreas  = "/epaycas/electric/queryelectricarea"
	pathQueryBuis   = "/epaycas/electric/queryelectricbuis"
	pathQueryFloors = "/epaycas/electric/queryelectricfloors"
	pathQueryRooms  = "/epaycas/electric/queryelectricrooms"
	successRetmsg   = "成功"
	formSysID       = "1"
)

// degreeEnvelope 是电费查询的响应信封.
type degreeEnvelope struct {
	Retcode        *int     `json:"retcode"`
	Retmsg         string   `json:"retmsg"`
	RestElecDegree *float64 `json:"restElecDegree"`
}

// QueryDegree 查询房间剩余电量.
//
// 成功需要同时满足: HTTP 2xx, Content-Type 为 JSON, retcode 为 0,
// retmsg 为 "成功", restElecDegree 为有限非负数.
func (c *Client) QueryDegree(ctx context.Context, room Room, session Session) (float64, error) {
	roomNo := strings.TrimSpace(room.RoomNo)
	if roomNo == "" {
		return 0, fmt.Errorf("房间号不能为空")
	}

	form := url.Values{}
	form.Set("sysid", formSysID)
	form.Set("roomNo", roomNo)
	form.Set("elcarea", strconv.Itoa(room.Elcarea))
	form.Set("elcbuis", room.Elcbuis)

	body, err := c.post(ctx, session, pathQueryDegree, form)
	if err != nil {
		return 0, err
	}

	var envelope degreeEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		return 0, fmt.Errorf("%w: 解析电量响应失败: %v", ErrUpstream, err)
	}
	if envelope.Retcode == nil {
		return 0, fmt.Errorf("%w: 响应缺少 retcode 字段", ErrUpstream)
	}
	if *envelope.Retcode != 0 {
		return 0, fmt.Errorf("%w: retcode=%d retmsg=%s", ErrUpstream, *envelope.Retcode, strings.TrimSpace(envelope.Retmsg))
	}
	if strings.TrimSpace(envelope.Retmsg) != successRetmsg {
		return 0, fmt.Errorf("%w: 上游返回异常消息 %q", ErrUpstream, strings.TrimSpace(envelope.Retmsg))
	}
	if envelope.RestElecDegree == nil {
		return 0, fmt.Errorf("%w: 响应缺少 restElecDegree 字段", ErrNoDegree)
	}
	degree := *envelope.RestElecDegree
	if math.IsNaN(degree) || math.IsInf(degree, 0) || degree < 0 {
		return 0, fmt.Errorf("%w: restElecDegree 不是有限非负数", ErrUpstream)
	}
	return degree, nil
}
