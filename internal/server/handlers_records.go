package server

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/azazo1/ecnu-dorm-watt/internal/protocol"
	"github.com/azazo1/ecnu-dorm-watt/internal/store"
)

const (
	// defaultRecordsLimit 是 JSON 记录查询的默认上限.
	defaultRecordsLimit = 100000
	// maxRecordsLimit 是 JSON 记录查询的硬上限.
	maxRecordsLimit = 200000
)

// handleRecords 处理样本查询与 CSV 导出.
func (s *Server) handleRecords(w http.ResponseWriter, r *http.Request) {
	room, ok := s.queryRoom(w, r, "room_id")
	if !ok {
		return
	}
	from, err := parseQueryTime(r, "from")
	if err != nil {
		s.writeParamProblem(w, r, err)
		return
	}
	to, err := parseQueryTime(r, "to")
	if err != nil {
		s.writeParamProblem(w, r, err)
		return
	}
	if from != nil && to != nil && from.After(*to) {
		s.writeParamProblem(w, r, validationFailedf("from 不能晚于 to"))
		return
	}

	limit := defaultRecordsLimit
	if v, present, err := parseQueryInt64(r, "limit"); err != nil {
		s.writeParamProblem(w, r, err)
		return
	} else if present {
		if v <= 0 {
			s.writeParamProblem(w, r, invalidRequestf("limit 必须是正整数"))
			return
		}
		if v > maxRecordsLimit {
			s.writeParamProblem(w, r, validationFailedf("limit 不能超过 %d", maxRecordsLimit))
			return
		}
		limit = int(v)
	}

	format := "json"
	if raw, present := queryValue(r, "format"); present && raw != "" {
		format = strings.ToLower(raw)
	}
	withHeader, _, err := parseQueryBool(r, "header")
	if err != nil {
		s.writeParamProblem(w, r, err)
		return
	}

	switch format {
	case "json":
		s.writeRecordsJSON(w, r, room, from, to, limit)
	case "csv":
		s.writeRecordsCSV(w, r, room, from, to, withHeader)
	default:
		s.writeParamProblem(w, r, invalidRequestf("format 只能是 json 或 csv"))
	}
}

// writeRecordsJSON 以 JSON 返回样本.
func (s *Server) writeRecordsJSON(w http.ResponseWriter, r *http.Request, room *store.Room, from, to *time.Time, limit int) {
	ctx := r.Context()
	count, err := s.db.CountSamples(ctx, room.ID, from, to)
	if err != nil {
		s.writeStorageProblem(w, r, err)
		return
	}
	if count > limit {
		s.writeProblem(w, r, http.StatusUnprocessableEntity, protocol.CodeRecordsTooMany, "",
			fmt.Sprintf("记录数 %d 超过单次上限 %d, 请用 from 与 to 收窄范围或使用 CSV 导出", count, limit))
		return
	}
	samples, err := s.db.QuerySamples(ctx, room.ID, from, to, limit)
	if err != nil {
		s.writeStorageProblem(w, r, err)
		return
	}
	records := make([]protocol.Sample, 0, len(samples))
	for _, sample := range samples {
		records = append(records, protocol.Sample{
			Time:   s.exportTime(sample.Time),
			Degree: roundDegree(sample.Degree),
		})
	}
	s.writeJSON(w, r, http.StatusOK, protocol.Records{
		RoomID:  room.ID,
		RoomNo:  room.RoomNo,
		From:    s.exportTimePtr(from),
		To:      s.exportTimePtr(to),
		Records: records,
	})
}

// writeRecordsCSV 以 CSV 流式返回样本.
func (s *Server) writeRecordsCSV(w http.ResponseWriter, r *http.Request, room *store.Room, from, to *time.Time, withHeader bool) {
	s.writeCSVHeader(w, fmt.Sprintf("records-%d.csv", room.ID))
	s.streamSamplesCSV(w, r, withHeader, func(fn func(store.Sample) error) error {
		return s.db.StreamSamples(r.Context(), room.ID, from, to, fn)
	})
}
