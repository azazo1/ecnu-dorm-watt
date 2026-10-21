package server

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/azazo1/ecnu-dorm-watt/internal/protocol"
	"github.com/azazo1/ecnu-dorm-watt/internal/store"
)

// testLocation 返回测试使用的时区.
func testLocation(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("加载时区失败: %v", err)
	}
	return loc
}

func TestRecords(t *testing.T) {
	loc := testLocation(t)
	base := time.Date(2026, 1, 1, 10, 0, 0, 0, loc)
	srv, db := newTestServer(t, nil)
	room := db.addRoom("4408_MH_83_257", 102, "new-83_MH", base)
	db.activeID = room.ID
	db.addSample(room.ID, base, 33.4321)
	db.addSample(room.ID, base.Add(time.Hour), 30)
	db.addSample(room.ID, base.Add(2*time.Hour), 28.5)
	token := db.addToken("reader", "read")

	query := url.Values{}
	query.Set("from", base.Add(30*time.Minute).Format(time.RFC3339))
	query.Set("to", base.Add(90*time.Minute).Format(time.RFC3339))
	rec := doRequest(t, srv, http.MethodGet, "/v1/records?"+query.Encode(), token, "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, body=%s", rec.Code, rec.Body.String())
	}
	var records protocol.Records
	if err := json.Unmarshal(rec.Body.Bytes(), &records); err != nil {
		t.Fatalf("解析 records 失败: %v", err)
	}
	if records.RoomID != room.ID || records.RoomNo != room.RoomNo {
		t.Fatalf("房间信息 = %+v", records)
	}
	if records.From == nil || records.To == nil {
		t.Fatal("from 与 to 应原样回显")
	}
	if len(records.Records) != 1 || !records.Records[0].Time.Equal(base.Add(time.Hour).Truncate(time.Second)) {
		t.Fatalf("时间范围过滤结果 = %+v", records.Records)
	}
	if records.Records[0].Degree != 30 {
		t.Fatalf("电量 = %v", records.Records[0].Degree)
	}

	rec = doRequest(t, srv, http.MethodGet, "/v1/records", token, "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("无参数查询状态码 = %d", rec.Code)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &records); err != nil {
		t.Fatalf("解析 records 失败: %v", err)
	}
	if len(records.Records) != 3 {
		t.Fatalf("记录数 = %d, 期望 3", len(records.Records))
	}
	for i := 1; i < len(records.Records); i++ {
		if records.Records[i].Time.Before(records.Records[i-1].Time) {
			t.Fatal("记录应按时间升序")
		}
	}
	if records.From != nil || records.To != nil {
		t.Fatal("未提供时间范围时应为 null")
	}

	rec = doRequest(t, srv, http.MethodGet, "/v1/records?limit=2", token, "", nil)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("超限状态码 = %d, 期望 422", rec.Code)
	}
	if code := problemOf(t, rec).Code; code != protocol.CodeRecordsTooMany {
		t.Fatalf("超限 code = %q", code)
	}

	rec = doRequest(t, srv, http.MethodGet, "/v1/records?from=2026-01-01T10:00:00", token, "", nil)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("缺少时区偏移状态码 = %d, 期望 422", rec.Code)
	}
	if code := problemOf(t, rec).Code; code != protocol.CodeValidationFailed {
		t.Fatalf("code = %q", code)
	}

	csvQuery := url.Values{}
	csvQuery.Set("format", "csv")
	csvQuery.Set("header", "true")
	rec = doRequest(t, srv, http.MethodGet, "/v1/records?"+csvQuery.Encode(), token, "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("CSV 状态码 = %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "text/csv; charset=utf-8" {
		t.Fatalf("CSV Content-Type = %q", got)
	}
	if got := rec.Header().Get("Content-Disposition"); !strings.Contains(got, `filename="records-1.csv"`) {
		t.Fatalf("CSV Content-Disposition = %q", got)
	}
	lines := strings.Split(strings.TrimSpace(rec.Body.String()), "\n")
	if len(lines) != 4 || lines[0] != "time,degree" {
		t.Fatalf("CSV 内容 = %q", lines)
	}
	if lines[1] != "2026-01-01T10:00:00+08:00,33.43" {
		t.Fatalf("首行 CSV = %q", lines[1])
	}

	rec = doRequest(t, srv, http.MethodGet, "/v1/records?room_id=999", token, "", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("未知房间状态码 = %d, 期望 404", rec.Code)
	}
}

