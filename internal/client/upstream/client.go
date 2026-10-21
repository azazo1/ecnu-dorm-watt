// Package upstream 实现访问 DormWatt 服务端 /v1 API 的 HTTP 客户端.
//
// 客户端只依赖标准库与 internal/protocol 中的线格式类型, 不引入服务端内部包.
package upstream

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/azazo1/ecnu-dorm-watt/internal/protocol"
)

const (
	// DefaultTimeout 是未配置时的请求超时.
	DefaultTimeout = 20 * time.Second
	// DefaultUserAgent 是默认 User-Agent.
	DefaultUserAgent = "dormwatt-client/0.1"
	// maxResponseBytes 限制 JSON 响应读取大小.
	maxResponseBytes = 8 << 20
	// maxProblemBytes 限制错误响应读取大小.
	maxProblemBytes = 64 << 10
)

// Config 是上游客户端配置.
type Config struct {
	BaseURL   string
	Token     string
	Timeout   time.Duration
	UserAgent string
}

// Client 是服务端 HTTP 客户端.
type Client struct {
	baseURL string
	token   string
	ua      string
	http    *http.Client
}

// New 创建上游客户端, 并校验 base URL.
func New(cfg Config) (*Client, error) {
	base := strings.TrimSpace(cfg.BaseURL)
	if base == "" {
		return nil, errors.New("服务端地址为空, 请先执行 dormwatt client config set-server")
	}
	u, err := url.Parse(base)
	if err != nil {
		return nil, fmt.Errorf("服务端地址无法解析: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("服务端地址必须使用 http 或 https, 当前为 %q", u.Scheme)
	}
	if u.Host == "" {
		return nil, errors.New("服务端地址缺少主机名")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("服务端地址不允许包含查询参数或片段")
	}
	u.Path = strings.TrimSuffix(u.Path, "/")

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	ua := cfg.UserAgent
	if ua == "" {
		ua = DefaultUserAgent
	}
	return &Client{
		baseURL: u.String(),
		token:   cfg.Token,
		ua:      ua,
		http:    &http.Client{Timeout: timeout},
	}, nil
}

// BaseURL 返回规范化后的服务端基址.
func (c *Client) BaseURL() string { return c.baseURL }

// Token 返回当前 token, 仅用于内部诊断, 不要输出到日志.
func (c *Client) Token() string { return c.token }

// endpoint 在保留 base 路径的前提下拼接 /v1 路径与查询参数.
//
// JoinBaseURL 会把 suffix 当作未转义的 URL 路径, 因此这里先解码一次,
// 避免归档名称中的空格与中文被二次转义.
func (c *Client) endpoint(path string, query url.Values) (string, error) {
	if decoded, err := url.PathUnescape(path); err == nil {
		path = decoded
	}
	joined, err := protocol.JoinBaseURL(c.baseURL, path)
	if err != nil {
		return "", fmt.Errorf("拼接请求地址失败: %w", err)
	}
	if len(query) == 0 {
		return joined, nil
	}
	u, err := url.Parse(joined)
	if err != nil {
		return "", fmt.Errorf("拼接请求地址失败: %w", err)
	}
	u.RawQuery = query.Encode()
	return u.String(), nil
}

// newRequest 构造带鉴权头的请求.
func (c *Client) newRequest(ctx context.Context, method, path string, query url.Values, body []byte, contentType string) (*http.Request, error) {
	target, err := c.endpoint(path, query)
	if err != nil {
		return nil, err
	}
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, reader)
	if err != nil {
		return nil, fmt.Errorf("构造请求失败: %w", err)
	}
	req.Header.Set("User-Agent", c.ua)
	req.Header.Set("Accept", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	if body != nil {
		if contentType == "" {
			contentType = "application/json"
		}
		req.Header.Set("Content-Type", contentType)
	}
	return req, nil
}

// send 执行请求并把传输错误转换为 TransportError.
func (c *Client) send(req *http.Request) (*http.Response, error) {
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, &TransportError{
			Op:  req.Method + " " + req.URL.Path,
			Err: err,
			TLS: isTLSError(err),
		}
	}
	return resp, nil
}

