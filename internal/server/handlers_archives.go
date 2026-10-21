package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/azazo1/ecnu-dorm-watt/internal/protocol"
	"github.com/azazo1/ecnu-dorm-watt/internal/store"
)

// maxArchiveNameLength 是归档名称的 Unicode 字符上限.
const maxArchiveNameLength = 64

// createArchiveRequest 是创建归档的请求.
type createArchiveRequest struct {
	RoomID *int64  `json:"room_id"`
	From   *string `json:"from"`
	To     *string `json:"to"`
	Name   string  `json:"name"`
}

// handleListArchives 返回归档列表.
func (s *Server) handleListArchives(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var roomID *int64
	raw, present := queryValue(r, "room_id")
	if present && raw != "" {
		if raw == "all" {
			roomID = nil
		} else {
			id, _, err := parseQueryInt64(r, "room_id")
			if err != nil {
				s.writeParamProblem(w, r, err)
				return
			}
			if id <= 0 {
				s.writeParamProblem(w, r, invalidRequestf("room_id 必须是正整数或 all"))
				return
			}
			roomID = &id
		}
	} else {
		room, ok := s.requireActiveRoom(w, r)
		if !ok {
			return
		}
		roomID = &room.ID
	}
	includeDeleted, _, err := parseQueryBool(r, "include_deleted")
	if err != nil {
		s.writeParamProblem(w, r, err)
		return
	}
	archives, err := s.db.ListArchives(ctx, roomID, includeDeleted)
	if err != nil {
		s.writeStorageProblem(w, r, err)
		return
	}
	sort.SliceStable(archives, func(i, j int) bool {
		if !archives[i].StartTime.Equal(archives[j].StartTime) {
			return archives[i].StartTime.Before(archives[j].StartTime)
		}
		if archives[i].RoomID != archives[j].RoomID {
			return archives[i].RoomID < archives[j].RoomID
		}
		return archives[i].Name < archives[j].Name
	})
	out := make([]protocol.ArchiveMeta, 0, len(archives))
	for _, meta := range archives {
		out = append(out, s.exportArchive(meta))
	}
	s.writeJSON(w, r, http.StatusOK, archiveListResponse{Archives: out})
}

// handleCreateArchive 把时间范围内的样本切分为归档.
func (s *Server) handleCreateArchive(w http.ResponseWriter, r *http.Request) {
	var req createArchiveRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}
	ctx := r.Context()

	var roomID int64
	if req.RoomID != nil {
		if *req.RoomID <= 0 {
			s.writeParamProblem(w, r, validationFailedf("room_id 必须是正整数"))
			return
		}
		room, err := s.db.GetRoom(ctx, *req.RoomID)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				s.writeProblem(w, r, http.StatusNotFound, protocol.CodeNotFound, "", "房间不存在")
				return
			}
			s.writeStorageProblem(w, r, err)
			return
		}
		roomID = room.ID
	} else {
		room, ok := s.requireActiveRoom(w, r)
		if !ok {
			return
		}
		roomID = room.ID
	}

	from, err := parseBodyTime(req.From, "from")
	if err != nil {
		s.writeParamProblem(w, r, err)
		return
	}
	to, err := parseBodyTime(req.To, "to")
	if err != nil {
		s.writeParamProblem(w, r, err)
		return
	}
	if from != nil && to != nil && from.After(*to) {
		s.writeParamProblem(w, r, validationFailedf("from 不能晚于 to"))
		return
	}

	explicit := req.Name != ""
	baseName := req.Name
	if explicit {
		if msg := archiveNameProblem(baseName); msg != "" {
			s.writeParamProblem(w, r, validationFailedf("%s", msg))
			return
		}
	} else {
		generated, err := s.generatedArchiveName(ctx, roomID, from, to)
		if err != nil {
			if errors.Is(err, store.ErrArchiveEmpty) {
				s.writeProblem(w, r, http.StatusUnprocessableEntity, protocol.CodeArchiveEmpty, "", "时间范围内没有样本")
				return
			}
			s.writeStorageProblem(w, r, err)
			return
		}
		baseName = generated
	}

	// 自动生成的名称按 -2, -3 依次追加直到唯一.
	for attempt := 0; attempt < 100; attempt++ {
		name := baseName
		if !explicit && attempt > 0 {
			name = fmt.Sprintf("%s-%d", baseName, attempt+1)
		}
		meta, err := s.db.CreateArchive(ctx, roomID, name, from, to, time.Now())
		if err != nil {
			switch {
			case errors.Is(err, store.ErrArchiveEmpty):
				s.writeProblem(w, r, http.StatusUnprocessableEntity, protocol.CodeArchiveEmpty, "", "时间范围内没有样本")
				return
			case errors.Is(err, store.ErrDuplicate), errors.Is(err, store.ErrConflict):
				if explicit {
					s.writeProblem(w, r, http.StatusConflict, protocol.CodeArchiveExists, "", "归档名称已存在")
					return
				}
				continue
			case errors.Is(err, store.ErrNotFound):
				s.writeProblem(w, r, http.StatusNotFound, protocol.CodeNotFound, "", "房间不存在")
				return
			default:
				s.writeStorageProblem(w, r, err)
				return
			}
		}
		s.logger.Info("已创建归档", "name", meta.Name, "room_id", roomID, "records", meta.RecordsNum)
		s.writeJSON(w, r, http.StatusCreated, s.exportArchive(meta))
		return
	}
	s.writeProblem(w, r, http.StatusConflict, protocol.CodeArchiveExists, "", "无法生成唯一的归档名称")
}