func TestStatus(t *testing.T) {
	loc := testLocation(t)
	now := time.Date(2026, 1, 24, 14, 35, 32, 0, loc)
	monitor := &fakeMonitor{status: protocol.MonitorStatus{
		PollIntervalSeconds: 300,
		State:               "ok",
		LastAttemptAt:       &now,
		LastDegree:          func() *float64 { v := 33.4321; return &v }(),
	}}
	srv, db := newTestServer(t, func(deps *Deps) {
		deps.Monitor = monitor
	})
	room := db.addRoom("4408_MH_83_257", 102, "new-83_MH", now.Add(-24*time.Hour))
	db.activeID = room.ID
	db.addSample(room.ID, now, 33.43)
	if err := db.SaveSession(t.Context(), store.Session{
		JSESSIONID: "SECRET",
		State:      "ok",
		UpdatedAt:  now,
	}); err != nil {
		t.Fatalf("保存会话失败: %v", err)
	}
	token := db.addToken("reader", "read")

	rec := doRequest(t, srv, http.MethodGet, "/v1/status", token, "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, body=%s", rec.Code, rec.Body.String())
	}
	var status protocol.Status
	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
		t.Fatalf("解析 status 失败: %v", err)
	}
	if status.Version != "test" {
		t.Fatalf("status 基础字段 = %+v", status)
	}
	if status.UptimeSecond < 0 {
		t.Fatalf("uptime = %d", status.UptimeSecond)
	}
	if status.ActiveRoom == nil || status.ActiveRoom.ID != room.ID {
		t.Fatalf("active_room = %+v", status.ActiveRoom)
	}
	if status.Session.State != "ok" {
		t.Fatalf("session = %+v", status.Session)
	}
	if status.Monitor.PollIntervalSeconds != 300 || status.Monitor.LastDegree == nil || *status.Monitor.LastDegree != 33.43 {
		t.Fatalf("monitor = %+v", status.Monitor)
	}
	if status.Storage.Samples != 1 {
		t.Fatalf("storage = %+v", status.Storage)
	}
	if strings.Contains(rec.Body.String(), "SECRET") {
		t.Fatal("status 响应不应包含会话值")
	}
}

// fakeCodedError 是带错误码的上游错误.
type fakeCodedError struct {
	code string
}

func (e *fakeCodedError) Error() string { return e.code }

func (e *fakeCodedError) Code() string { return e.code }

func TestDegree(t *testing.T) {
	loc := testLocation(t)
	now := time.Date(2026, 1, 24, 14, 35, 32, 0, loc)
	degree := &fakeDegree{degree: 33.4321}
	srv, db := newTestServer(t, func(deps *Deps) {
		deps.Degree = degree
	})
	token := db.addToken("reader", "read")

	rec := doRequest(t, srv, http.MethodGet, "/v1/degree", token, "", nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("无活动房间状态码 = %d, 期望 409", rec.Code)
	}
	if code := problemOf(t, rec).Code; code != protocol.CodeRoomNotConfigured {
		t.Fatalf("code = %q", code)
	}

	room := db.addRoom("4408_MH_83_257", 102, "new-83_MH", now)
	db.activeID = room.ID
	rec = doRequest(t, srv, http.MethodGet, "/v1/degree", token, "", nil)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("无会话状态码 = %d, 期望 502", rec.Code)
	}
	if code := problemOf(t, rec).Code; code != protocol.CodeECNUNotLoggedIn {
		t.Fatalf("code = %q", code)
	}

	if err := db.SaveSession(t.Context(), store.Session{JSESSIONID: "x", State: "ok", UpdatedAt: now}); err != nil {
		t.Fatalf("保存会话失败: %v", err)
	}
	rec = doRequest(t, srv, http.MethodGet, "/v1/degree", token, "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, body=%s", rec.Code, rec.Body.String())
	}
	var result protocol.Degree
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("解析 degree 失败: %v", err)
	}
	if result.Degree != 33.43 || result.Source != "upstream" {
		t.Fatalf("degree 响应 = %+v", result)
	}
	if result.CachedUntil == nil || degree.calls != 1 {
		t.Fatalf("首次查询应写缓存, calls=%d", degree.calls)
	}

	rec = doRequest(t, srv, http.MethodGet, "/v1/degree", token, "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("缓存命中状态码 = %d", rec.Code)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("解析 degree 失败: %v", err)
	}
	if result.Source != "cache" || degree.calls != 1 {
		t.Fatalf("缓存命中响应 = %+v, calls=%d", result, degree.calls)
	}

	rec = doRequest(t, srv, http.MethodGet, "/v1/degree?fresh=true", token, "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("fresh 查询状态码 = %d", rec.Code)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("解析 degree 失败: %v", err)
	}
	if result.Source != "upstream" || degree.calls != 2 {
		t.Fatalf("fresh 查询响应 = %+v, calls=%d", result, degree.calls)
	}

	degree.err = &fakeCodedError{code: protocol.CodeECNUNotLoggedIn}
	rec = doRequest(t, srv, http.MethodGet, "/v1/degree?fresh=true", token, "", nil)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("上游失败状态码 = %d, 期望 502", rec.Code)
	}
	if code := problemOf(t, rec).Code; code != protocol.CodeECNUNotLoggedIn {
		t.Fatalf("上游失败 code = %q", code)
	}
}

