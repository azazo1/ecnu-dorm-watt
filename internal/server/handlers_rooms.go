package server

import (
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/azazo1/ecnu-dorm-watt/internal/protocol"
	"github.com/azazo1/ecnu-dorm-watt/internal/store"
)

// createRoomRequest 是新建房间的请求.
type createRoomRequest struct {
	RoomNo   string `json:"room_no"`
	Elcarea  int    `json:"elcarea"`
	Elcbuis  string `json:"elcbuis"`
	Activate *bool  `json:"activate"`
}

// handleListRooms 返回全部房间.
func (s *Server) handleListRooms(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rooms, err := s.db.ListRooms(ctx)
	if err != nil {
		s.writeStorageProblem(w, r, err)
		return
	}
	var activeID int64
	active, err := s.db.ActiveRoom(ctx)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		s.writeStorageProblem(w, r, err)
		return
	}
	if active != nil {
		activeID = active.ID
	}
	sort.SliceStable(rooms, func(i, j int) bool {
		if rooms[i].CreatedAt.Equal(rooms[j].CreatedAt) {
			return rooms[i].ID < rooms[j].ID
		}
		return rooms[i].CreatedAt.Before(rooms[j].CreatedAt)
	})
	out := make([]protocol.Room, 0, len(rooms))
	for _, room := range rooms {
		item, err := s.exportRoom(ctx, room, room.ID == activeID)
		if err != nil {
			s.writeStorageProblem(w, r, err)
			return
		}
		out = append(out, item)
	}
	s.writeJSON(w, r, http.StatusOK, roomListResponse{Rooms: out})
}

// handleCreateRoom 新建或复用房间.
func (s *Server) handleCreateRoom(w http.ResponseWriter, r *http.Request) {
	var req createRoomRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}
	if err := validateRoomNumber(req.RoomNo); err != nil {
		s.writeParamProblem(w, r, err)
		return
	}
	if req.Elcarea < 0 {
		s.writeParamProblem(w, r, validationFailedf("elcarea 必须是非负整数"))
		return
	}
	if err := validateBuilding(req.Elcbuis); err != nil {
		s.writeParamProblem(w, r, err)
		return
	}
	activate := true
	if req.Activate != nil {
		activate = *req.Activate
	}

	ctx := r.Context()
	room, created, err := s.db.EnsureRoom(ctx, req.RoomNo, req.Elcarea, req.Elcbuis, time.Now())
	if err != nil {
		s.writeStorageProblem(w, r, err)
		return
	}

	active := false
	if activate {
		old, oldErr := s.db.ActiveRoom(ctx)
		if oldErr != nil && !errors.Is(oldErr, store.ErrNotFound) {
			s.writeStorageProblem(w, r, oldErr)
			return
		}
		if err := s.db.SetActiveRoom(ctx, room.ID); err != nil {
			s.writeStorageProblem(w, r, err)
			return
		}
		var oldID int64
		if old != nil {
			oldID = old.ID
		}
		s.logger.Info("切换活动房间", "old_room_id", oldID, "new_room_id", room.ID)
		s.wakeMonitor()
		active = true
	} else {
		current, currentErr := s.db.ActiveRoom(ctx)
		if currentErr != nil && !errors.Is(currentErr, store.ErrNotFound) {
			s.writeStorageProblem(w, r, currentErr)
			return
		}
		active = current != nil && current.ID == room.ID
	}

	out, err := s.exportRoom(ctx, room, active)
	if err != nil {
		s.writeStorageProblem(w, r, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	s.writeJSON(w, r, status, out)
}

// handleActiveRoom 返回当前活动房间.
func (s *Server) handleActiveRoom(w http.ResponseWriter, r *http.Request) {
	room, ok := s.requireActiveRoom(w, r)
	if !ok {
		return
	}
	out, err := s.exportRoom(r.Context(), *room, true)
	if err != nil {
		s.writeStorageProblem(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, out)
}

// handleSetActiveRoom 把指定房间设为活动房间.
func (s *Server) handleSetActiveRoom(w http.ResponseWriter, r *http.Request) {
	id, err := parsePathID(r)
	if err != nil {
		s.writeParamProblem(w, r, err)
		return
	}
	ctx := r.Context()
	room, err := s.db.GetRoom(ctx, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			s.writeProblem(w, r, http.StatusNotFound, protocol.CodeNotFound, "", "房间不存在")
			return
		}
		s.writeStorageProblem(w, r, err)
		return
	}
	old, oldErr := s.db.ActiveRoom(ctx)
	if oldErr != nil && !errors.Is(oldErr, store.ErrNotFound) {
		s.writeStorageProblem(w, r, oldErr)
		return
	}
	if err := s.db.SetActiveRoom(ctx, room.ID); err != nil {
		s.writeStorageProblem(w, r, err)
		return
	}
	var oldID int64
	if old != nil {
		oldID = old.ID
	}
	s.logger.Info("切换活动房间", "old_room_id", oldID, "new_room_id", room.ID)
	s.wakeMonitor()
	s.writeNoContent(w)
}

// handleClearActiveRoom 取消活动房间.
func (s *Server) handleClearActiveRoom(w http.ResponseWriter, r *http.Request) {
	old, err := s.db.ActiveRoom(r.Context())
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		s.writeStorageProblem(w, r, err)
		return
	}
	if err := s.db.ClearActiveRoom(r.Context()); err != nil {
		s.writeStorageProblem(w, r, err)
		return
	}
	if old != nil {
		s.logger.Info("取消活动房间", "old_room_id", old.ID)
	}
	s.wakeMonitor()
	s.writeNoContent(w)
}

// handleRoomInfo 解析房间名称信息.
func (s *Server) handleRoomInfo(w http.ResponseWriter, r *http.Request) {
	id, err := parsePathID(r)
	if err != nil {
		s.writeParamProblem(w, r, err)
		return
	}
	room, err := s.db.GetRoom(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			s.writeProblem(w, r, http.StatusNotFound, protocol.CodeNotFound, "", "房间不存在")
			return
		}
		s.writeStorageProblem(w, r, err)
		return
	}
	if segments := strings.Split(room.RoomNo, "_"); len(segments) != 4 {
		s.writeParamProblem(w, r, validationFailedf("room_no 必须恰好包含四段"))
		return
	}
	if s.catalog == nil {
		s.writeProblem(w, r, http.StatusServiceUnavailable, protocol.CodeUnavailable, "", "ECNU 目录数据源尚未配置")
		return
	}
	info, err := s.catalog.RoomInfo(r.Context(), *room)
	if err != nil {
		s.writeUpstreamProblem(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, info)
}

// wakeMonitor 唤醒采集循环, 未配置时忽略.
func (s *Server) wakeMonitor() {
	if s.monitor != nil {
		s.monitor.Wake()
	}
}

// parsePathID 解析路径中的房间 id.
func parsePathID(r *http.Request) (int64, error) {
	raw := r.PathValue("id")
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		return 0, invalidRequestf("房间 id 必须是正整数")
	}
	return id, nil
}
