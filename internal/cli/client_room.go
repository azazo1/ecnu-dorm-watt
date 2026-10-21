package cli

import (
	"fmt"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/azazo1/ecnu-dorm-watt/internal/client/upstream"
	"github.com/azazo1/ecnu-dorm-watt/internal/protocol"
)

func (a *App) newRoomCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "room",
		Short: "管理房间",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(
		a.newRoomListCommand(),
		a.newRoomCurrentCommand(),
		a.newRoomSetCommand(),
		a.newRoomSelectCommand(),
		a.newRoomInfoCommand(),
		a.newRoomClearCommand(),
	)
	return cmd
}

func (a *App) newRoomListCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "列出所有房间",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, cfg, _, err := a.clientFor()
			if err != nil {
				return err
			}
			rooms, err := c.Rooms(cmd.Context())
			if err != nil {
				return err
			}
			if a.opts.json {
				return a.printJSON(map[string]any{"rooms": rooms})
			}
			loc := location(cfg.UI.Timezone)
			rows := make([][]string, 0, len(rooms))
			for _, room := range rooms {
				active := "no"
				if room.Active {
					active = "yes"
				}
				rows = append(rows, []string{
					fmt.Sprintf("%d", room.ID),
					room.RoomNo,
					fmt.Sprintf("%d", room.Elcarea),
					room.Elcbuis,
					active,
					fmt.Sprintf("%d", room.SampleCount),
					formatTimePtr(room.LastSampleAt, loc),
				})
			}
			return a.printTable(a.stdout, []string{"ID", "ROOM_NO", "AREA", "BUILDING", "ACTIVE", "SAMPLES", "LAST"}, rows)
		},
	}
}

func (a *App) newRoomCurrentCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "current",
		Short: "显示活动房间",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, cfg, _, err := a.clientFor()
			if err != nil {
				return err
			}
			room, err := c.ActiveRoom(cmd.Context())
			if err != nil {
				if upstream.IsCode(err, protocol.CodeRoomNotConfigured) {
					return exitErr(ExitNotConfigured, fmt.Errorf("当前没有活动房间, 请执行 dormwatt client room set 或 dormwatt client room select"))
				}
				return err
			}
			if a.opts.json {
				return a.printJSON(room)
			}
			loc := location(cfg.UI.Timezone)
			return a.printKV(a.stdout, [][2]string{
				{"id", fmt.Sprintf("%d", room.ID)},
				{"room_no", room.RoomNo},
				{"area", fmt.Sprintf("%d", room.Elcarea)},
				{"building", room.Elcbuis},
				{"created", formatTime(room.CreatedAt, loc)},
				{"samples", fmt.Sprintf("%d", room.SampleCount)},
				{"last sample", formatTimePtr(room.LastSampleAt, loc)},
			})
		},
	}
}