func TestRooms(t *testing.T) {
	monitor := &fakeMonitor{}
	srv, db := newTestServer(t, func(deps *Deps) {
		deps.Monitor = monitor
	})
	token := db.addToken("writer", "read", "write")

	rec := doRequest(t, srv, http.MethodPost, "/v1/rooms", token,
		`{"room_no":"a/b","elcarea":102,"elcbuis":"new-83_MH"}`, nil)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("非法 room_no 状态码 = %d, 期望 422", rec.Code)
	}

	rec = doRequest(t, srv, http.MethodPost, "/v1/rooms", token,
		`{"room_no":"4408_MH_83_257","elcarea":102,"elcbuis":"new-83_MH"}`, nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("新建房间状态码 = %d, body=%s", rec.Code, rec.Body.String())
	}
	if monitor.woken == 0 {
		t.Fatal("切换活动房间应唤醒采集循环")
	}

	rec = doRequest(t, srv, http.MethodPost, "/v1/rooms", token,
		`{"room_no":"4408_MH_83_257","elcarea":102,"elcbuis":"new-83_MH"}`, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("复用房间状态码 = %d, 期望 200", rec.Code)
	}

	rec = doRequest(t, srv, http.MethodGet, "/v1/rooms/active", token, "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("活动房间状态码 = %d", rec.Code)
	}
	var active protocol.Room
	if err := json.Unmarshal(rec.Body.Bytes(), &active); err != nil {
		t.Fatalf("解析房间失败: %v", err)
	}
	if !active.Active || active.SampleCount != 0 {
		t.Fatalf("活动房间 = %+v", active)
	}

	rec = doRequest(t, srv, http.MethodDelete, "/v1/rooms/active", token, "", nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("取消活动房间状态码 = %d", rec.Code)
	}
	rec = doRequest(t, srv, http.MethodGet, "/v1/rooms/active", token, "", nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("无活动房间状态码 = %d, 期望 409", rec.Code)
	}

	rec = doRequest(t, srv, http.MethodGet, "/v1/rooms", token, "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("房间列表状态码 = %d", rec.Code)
	}
	var list roomListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("解析房间列表失败: %v", err)
	}
	if len(list.Rooms) != 1 {
		t.Fatalf("房间数 = %d", len(list.Rooms))
	}
}

