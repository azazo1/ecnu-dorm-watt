package ecnu

import (
	"context"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/azazo1/ecnu-dorm-watt/internal/protocol"
)

const (
	// DefaultBaseURL 是 ECNU 缴费平台地址.
	DefaultBaseURL = "https://epay.ecnu.edu.cn"
	// DefaultUserAgent 是默认 User-Agent.
	DefaultUserAgent = "dormwatt/0.1"
	// DefaultTimeout 是默认单次请求总超时.
	DefaultTimeout = 15 * time.Second
	// MaxResponseBytes 是响应体上限, 超过时按上游错误处理.
	MaxResponseBytes = 1 << 20

	connectTimeout      = 5 * time.Second
	tlsHandshakeTimeout = 5 * time.Second
)

// Room 描述查询电费所需的房间参数.
type Room struct {
	RoomNo  string
	Elcarea int
	Elcbuis string
}

// Session 是 ECNU 上游会话的三个凭据.
type Session struct {
	JSESSIONID string
	Cookie     string
	CSRFToken  string
}

// Complete 报告三个凭据是否都已填写.
func (s Session) Complete() bool {
	return strings.TrimSpace(s.JSESSIONID) != "" &&
		strings.TrimSpace(s.Cookie) != "" &&
		strings.TrimSpace(s.CSRFToken) != ""
}

// Options 是构造 Client 的配置.
type Options struct {
	// BaseURL 可以包含路径前缀, 为空时使用 DefaultBaseURL.
	BaseURL string
	// Timeout 是单次请求总超时, 为 0 时使用 DefaultTimeout.
	Timeout time.Duration
	// UserAgent 为空时使用 DefaultUserAgent.
	UserAgent string
	// HTTPClient 可以注入自定义客户端, 用于测试或代理.
	// 注入时不会被原地修改, 重定向策略始终由本包设置.
	HTTPClient *http.Client
}

// Client 是 ECNU 上游客户端, 可被多个 goroutine 并发使用.
type Client struct {
	baseURL   *url.URL
	timeout   time.Duration
	userAgent string
	http      *http.Client
}

// New 构造客户端.
func New(opts Options) (*Client, error) {
	raw := strings.TrimSpace(opts.BaseURL)
	if raw == "" {
		raw = DefaultBaseURL
	}
	base, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("解析 ECNU base URL 失败: %w", err)
	}
	if base.Scheme != "http" && base.Scheme != "https" {
		return nil, fmt.Errorf("ECNU base URL 必须以 http 或 https 开头: %q", raw)
	}
	if base.Host == "" {
		return nil, fmt.Errorf("ECNU base URL 缺少主机名: %q", raw)
	}

	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	userAgent := strings.TrimSpace(opts.UserAgent)
	if userAgent == "" {
		userAgent = DefaultUserAgent
	}

	var httpClient *http.Client
	if opts.HTTPClient != nil {
		cloned := *opts.HTTPClient
		httpClient = &cloned
	} else {
		httpClient = &http.Client{Timeout: timeout}
	}
	if httpClient.Transport == nil {
		dialer := &net.Dialer{Timeout: connectTimeout, KeepAlive: 30 * time.Second}
		httpClient.Transport = &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			DialContext:           dialer.DialContext,
			TLSHandshakeTimeout:   tlsHandshakeTimeout,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          8,
			MaxIdleConnsPerHost:   4,
			IdleConnTimeout:       90 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		}
	}
	// 不自动跟随重定向, SSO 跳转直接归类为会话失效.
	httpClient.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}

	return &Client{
		baseURL:   base,
		timeout:   timeout,
		userAgent: userAgent,
		http:      httpClient,
	}, nil
}

// BaseURL 返回去掉末尾斜杠的 base URL.
func (c *Client) BaseURL() string {
	return strings.TrimSuffix(c.baseURL.String(), "/")
}

// post 发送表单请求并返回通过初步校验的 JSON 响应体.
func (c *Client) post(ctx context.Context, session Session, path string, form url.Values) ([]byte, error) {
	if !session.Complete() {
		return nil, fmt.Errorf("%w: 缺少会话凭据", ErrNotLoggedIn)
	}
	endpoint, err := protocol.JoinBaseURL(c.BaseURL(), path)
	if err != nil {
		return nil, fmt.Errorf("%w: 拼接请求地址失败: %v", ErrUpstream, err)
	}

	reqCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("%w: 构造请求失败: %v", ErrUpstream, err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Cookie", "JSESSIONID="+session.JSESSIONID+"; cookie="+session.Cookie)
	req.Header.Set("X-CSRF-TOKEN", session.CSRFToken)

	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("%w: 请求上游失败: %v", ErrUpstream, err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		if c.redirectLeavesHost(resp) {
			return nil, fmt.Errorf("%w: 上游跳转到 %s", ErrNotLoggedIn, resp.Header.Get("Location"))
		}
		return nil, fmt.Errorf("%w: 上游返回意外重定向 %d", ErrUpstream, resp.StatusCode)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%w: 上游返回状态码 %d", ErrUpstream, resp.StatusCode)
	}
	if !isJSONContentType(resp.Header.Get("Content-Type")) {
		return nil, fmt.Errorf("%w: 上游返回了非 JSON 响应", ErrNotLoggedIn)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxResponseBytes+1))
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("%w: 读取上游响应失败: %v", ErrUpstream, err)
	}
	if int64(len(body)) > MaxResponseBytes {
		return nil, fmt.Errorf("%w: 上游响应超过 %d 字节", ErrUpstream, MaxResponseBytes)
	}
	return body, nil
}

// redirectLeavesHost 判断重定向是否离开受信任的主机.
// 目标是 SSO 或其他主机时视为会话失效.
func (c *Client) redirectLeavesHost(resp *http.Response) bool {
	raw := strings.TrimSpace(resp.Header.Get("Location"))
	if raw == "" {
		return false
	}
	ref, err := url.Parse(raw)
	if err != nil {
		return false
	}
	target := resp.Request.URL.ResolveReference(ref)
	host := strings.ToLower(target.Hostname())
	if host == "" {
		return false
	}
	return host != strings.ToLower(c.baseURL.Hostname())
}

// isJSONContentType 判断 Content-Type 是否为 application/json.
func isJSONContentType(raw string) bool {
	if strings.TrimSpace(raw) == "" {
		return false
	}
	mediaType, _, err := mime.ParseMediaType(raw)
	if err != nil {
		return false
	}
	return strings.EqualFold(mediaType, "application/json")
}
