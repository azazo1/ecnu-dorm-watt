package ecnu

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/azazo1/ecnu-dorm-watt/internal/protocol"
)

// RoomInfo 是按目录逐级匹配得到的房间信息.
type RoomInfo struct {
	Area     protocol.Area
	District protocol.District
	Building protocol.Building
	Floor    protocol.Floor
	Room     protocol.CatalogRoom
}

// ParseRoomNo 按 "_" 拆分房间号, 要求恰好四段且每段非空.
//
// 段含义依次为: 房间名, 地区 id, 旧建筑段(忽略), 楼层 id.
func ParseRoomNo(roomNo string) ([4]string, error) {
	var parts [4]string
	trimmed := strings.TrimSpace(roomNo)
	if trimmed == "" {
		return parts, fmt.Errorf("房间号不能为空")
	}
	segments := strings.Split(trimmed, "_")
	if len(segments) != len(parts) {
		return parts, fmt.Errorf("房间号必须为四段, 实际为 %d 段: %q", len(segments), roomNo)
	}
	for i, segment := range segments {
		if strings.TrimSpace(segment) == "" {
			return parts, fmt.Errorf("房间号第 %d 段为空: %q", i+1, roomNo)
		}
		parts[i] = segment
	}
	return parts, nil
}

// RoomInfo 逐级查询地区, 行政区, 建筑, 楼层与房间, 并按 room_no 匹配.
//
// 房间号必须为四段, 否则返回校验错误.
// 任一目录缺少对应项时返回 ErrCatalogIncomplete.
func (c *Client) RoomInfo(ctx context.Context, room Room, session Session) (RoomInfo, error) {
	parts, err := ParseRoomNo(room.RoomNo)
	if err != nil {
		return RoomInfo{}, err
	}
	areaID := strconv.Itoa(room.Elcarea)
	districtID := parts[1]
	buildingID := strings.TrimSpace(room.Elcbuis)
	if buildingID == "" {
		return RoomInfo{}, fmt.Errorf("建筑 id 不能为空")
	}

	districts, err := c.Districts(ctx, session)
	if err != nil {
		return RoomInfo{}, err
	}
	area, ok := findArea(districts.Areas, areaID)
	if !ok {
		return RoomInfo{}, fmt.Errorf("%w: 地区 %s 不存在", ErrCatalogIncomplete, areaID)
	}
	district, ok := findDistrict(districts.Districts, districtID)
	if !ok {
		return RoomInfo{}, fmt.Errorf("%w: 行政区 %s 不存在", ErrCatalogIncomplete, districtID)
	}

	buildings, err := c.Buildings(ctx, session, areaID, districtID)
	if err != nil {
		return RoomInfo{}, err
	}
	building, ok := findBuilding(buildings.Buildings, buildingID)
	if !ok {
		return RoomInfo{}, fmt.Errorf("%w: 建筑 %s 不存在", ErrCatalogIncomplete, buildingID)
	}

	floors, err := c.Floors(ctx, session, areaID, districtID, building.BuildingID)
	if err != nil {
		return RoomInfo{}, err
	}
	floor, ok := findFloor(floors.Floors, parts[3])
	if !ok {
		return RoomInfo{}, fmt.Errorf("%w: 楼层 %s 不存在", ErrCatalogIncomplete, parts[3])
	}

	rooms, err := c.Rooms(ctx, session, areaID, districtID, building.BuildingID, floor.FloorID)
	if err != nil {
		return RoomInfo{}, err
	}
	if _, ok := findRoom(rooms.Rooms, room.RoomNo); !ok {
		return RoomInfo{}, fmt.Errorf("%w: 房间 %s 不存在", ErrCatalogIncomplete, room.RoomNo)
	}

	return RoomInfo{
		Area:     area,
		District: district,
		Building: building,
		Floor:    floor,
		// 房间名按设计文档取房间号第一段, 不信任上游回显.
		Room: protocol.CatalogRoom{RoomID: strings.TrimSpace(room.RoomNo), RoomName: parts[0]},
	}, nil
}

func findArea(items []protocol.Area, id string) (protocol.Area, bool) {
	for _, item := range items {
		if item.AreaID == id {
			return item, true
		}
	}
	return protocol.Area{}, false
}

func findDistrict(items []protocol.District, id string) (protocol.District, bool) {
	for _, item := range items {
		if item.DistrictID == id {
			return item, true
		}
	}
	return protocol.District{}, false
}

func findBuilding(items []protocol.Building, id string) (protocol.Building, bool) {
	for _, item := range items {
		if item.BuildingID == id {
			return item, true
		}
	}
	return protocol.Building{}, false
}

func findFloor(items []protocol.Floor, id string) (protocol.Floor, bool) {
	for _, item := range items {
		if item.FloorID == id {
			return item, true
		}
	}
	return protocol.Floor{}, false
}

func findRoom(items []protocol.CatalogRoom, id string) (protocol.CatalogRoom, bool) {
	for _, item := range items {
		if item.RoomID == id {
			return item, true
		}
	}
	return protocol.CatalogRoom{}, false
}