func TestArchives(t *testing.T) {
	loc := testLocation(t)
	start := time.Date(2026, 1, 1, 10, 0, 0, 0, loc)
	srv, db := newTestServer(t, nil)
	roomA := db.addRoom("4408_MH_83_257", 102, "new-83_MH", start)
	roomB := db.addRoom("4409_MH_83_257", 102, "new-83_MH", start)
	db.activeID = roomA.ID
	db.addArchive(store.ArchiveMeta{
		RoomID: roomA.ID, Name: "a-range",
		StartTime: start, EndTime: start.Add(time.Hour), RecordsNum: 2, CreatedAt: start.Add(2 * time.Hour),
	}, []store.Sample{{Time: start, Degree: 30}, {Time: start.Add(time.Hour), Degree: 29}})
	db.addArchive(store.ArchiveMeta{
		RoomID: roomB.ID, Name: "b-range",
		StartTime: start.Add(-time.Hour), EndTime: start, RecordsNum: 1, CreatedAt: start.Add(2 * time.Hour),
	}, []store.Sample{{Time: start.Add(-time.Hour), Degree: 31}})
	token := db.addToken("writer", "read", "write")

	rec := doRequest(t, srv, http.MethodGet, "/v1/archives?room_id=all", token, "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("归档列表状态码 = %d, body=%s", rec.Code, rec.Body.String())
	}
	var list archiveListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("解析归档列表失败: %v", err)
	}
	if len(list.Archives) != 2 {
		t.Fatalf("归档数 = %d, 期望 2", len(list.Archives))
	}
	if list.Archives[0].Name != "b-range" || list.Archives[1].Name != "a-range" {
		t.Fatalf("归档顺序 = %+v", list.Archives)
	}
	if list.Archives[0].RoomID != roomB.ID || list.Archives[1].RoomID != roomA.ID {
		t.Fatalf("归档房间 = %+v", list.Archives)
	}

	rec = doRequest(t, srv, http.MethodGet, "/v1/archives", token, "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("默认房间归档状态码 = %d", rec.Code)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("解析归档列表失败: %v", err)
	}
	if len(list.Archives) != 1 || list.Archives[0].Name != "a-range" {
		t.Fatalf("默认房间归档 = %+v", list.Archives)
	}

	// 用活动房间样本创建归档.
	db.addSample(roomA.ID, start.Add(24*time.Hour), 28.5)
	db.addSample(roomA.ID, start.Add(25*time.Hour), 28)
	body := `{"from":"2026-01-02T00:00:00+08:00","to":"2026-01-02T23:59:59+08:00","name":"c-range"}`
	rec = doRequest(t, srv, http.MethodPost, "/v1/archives", token, body, nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("创建归档状态码 = %d, body=%s", rec.Code, rec.Body.String())
	}
	var created protocol.ArchiveMeta
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("解析归档失败: %v", err)
	}
	if created.Name != "c-range" || created.RecordsNum != 2 || created.RoomID != roomA.ID {
		t.Fatalf("创建的归档 = %+v", created)
	}

	rec = doRequest(t, srv, http.MethodPost, "/v1/archives", token, body, nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("重名归档状态码 = %d, 期望 409", rec.Code)
	}
	if code := problemOf(t, rec).Code; code != protocol.CodeArchiveExists {
		t.Fatalf("重名归档 code = %q", code)
	}

	rec = doRequest(t, srv, http.MethodGet, "/v1/archives/c-range", token, "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("下载归档状态码 = %d, body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Disposition"); !strings.Contains(got, "filename*=UTF-8''c-range.csv") {
		t.Fatalf("Content-Disposition = %q", got)
	}
	if rows := strings.Split(strings.TrimSpace(rec.Body.String()), "\n"); len(rows) != 2 {
		t.Fatalf("归档 CSV 行数 = %d, body=%q", len(rows), rec.Body.String())
	}

	rec = doRequest(t, srv, http.MethodDelete, "/v1/archives/c-range", token, "", nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("删除归档状态码 = %d", rec.Code)
	}
	rec = doRequest(t, srv, http.MethodDelete, "/v1/archives/c-range", token, "", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("重复删除状态码 = %d, 期望 404", rec.Code)
	}
}

func TestMethodNotAllowed(t *testing.T) {
	srv, db := newTestServer(t, nil)
	token := db.addToken("reader", "read")
	rec := doRequest(t, srv, http.MethodPost, "/v1/status", token, "", nil)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("状态码 = %d, 期望 405", rec.Code)
	}
	if got := rec.Header().Get("Allow"); got != "GET" {
		t.Fatalf("Allow = %q", got)
	}
	if code := problemOf(t, rec).Code; code != protocol.CodeMethodNotAllowed {
		t.Fatalf("code = %q", code)
	}
}

// fakeCatalog 是测试用的目录数据源.
type fakeCatalog struct {
	districts protocol.Districts
	err       error
}

func (c *fakeCatalog) Districts(ctx context.Context) (protocol.Districts, error) {
	return c.districts, c.err
}

func (c *fakeCatalog) Buildings(ctx context.Context, areaID, districtID string) (protocol.Buildings, error) {
	return protocol.Buildings{Buildings: []protocol.Building{{BuildingID: "new-83_MH", BuildingName: "闵行本科生4号楼"}}}, c.err
}

func (c *fakeCatalog) Floors(ctx context.Context, areaID, districtID, buildingID string) (protocol.Floors, error) {
	return protocol.Floors{Floors: []protocol.Floor{{FloorID: "257", FloorName: "4"}}}, c.err
}

func (c *fakeCatalog) Rooms(ctx context.Context, areaID, districtID, buildingID, floorID string) (protocol.CatalogRooms, error) {
	return protocol.CatalogRooms{Rooms: []protocol.CatalogRoom{{RoomID: "4408_MH_83_257", RoomName: "4408"}}}, c.err
}

func (c *fakeCatalog) RoomInfo(ctx context.Context, room store.Room) (RoomInfo, error) {
	return RoomInfo{
		Area:     protocol.Area{AreaID: "102", AreaName: "华东师范大学"},
		District: protocol.District{DistrictID: "MH", DistrictName: "剑川路公寓"},
		Building: protocol.Building{BuildingID: room.Elcbuis, BuildingName: "闵行本科生4号楼"},
		Floor:    protocol.Floor{FloorID: "257", FloorName: "4"},
		Room:     protocol.CatalogRoom{RoomID: room.RoomNo, RoomName: "4408"},
	}, c.err
}

