package ecnu

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/azazo1/ecnu-dorm-watt/internal/protocol"
)

// 以下结构镜像上游 JSON, 只在解析时使用 camelCase 字段名.

type upstreamArea struct {
	AreaID   string `json:"areaId"`
	AreaName string `json:"areaName"`
}

type upstreamDistrict struct {
	DistrictID   string `json:"districtId"`
	DistrictName string `json:"districtName"`
}

type upstreamBuilding struct {
	BuiID   string `json:"buiId"`
	BuiName string `json:"buiName"`
}

type upstreamFloor struct {
	FloorID   string `json:"floorId"`
	FloorName string `json:"floorName"`
}

type upstreamRoom struct {
	RoomID   string `json:"roomId"`
	RoomName string `json:"roomName"`
}

type catalogEnvelope struct {
	Retcode   *int               `json:"retcode"`
	Retmsg    string             `json:"retmsg"`
	Areas     []upstreamArea     `json:"areas"`
	Districts []upstreamDistrict `json:"districts"`
	Buils     []upstreamBuilding `json:"buils"`
	Floors    []upstreamFloor    `json:"floors"`
	Rooms     []upstreamRoom     `json:"rooms"`
}

// Districts 查询地区与行政区列表.
func (c *Client) Districts(ctx context.Context, session Session) (protocol.Districts, error) {
	form := url.Values{}
	form.Set("sysid", formSysID)

	var envelope catalogEnvelope
	if err := c.queryCatalog(ctx, session, pathQueryAreas, form, &envelope); err != nil {
		return protocol.Districts{}, err
	}
	return protocol.Districts{
		Areas:     mapAreas(envelope.Areas),
		Districts: mapDistricts(envelope.Districts),
	}, nil
}

// Buildings 查询指定地区与行政区下的建筑列表.
func (c *Client) Buildings(ctx context.Context, session Session, areaID, districtID string) (protocol.Buildings, error) {
	form := url.Values{}
	form.Set("sysid", formSysID)
	form.Set("area", areaID)
	form.Set("district", districtID)

	var envelope catalogEnvelope
	if err := c.queryCatalog(ctx, session, pathQueryBuis, form, &envelope); err != nil {
		return protocol.Buildings{}, err
	}
	return protocol.Buildings{Buildings: mapBuildings(envelope.Buils)}, nil
}

// Floors 查询指定建筑下的楼层列表.
func (c *Client) Floors(ctx context.Context, session Session, areaID, districtID, buildingID string) (protocol.Floors, error) {
	form := url.Values{}
	form.Set("sysid", formSysID)
	form.Set("area", areaID)
	form.Set("district", districtID)
	form.Set("build", buildingID)

	var envelope catalogEnvelope
	if err := c.queryCatalog(ctx, session, pathQueryFloors, form, &envelope); err != nil {
		return protocol.Floors{}, err
	}
	return protocol.Floors{Floors: mapFloors(envelope.Floors)}, nil
}

// Rooms 查询指定楼层下的房间列表.
func (c *Client) Rooms(ctx context.Context, session Session, areaID, districtID, buildingID, floorID string) (protocol.CatalogRooms, error) {
	form := url.Values{}
	form.Set("sysid", formSysID)
	form.Set("area", areaID)
	form.Set("district", districtID)
	form.Set("build", buildingID)
	form.Set("floor", floorID)

	var envelope catalogEnvelope
	if err := c.queryCatalog(ctx, session, pathQueryRooms, form, &envelope); err != nil {
		return protocol.CatalogRooms{}, err
	}
	return protocol.CatalogRooms{Rooms: mapRooms(envelope.Rooms)}, nil
}

// queryCatalog 发送目录请求并解析信封.
func (c *Client) queryCatalog(ctx context.Context, session Session, path string, form url.Values, out *catalogEnvelope) error {
	body, err := c.post(ctx, session, path, form)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("%w: 解析目录响应失败: %v", ErrUpstream, err)
	}
	if out.Retcode != nil && *out.Retcode != 0 {
		return fmt.Errorf("%w: retcode=%d retmsg=%s", ErrUpstream, *out.Retcode, strings.TrimSpace(out.Retmsg))
	}
	return nil
}

func mapAreas(items []upstreamArea) []protocol.Area {
	areas := make([]protocol.Area, 0, len(items))
	for _, item := range items {
		areas = append(areas, protocol.Area{AreaID: item.AreaID, AreaName: item.AreaName})
	}
	return areas
}

func mapDistricts(items []upstreamDistrict) []protocol.District {
	districts := make([]protocol.District, 0, len(items))
	for _, item := range items {
		districts = append(districts, protocol.District{
			DistrictID:   item.DistrictID,
			DistrictName: item.DistrictName,
		})
	}
	return districts
}

func mapBuildings(items []upstreamBuilding) []protocol.Building {
	buildings := make([]protocol.Building, 0, len(items))
	for _, item := range items {
		buildings = append(buildings, protocol.Building{
			BuildingID:   item.BuiID,
			BuildingName: item.BuiName,
		})
	}
	return buildings
}

func mapFloors(items []upstreamFloor) []protocol.Floor {
	floors := make([]protocol.Floor, 0, len(items))
	for _, item := range items {
		floors = append(floors, protocol.Floor{FloorID: item.FloorID, FloorName: item.FloorName})
	}
	return floors
}

func mapRooms(items []upstreamRoom) []protocol.CatalogRoom {
	rooms := make([]protocol.CatalogRoom, 0, len(items))
	for _, item := range items {
		rooms = append(rooms, protocol.CatalogRoom{RoomID: item.RoomID, RoomName: item.RoomName})
	}
	return rooms
}