// doJSON 发送请求并解析 JSON 响应; out 为 nil 时丢弃响应体.
func (c *Client) doJSON(ctx context.Context, method, path string, query url.Values, body any, out any) error {
	var payload []byte
	if body != nil {
		var err error
		payload, err = json.Marshal(body)
		if err != nil {
			return fmt.Errorf("编码请求体失败: %w", err)
		}
	}
	req, err := c.newRequest(ctx, method, path, query, payload, "")
	if err != nil {
		return err
	}
	resp, err := c.send(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return decodeProblem(resp)
	}
	if out == nil {
		io.Copy(io.Discard, io.LimitReader(resp.Body, maxProblemBytes))
		return nil
	}
	dec := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes))
	if err := dec.Decode(out); err != nil {
		return fmt.Errorf("解析服务端响应失败: %w", err)
	}
	return nil
}

// getJSON 是 GET + 解析 JSON 的便捷封装.
func (c *Client) getJSON(ctx context.Context, path string, query url.Values, out any) error {
	return c.doJSON(ctx, http.MethodGet, path, query, nil, out)
}

// decodeProblem 读取并解析 problem+json 错误响应.
func decodeProblem(resp *http.Response) error {
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxProblemBytes))
	if err != nil {
		return fmt.Errorf("读取错误响应失败: %w", err)
	}
	ae := &APIError{Status: resp.StatusCode}
	var p protocol.Problem
	if json.Unmarshal(raw, &p) == nil && p.Code != "" {
		ae.Problem = p
	} else {
		ae.Body = summarizeBody(raw)
	}
	return ae
}

func summarizeBody(raw []byte) string {
	text := strings.TrimSpace(string(raw))
	text = strings.ReplaceAll(text, "\n", " ")
	if len(text) > 200 {
		text = text[:200] + "..."
	}
	return text
}

// DoRaw 透传请求, 返回原始响应. 传输错误转换为 TransportError, 非 2xx 也原样返回.
func (c *Client) DoRaw(ctx context.Context, method, path string, query url.Values, body []byte, contentType string) (*http.Response, error) {
	req, err := c.newRequest(ctx, method, path, query, body, contentType)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "*/*")
	return c.send(req)
}

// HealthInfo 是 GET /v1/health 的响应.
type HealthInfo struct {
	Status  string `json:"status"`
	Version string `json:"version"`
}