func TestCatalog(t *testing.T) {
	catalog := &fakeCatalog{districts: protocol.Districts{
		Areas:     []protocol.Area{{AreaID: "102", AreaName: "华东师范大学"}},
		Districts: []protocol.District{{DistrictID: "MH", DistrictName: "剑川路公寓"}},
	}}
	srv, db := newTestServer(t, func(deps *Deps) {
		deps.Catalog = catalog
	})
	token := db.addToken("writer", "read", "write")
	loc := testLocation(t)
	room := db.addRoom("4408_MH_83_257", 102, "new-83_MH", time.Date(2026, 1, 1, 0, 0, 0, 0, loc))

	rec := doRequest(t, srv, http.MethodGet, "/v1/ecnu/districts", token, "", nil)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("无会话状态码 = %d, 期望 502", rec.Code)
	}
	if code := problemOf(t, rec).Code; code != protocol.CodeECNUNotLoggedIn {
		t.Fatalf("code = %q", code)
	}

	body := `{"jsession_id":"a","cookie":"b","x_csrf_token":"c"}`
	if rec := doRequest(t, srv, http.MethodPut, "/v1/session", token, body, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("上传会话状态码 = %d", rec.Code)
	}
	rec = doRequest(t, srv, http.MethodGet, "/v1/ecnu/districts", token, "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("目录状态码 = %d, body=%s", rec.Code, rec.Body.String())
	}
	var districts protocol.Districts
	if err := json.Unmarshal(rec.Body.Bytes(), &districts); err != nil {
		t.Fatalf("解析目录失败: %v", err)
	}
	if len(districts.Areas) != 1 || districts.Areas[0].AreaID != "102" {
		t.Fatalf("目录数据 = %+v", districts)
	}

	rec = doRequest(t, srv, http.MethodGet, "/v1/ecnu/buildings", token, "", nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("缺少参数状态码 = %d, 期望 400", rec.Code)
	}

	rec = doRequest(t, srv, http.MethodGet, "/v1/rooms/"+strconv.FormatInt(room.ID, 10)+"/info", token, "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("房间信息状态码 = %d, body=%s", rec.Code, rec.Body.String())
	}
	var info RoomInfo
	if err := json.Unmarshal(rec.Body.Bytes(), &info); err != nil {
		t.Fatalf("解析房间信息失败: %v", err)
	}
	if info.Room.RoomID != room.RoomNo || info.Building.BuildingID != room.Elcbuis {
		t.Fatalf("房间信息 = %+v", info)
	}
}

func TestPanicRecovered(t *testing.T) {
	srv, db := newTestServer(t, nil)
	db.panicOnStats = true
	token := db.addToken("reader", "read")
	rec := doRequest(t, srv, http.MethodGet, "/v1/status", token, "", nil)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("panic 后状态码 = %d, 期望 500", rec.Code)
	}
	if code := problemOf(t, rec).Code; code != protocol.CodeInternal {
		t.Fatalf("panic 后 code = %q", code)
	}
}

func TestGzipOnlyForLargeJSON(t *testing.T) {
	loc := testLocation(t)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, loc)
	srv, db := newTestServer(t, nil)
	room := db.addRoom("4408_MH_83_257", 102, "new-83_MH", base)
	db.activeID = room.ID
	for i := 0; i < 200; i++ {
		db.addSample(room.ID, base.Add(time.Duration(i)*time.Minute), 30.5)
	}
	token := db.addToken("reader", "read")
	headers := map[string]string{"Accept-Encoding": "gzip"}

	rec := doRequest(t, srv, http.MethodGet, "/v1/health", token, "", headers)
	if got := rec.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("小响应不应压缩, Content-Encoding=%q", got)
	}

	rec = doRequest(t, srv, http.MethodGet, "/v1/records", token, "", headers)
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("大 JSON 响应应压缩, Content-Encoding=%q", got)
	}
	reader, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatalf("解压失败: %v", err)
	}
	defer reader.Close()
	var records protocol.Records
	if err := json.NewDecoder(reader).Decode(&records); err != nil {
		t.Fatalf("解析解压后的 records 失败: %v", err)
	}
	if len(records.Records) != 200 {
		t.Fatalf("记录数 = %d, 期望 200", len(records.Records))
	}

	rec = doRequest(t, srv, http.MethodGet, "/v1/records?format=csv", token, "", headers)
	if got := rec.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("CSV 不应压缩, Content-Encoding=%q", got)
	}
}
