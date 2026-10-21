// Package login 提供 ECNU 登录辅助.
//
// 浏览器模式只负责打开页面, 等待用户完成 SSO, 读取 cookies 与 meta 中的
// CSRF token. 它不发送任何请求到 DormWatt 服务端.
package login

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
)

const (
	// BillURL 是 ECNU 电费查询入口.
	BillURL = "https://epay.ecnu.edu.cn/epaycas/electric/load4electricbill?elcsysid=1"
	// SSOHost 是统一身份认证主机.
	SSOHost = "sso.ecnu.edu.cn"
	// DefaultTimeout 是等待用户完成登录的默认上限.
	DefaultTimeout = 5 * time.Minute
)

// Credentials 是从浏览器读取的会话三元组.
type Credentials struct {
	JSESSIONID string
	Cookie     string
	CSRFToken  string
}

// Complete 报告三个字段是否都非空.
func (c Credentials) Complete() bool {
	return strings.TrimSpace(c.JSESSIONID) != "" &&
		strings.TrimSpace(c.Cookie) != "" &&
		strings.TrimSpace(c.CSRFToken) != ""
}

// Options 是浏览器登录参数.
type Options struct {
	// URL 为空时使用 BillURL.
	URL string
	// BrowserPath 为空时由 launcher 自动查找 Chrome 或 Chromium.
	BrowserPath string
	// Timeout 为 0 时使用 DefaultTimeout.
	Timeout time.Duration
	// Logf 用于输出进度, 可为空.
	Logf func(format string, args ...any)
}

// Browser 打开有头浏览器, 等待用户完成 SSO, 返回会话凭据.
func Browser(ctx context.Context, opts Options) (Credentials, error) {
	target := strings.TrimSpace(opts.URL)
	if target == "" {
		target = BillURL
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	logf := opts.Logf
	if logf == nil {
		logf = func(string, ...any) {}
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	l := launcher.New().Headless(false).Leakless(false)
	if path := strings.TrimSpace(opts.BrowserPath); path != "" {
		l = l.Bin(path)
	}
	controlURL, err := l.Launch()
	if err != nil {
		return Credentials{}, fmt.Errorf("启动 Chrome 失败: %w", err)
	}

	browser := rod.New().ControlURL(controlURL)
	if err := browser.Connect(); err != nil {
		return Credentials{}, fmt.Errorf("连接 Chrome 失败: %w", err)
	}
	defer browser.Close()

	page, err := browser.Page(proto.TargetCreateTarget{URL: target})
	if err != nil {
		return Credentials{}, fmt.Errorf("打开电费页面失败: %w", err)
	}
	defer page.Close()

	logf("已打开 ECNU 电费页面, 请在浏览器中完成登录")

	if err := waitForSSO(ctx, page); err != nil {
		return Credentials{}, err
	}
	logf("登录跳转完成, 正在读取会话凭据")

	creds, err := readCredentials(browser, page)
	if err != nil {
		return Credentials{}, err
	}
	if !creds.Complete() {
		return Credentials{}, fmt.Errorf("未能完整读取 JSESSIONID, cookie 与 X-CSRF-TOKEN")
	}
	return creds, nil
}

func waitForSSO(ctx context.Context, page *rod.Page) error {
	ticker := time.NewTicker(300 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("等待登录超时: %w", ctx.Err())
		case <-ticker.C:
		}
		info, err := page.Info()
		if err != nil {
			return fmt.Errorf("读取浏览器页面失败: %w", err)
		}
		u := strings.ToLower(info.URL)
		if u == "" || strings.HasPrefix(u, "about:") {
			continue
		}
		if strings.Contains(u, SSOHost) {
			continue
		}
		hasError, _, err := page.Has("#main-frame-error")
		if err != nil {
			return fmt.Errorf("检查页面状态失败: %w", err)
		}
		if hasError {
			return fmt.Errorf("浏览器页面显示网络错误, 请检查网络后重试")
		}
		return nil
	}
}

func readCredentials(browser *rod.Browser, page *rod.Page) (Credentials, error) {
	var creds Credentials
	deadline := time.Now().Add(5 * time.Second)
	for {
		cookies, err := browser.GetCookies()
		if err != nil {
			return Credentials{}, fmt.Errorf("读取浏览器 cookies 失败: %w", err)
		}
		for _, cookie := range cookies {
			switch cookie.Name {
			case "JSESSIONID":
				creds.JSESSIONID = cookie.Value
			case "cookie":
				creds.Cookie = cookie.Value
			}
		}
		if creds.JSESSIONID != "" && creds.Cookie != "" {
			break
		}
		if time.Now().After(deadline) {
			return creds, fmt.Errorf("浏览器中缺少 JSESSIONID 或 cookie")
		}
		time.Sleep(200 * time.Millisecond)
	}

	meta, err := page.ElementX("//html/head/meta[4]")
	if err != nil {
		return Credentials{}, fmt.Errorf("查找 X-CSRF-TOKEN 元素失败: %w", err)
	}
	content, err := meta.Attribute("content")
	if err != nil {
		return Credentials{}, fmt.Errorf("读取 X-CSRF-TOKEN 失败: %w", err)
	}
	if content == nil || strings.TrimSpace(*content) == "" {
		return Credentials{}, fmt.Errorf("页面 meta[4] 中缺少 X-CSRF-TOKEN")
	}
	creds.CSRFToken = strings.TrimSpace(*content)
	return creds, nil
}