func (a *App) newRoomSetCommand() *cobra.Command {
	var (
		roomNo     string
		area       int
		building   string
		noActivate bool
	)
	cmd := &cobra.Command{
		Use:   "set",
		Short: "按参数设置房间",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if roomNo == "" || building == "" {
				return usageErrorf("必须提供 --room-no 与 --building")
			}
			if area < 0 {
				return usageErrorf("--area 必须大于等于 0")
			}
			c, _, _, err := a.clientFor()
			if err != nil {
				return err
			}
			activate := !noActivate
			room, created, err := c.CreateRoom(cmd.Context(), upstream.CreateRoomRequest{
				RoomNo:   roomNo,
				Elcarea:  area,
				Elcbuis:  building,
				Activate: &activate,
			})
			if err != nil {
				return err
			}
			if a.opts.json {
				return a.printJSON(map[string]any{
					"room":      room,
					"created":   created,
					"activated": activate,
				})
			}
			state := "已存在"
			if created {
				state = "新建"
			}
			fmt.Fprintf(a.stdout, "房间 id: %d (%s)\n", room.ID, state)
			if activate {
				fmt.Fprintln(a.stdout, "已切换为活动房间")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&roomNo, "room-no", "", "房间编号, 例如 4408_MH_83_257")
	cmd.Flags().IntVar(&area, "area", 0, "地区 id, 例如 102")
	cmd.Flags().StringVar(&building, "building", "", "建筑 id, 例如 new-83_MH")
	cmd.Flags().BoolVar(&noActivate, "no-activate", false, "只创建房间, 不设为活动房间")
	return cmd
}

func (a *App) newRoomSelectCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "select",
		Short: "交互式选择房间",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !isTerminalReader(a.stdin) || !isTerminalWriter(a.stdout) {
				return usageErrorf("标准输入或输出不是终端, 请改用 dormwatt client room set 显式传入参数")
			}
			c, _, _, err := a.clientFor()
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			districts, err := c.Districts(ctx)
			if err != nil {
				return err
			}
			areaID := ""
			if len(districts.Areas) == 1 {
				areaID = districts.Areas[0].AreaID
			} else if len(districts.Areas) > 1 {
				names := make([]string, len(districts.Areas))
				for i, area := range districts.Areas {
					names[i] = area.AreaName
				}
				idx, err := a.chooseOption("校区", names)
				if err != nil {
					return exitErr(ExitInterrupted, err)
				}
				areaID = districts.Areas[idx].AreaID
			}
			if len(districts.Districts) == 0 {
				return exitErr(ExitServer, fmt.Errorf("服务端未返回可用的地区列表"))
			}
			districtNames := make([]string, len(districts.Districts))
			for i, d := range districts.Districts {
				districtNames[i] = d.DistrictName
			}
			districtIdx, err := a.chooseOption("地区", districtNames)
			if err != nil {
				return exitErr(ExitInterrupted, err)
			}
			district := districts.Districts[districtIdx]

			buildings, err := c.Buildings(ctx, areaID, district.DistrictID)
			if err != nil {
				return err
			}
			if len(buildings.Buildings) == 0 {
				return exitErr(ExitServer, fmt.Errorf("该地区没有可用建筑"))
			}
			buildingNames := make([]string, len(buildings.Buildings))
			for i, b := range buildings.Buildings {
				buildingNames[i] = b.BuildingName
			}
			buildingIdx, err := a.chooseOption("建筑", buildingNames)
			if err != nil {
				return exitErr(ExitInterrupted, err)
			}
			building := buildings.Buildings[buildingIdx]

			floors, err := c.Floors(ctx, areaID, district.DistrictID, building.BuildingID)
			if err != nil {
				return err
			}
			if len(floors.Floors) == 0 {
				return exitErr(ExitServer, fmt.Errorf("该建筑没有可用楼层"))
			}
			floorNames := make([]string, len(floors.Floors))
			for i, f := range floors.Floors {
				floorNames[i] = f.FloorName
			}
			floorIdx, err := a.chooseOption("楼层", floorNames)
			if err != nil {
				return exitErr(ExitInterrupted, err)
			}
			floor := floors.Floors[floorIdx]

			rooms, err := c.CatalogRooms(ctx, areaID, district.DistrictID, building.BuildingID, floor.FloorID)
			if err != nil {
				return err
			}
			if len(rooms.Rooms) == 0 {
				return exitErr(ExitServer, fmt.Errorf("该楼层没有可用房间"))
			}
			roomNames := make([]string, len(rooms.Rooms))
			for i, r := range rooms.Rooms {
				roomNames[i] = r.RoomName
			}
			roomIdx, err := a.chooseOption("房间", roomNames)
			if err != nil {
				return exitErr(ExitInterrupted, err)
			}
			room := rooms.Rooms[roomIdx]

			area := 0
			if areaID != "" {
				if n, err := parseInt(areaID); err == nil {
					area = n
				}
			}
			activate := true
			saved, created, err := c.CreateRoom(ctx, upstream.CreateRoomRequest{
				RoomNo:   room.RoomID,
				Elcarea:  area,
				Elcbuis:  building.BuildingID,
				Activate: &activate,
			})
			if err != nil {
				return err
			}
			if a.opts.json {
				return a.printJSON(map[string]any{"room": saved, "created": created, "activated": true})
			}
			fmt.Fprintf(a.stdout, "房间 id: %d (%s), 已设为活动房间\n", saved.ID, saved.RoomNo)
			return nil
		},
	}
}

func (a *App) newRoomInfoCommand() *cobra.Command {
	var roomID int64
	cmd := &cobra.Command{
		Use:   "info",
		Short: "显示房间的中文名称信息",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, _, _, err := a.clientFor()
			if err != nil {
				return err
			}
			id := roomID
			if id == 0 {
				room, err := c.ActiveRoom(cmd.Context())
				if err != nil {
					if upstream.IsCode(err, protocol.CodeRoomNotConfigured) {
						return exitErr(ExitNotConfigured, fmt.Errorf("当前没有活动房间, 请使用 --room 指定房间 id"))
					}
					return err
				}
				id = room.ID
			}
			info, err := c.RoomInfo(cmd.Context(), id)
			if err != nil {
				return err
			}
			if a.opts.json {
				return a.printJSON(info)
			}
			return a.printKV(a.stdout, [][2]string{
				{"地区", info.Area.AreaName},
				{"小区", info.District.DistrictName},
				{"建筑", info.Building.BuildingName},
				{"楼层", info.Floor.FloorName},
				{"房间", info.Room.RoomName},
			})
		},
	}
	cmd.Flags().Int64Var(&roomID, "room", 0, "房间 id, 默认活动房间")
	return cmd
}

func (a *App) newRoomClearCommand() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "clear",
		Short: "取消活动房间",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, _, _, err := a.clientFor()
			if err != nil {
				return err
			}
			if !yes {
				ok, err := a.confirm("取消活动房间后采集会暂停, 历史数据保留. 继续?")
				if err != nil {
					return exitErr(ExitInterrupted, err)
				}
				if !ok {
					fmt.Fprintln(a.stdout, "已取消")
					return nil
				}
			}
			if err := c.ClearActiveRoom(cmd.Context()); err != nil {
				return err
			}
			if a.opts.json {
				return a.printJSON(map[string]any{"ok": true, "active_room": nil})
			}
			fmt.Fprintln(a.stdout, "活动房间已取消")
			return nil
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "跳过确认")
	return cmd
}

func parseInt(s string) (int, error) {
	return strconv.Atoi(s)
}