// Health 调用 GET /v1/health.
func (c *Client) Health(ctx context.Context) (*HealthInfo, error) {
	var out HealthInfo
	if err := c.getJSON(ctx, "/v1/health", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Whoami 调用 GET /v1/whoami.
func (c *Client) Whoami(ctx context.Context) (*protocol.WhoAmI, error) {
	var out protocol.WhoAmI
	if err := c.getJSON(ctx, "/v1/whoami", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Status 调用 GET /v1/status.
func (c *Client) Status(ctx context.Context) (*protocol.Status, error) {
	var out protocol.Status
	if err := c.getJSON(ctx, "/v1/status", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Degree 调用 GET /v1/degree.
func (c *Client) Degree(ctx context.Context, fresh bool) (*protocol.Degree, error) {
	q := url.Values{}
	if fresh {
		q.Set("fresh", "true")
	}
	var out protocol.Degree
	if err := c.getJSON(ctx, "/v1/degree", q, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// RecordsOptions 是记录查询参数.
type RecordsOptions struct {
	RoomID int64
	From   *time.Time
	To     *time.Time
	Limit  int
	Format string
	Header bool
}

func (o RecordsOptions) query() url.Values {
	q := url.Values{}
	if o.RoomID > 0 {
		q.Set("room_id", fmt.Sprintf("%d", o.RoomID))
	}
	if o.From != nil {
		q.Set("from", protocol.FormatTime(*o.From, o.From.Location()))
	}
	if o.To != nil {
		q.Set("to", protocol.FormatTime(*o.To, o.To.Location()))
	}
	if o.Limit > 0 {
		q.Set("limit", fmt.Sprintf("%d", o.Limit))
	}
	if o.Format != "" && o.Format != "json" {
		q.Set("format", o.Format)
	}
	if o.Header {
		q.Set("header", "true")
	}
	return q
}

// Records 调用 GET /v1/records 并解析 JSON.
func (c *Client) Records(ctx context.Context, opts RecordsOptions) (*protocol.Records, error) {
	var out protocol.Records
	if err := c.getJSON(ctx, "/v1/records", opts.query(), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// RecordsCSV 调用 GET /v1/records?format=csv 并流式写入 w.
func (c *Client) RecordsCSV(ctx context.Context, opts RecordsOptions, w io.Writer) error {
	q := opts.query()
	q.Set("format", "csv")
	req, err := c.newRequest(ctx, http.MethodGet, "/v1/records", q, nil, "")
	if err != nil {
		return err
	}
	resp, err := c.send(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return decodeProblem(resp)
	}
	_, err = io.Copy(w, resp.Body)
	return err
}

// RoomsResponse 是 GET /v1/rooms 的响应.
type RoomsResponse struct {
	Rooms []protocol.Room `json:"rooms"`
}

// Rooms 调用 GET /v1/rooms.
func (c *Client) Rooms(ctx context.Context) ([]protocol.Room, error) {
	var out RoomsResponse
	if err := c.getJSON(ctx, "/v1/rooms", nil, &out); err != nil {
		return nil, err
	}
	return out.Rooms, nil
}

// ActiveRoom 调用 GET /v1/rooms/active. 没有活动房间时返回 room_not_configured 错误.
func (c *Client) ActiveRoom(ctx context.Context) (*protocol.Room, error) {
	var out protocol.Room
	if err := c.getJSON(ctx, "/v1/rooms/active", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateRoomRequest 是 POST /v1/rooms 的请求体.
type CreateRoomRequest struct {
	RoomNo   string `json:"room_no"`
	Elcarea  int    `json:"elcarea"`
	Elcbuis  string `json:"elcbuis"`
	Activate *bool  `json:"activate,omitempty"`
}

// CreateRoom 调用 POST /v1/rooms, 返回房间与是否新建.
func (c *Client) CreateRoom(ctx context.Context, reqBody CreateRoomRequest) (*protocol.Room, bool, error) {
	payload, err := json.Marshal(reqBody)
	if err != nil {
		return nil, false, fmt.Errorf("编码请求体失败: %w", err)
	}
	req, err := c.newRequest(ctx, http.MethodPost, "/v1/rooms", nil, payload, "")
	if err != nil {
		return nil, false, err
	}
	resp, err := c.send(req)
	if err != nil {
		return nil, false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, false, decodeProblem(resp)
	}
	var out protocol.Room
	dec := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes))
	if err := dec.Decode(&out); err != nil {
		return nil, false, fmt.Errorf("解析服务端响应失败: %w", err)
	}
	return &out, resp.StatusCode == http.StatusCreated, nil
}

// SetActiveRoom 调用 PUT /v1/rooms/{id}/active.
func (c *Client) SetActiveRoom(ctx context.Context, id int64) error {
	return c.doJSON(ctx, http.MethodPut, fmt.Sprintf("/v1/rooms/%d/active", id), nil, nil, nil)
}

// ClearActiveRoom 调用 DELETE /v1/rooms/active.
func (c *Client) ClearActiveRoom(ctx context.Context) error {
	return c.doJSON(ctx, http.MethodDelete, "/v1/rooms/active", nil, nil, nil)
}

// RoomInfo 是 GET /v1/rooms/{id}/info 的响应.
type RoomInfo struct {
	Area     protocol.Area        `json:"area"`
	District protocol.District    `json:"district"`
	Building protocol.Building    `json:"building"`
	Floor    protocol.Floor       `json:"floor"`
	Room     protocol.CatalogRoom `json:"room"`
}

// RoomInfo 调用 GET /v1/rooms/{id}/info.
func (c *Client) RoomInfo(ctx context.Context, id int64) (*RoomInfo, error) {
	var out RoomInfo
	if err := c.getJSON(ctx, fmt.Sprintf("/v1/rooms/%d/info", id), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Districts 调用 GET /v1/ecnu/districts.
func (c *Client) Districts(ctx context.Context) (*protocol.Districts, error) {
	var out protocol.Districts
	if err := c.getJSON(ctx, "/v1/ecnu/districts", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Buildings 调用 GET /v1/ecnu/buildings.
func (c *Client) Buildings(ctx context.Context, areaID, districtID string) (*protocol.Buildings, error) {
	q := url.Values{"area_id": {areaID}, "district_id": {districtID}}
	var out protocol.Buildings
	if err := c.getJSON(ctx, "/v1/ecnu/buildings", q, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Floors 调用 GET /v1/ecnu/floors.
func (c *Client) Floors(ctx context.Context, areaID, districtID, buildingID string) (*protocol.Floors, error) {
	q := url.Values{"area_id": {areaID}, "district_id": {districtID}, "building_id": {buildingID}}
	var out protocol.Floors
	if err := c.getJSON(ctx, "/v1/ecnu/floors", q, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CatalogRooms 调用 GET /v1/ecnu/rooms.
func (c *Client) CatalogRooms(ctx context.Context, areaID, districtID, buildingID, floorID string) (*protocol.CatalogRooms, error) {
	q := url.Values{
		"area_id":     {areaID},
		"district_id": {districtID},
		"building_id": {buildingID},
		"floor_id":    {floorID},
	}
	var out protocol.CatalogRooms
	if err := c.getJSON(ctx, "/v1/ecnu/rooms", q, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Session 调用 GET /v1/session.
func (c *Client) Session(ctx context.Context) (*protocol.SessionInfo, error) {
	var out protocol.SessionInfo
	if err := c.getJSON(ctx, "/v1/session", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// SessionRequest 是 PUT /v1/session 的请求体.
type SessionRequest struct {
	JSESSIONID string `json:"jsession_id"`
	Cookie     string `json:"cookie"`
	CSRFToken  string `json:"x_csrf_token"`
}

// PutSession 调用 PUT /v1/session.
func (c *Client) PutSession(ctx context.Context, sess SessionRequest) error {
	return c.doJSON(ctx, http.MethodPut, "/v1/session", nil, sess, nil)
}

// DeleteSession 调用 DELETE /v1/session.
func (c *Client) DeleteSession(ctx context.Context) error {
	return c.doJSON(ctx, http.MethodDelete, "/v1/session", nil, nil, nil)
}

// ArchivesResponse 是 GET /v1/archives 的响应.
type ArchivesResponse struct {
	Archives []protocol.ArchiveMeta `json:"archives"`
}

// Archives 调用 GET /v1/archives. roomID 为空时使用活动房间, 为 all 时查询全部.
func (c *Client) Archives(ctx context.Context, roomID string, includeDeleted bool) ([]protocol.ArchiveMeta, error) {
	q := url.Values{}
	if roomID != "" {
		q.Set("room_id", roomID)
	}
	if includeDeleted {
		q.Set("include_deleted", "true")
	}
	var out ArchivesResponse
	if err := c.getJSON(ctx, "/v1/archives", q, &out); err != nil {
		return nil, err
	}
	return out.Archives, nil
}

// ArchiveRequest 是 POST /v1/archives 的请求体.
type ArchiveRequest struct {
	RoomID int64  `json:"room_id,omitempty"`
	From   string `json:"from,omitempty"`
	To     string `json:"to,omitempty"`
	Name   string `json:"name,omitempty"`
}

// CreateArchive 调用 POST /v1/archives.
func (c *Client) CreateArchive(ctx context.Context, reqBody ArchiveRequest) (*protocol.ArchiveMeta, error) {
	payload, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("编码请求体失败: %w", err)
	}
	req, err := c.newRequest(ctx, http.MethodPost, "/v1/archives", nil, payload, "")
	if err != nil {
		return nil, err
	}
	resp, err := c.send(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, decodeProblem(resp)
	}
	var out protocol.ArchiveMeta
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(&out); err != nil {
		return nil, fmt.Errorf("解析服务端响应失败: %w", err)
	}
	return &out, nil
}

// DownloadArchive 调用 GET /v1/archives/{name} 并流式写入 w.
func (c *Client) DownloadArchive(ctx context.Context, name, roomID string, w io.Writer) error {
	q := url.Values{}
	if roomID != "" {
		q.Set("room_id", roomID)
	}
	path := "/v1/archives/" + url.PathEscape(name)
	req, err := c.newRequest(ctx, http.MethodGet, path, q, nil, "")
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "text/csv")
	resp, err := c.send(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return decodeProblem(resp)
	}
	_, err = io.Copy(w, resp.Body)
	return err
}

// DeleteArchive 调用 DELETE /v1/archives/{name}.
func (c *Client) DeleteArchive(ctx context.Context, name, roomID string) error {
	q := url.Values{}
	if roomID != "" {
		q.Set("room_id", roomID)
	}
	return c.doJSON(ctx, http.MethodDelete, "/v1/archives/"+url.PathEscape(name), q, nil, nil)
}