// handleDownloadArchive 以 CSV 下载归档内容.
func (s *Server) handleDownloadArchive(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		s.writeParamProblem(w, r, invalidRequestf("归档名称不能为空"))
		return
	}
	if msg := archiveNameProblem(name); msg != "" {
		s.writeParamProblem(w, r, invalidRequestf("%s", msg))
		return
	}
	roomID, ok := s.queryArchiveRoom(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	meta, err := s.db.GetArchive(ctx, roomID, name)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			s.writeProblem(w, r, http.StatusNotFound, protocol.CodeNotFound, "", "归档不存在")
			return
		}
		s.writeStorageProblem(w, r, err)
		return
	}
	s.writeCSVHeader(w, meta.Name+".csv")
	s.streamSamplesCSV(w, r, false, func(fn func(store.Sample) error) error {
		return s.db.StreamArchive(ctx, meta.ID, fn)
	})
}

// handleDeleteArchive 软删除归档.
func (s *Server) handleDeleteArchive(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		s.writeParamProblem(w, r, invalidRequestf("归档名称不能为空"))
		return
	}
	if msg := archiveNameProblem(name); msg != "" {
		s.writeParamProblem(w, r, invalidRequestf("%s", msg))
		return
	}
	roomID, ok := s.queryArchiveRoom(w, r)
	if !ok {
		return
	}
	if err := s.db.DeleteArchive(r.Context(), roomID, name, time.Now()); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			s.writeProblem(w, r, http.StatusNotFound, protocol.CodeNotFound, "", "归档不存在或已删除")
			return
		}
		s.writeStorageProblem(w, r, err)
		return
	}
	s.logger.Info("已删除归档", "name", name)
	s.writeNoContent(w)
}

// queryArchiveRoom 解析归档接口的 room_id, 缺省时使用活动房间.
func (s *Server) queryArchiveRoom(w http.ResponseWriter, r *http.Request) (*int64, bool) {
	raw, present := queryValue(r, "room_id")
	if !present || raw == "" {
		room, ok := s.requireActiveRoom(w, r)
		if !ok {
			return nil, false
		}
		id := room.ID
		return &id, true
	}
	id, _, err := parseQueryInt64(r, "room_id")
	if err != nil {
		s.writeParamProblem(w, r, err)
		return nil, false
	}
	if id <= 0 {
		s.writeParamProblem(w, r, invalidRequestf("room_id 必须是正整数"))
		return nil, false
	}
	return &id, true
}

// exportArchive 把存储层归档转换为响应结构.
func (s *Server) exportArchive(meta store.ArchiveMeta) protocol.ArchiveMeta {
	return protocol.ArchiveMeta{
		Name:       meta.Name,
		RoomID:     meta.RoomID,
		StartTime:  s.exportTime(meta.StartTime),
		EndTime:    s.exportTime(meta.EndTime),
		RecordsNum: meta.RecordsNum,
		CreatedAt:  s.exportTime(meta.CreatedAt),
	}
}

// generatedArchiveName 按时间范围生成 YYYYMMDD-YYYYMMDD 名称.
func (s *Server) generatedArchiveName(ctx context.Context, roomID int64, from, to *time.Time) (string, error) {
	start := time.Now()
	if from != nil {
		start = *from
	} else {
		samples, err := s.db.QuerySamples(ctx, roomID, from, to, 1)
		if err != nil {
			return "", err
		}
		if len(samples) == 0 {
			return "", store.ErrArchiveEmpty
		}
		start = samples[0].Time
	}
	end := time.Now()
	if to != nil {
		end = *to
	}
	if end.Before(start) {
		end = start
	}
	return start.In(s.loc).Format("20060102") + "-" + end.In(s.loc).Format("20060102"), nil
}

// parseBodyTime 解析请求体中的 RFC3339 时间.
func parseBodyTime(raw *string, name string) (*time.Time, error) {
	if raw == nil || *raw == "" {
		return nil, nil
	}
	if strings.TrimSpace(*raw) != *raw {
		return nil, invalidRequestf("%s 不能包含空白字符", name)
	}
	t, err := protocol.ParseTime(*raw)
	if err != nil {
		return nil, validationFailedf("%s 必须是带时区偏移的 RFC3339 时间", name)
	}
	return &t, nil
}

// archiveNameProblem 校验归档名称, 返回空字符串表示合法.
func archiveNameProblem(name string) string {
	if name == "" {
		return "归档名称不能为空"
	}
	if runeCount(name) > maxArchiveNameLength {
		return fmt.Sprintf("归档名称不能超过 %d 个字符", maxArchiveNameLength)
	}
	if name == "." || name == ".." {
		return "归档名称不能是 . 或 .."
	}
	if strings.HasPrefix(name, ".") {
		return "归档名称不能以 . 开头"
	}
	if strings.ContainsAny(name, `/\`) {
		return "归档名称不能包含斜杠"
	}
	if hasControl(name) {
		return "归档名称不能包含控制字符"
	}
	return ""
}
