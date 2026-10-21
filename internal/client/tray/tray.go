//go:build !notray

// Package tray 提供 dormwatt client serve 的系统托盘.
//
// 托盘事件循环必须占用主 goroutine, HTTP 与轮询在后台运行.
// 使用 notray 构建标签时, 本文件被 tray_notray.go 替代.
package tray

import (
	"context"
	"runtime"
	"sync"

	"fyne.io/systray"
)

// Tray 是托盘控制器.
type Tray struct {
	appName string
	actions Actions

	mu    sync.Mutex
	state State
	menu  *menuItems
	ready bool
}

type menuItems struct {
	degree  *systray.MenuItem
	status  *systray.MenuItem
	open    *systray.MenuItem
	refresh *systray.MenuItem
	login   *systray.MenuItem
	quit    *systray.MenuItem
}

// New 创建托盘控制器.
func New(appName string, actions Actions) *Tray {
	if appName == "" {
		appName = "DormWatt"
	}
	return &Tray{appName: appName, actions: actions}
}

// Available 表示当前构建是否包含托盘能力.
func Available() bool { return true }

// Update 更新状态; 可以在任意 goroutine 调用.
func (t *Tray) Update(state State) {
	t.mu.Lock()
	t.state = state
	t.mu.Unlock()
	t.apply()
}

// Run 占用主 goroutine 运行托盘事件循环, ctx 取消时退出.
func (t *Tray) Run(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return nil
	}
	exited := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			systray.Quit()
		case <-exited:
		}
	}()
	systray.Run(t.onReady, func() {
		t.mu.Lock()
		t.ready = false
		t.mu.Unlock()
		close(exited)
	})
	return nil
}

func (t *Tray) onReady() {
	m := &menuItems{}
	m.degree = systray.AddMenuItem("当前电量: --", "当前剩余电量")
	m.degree.Disable()
	m.status = systray.AddMenuItem("状态: 启动中", "守护进程状态")
	m.status.Disable()
	systray.AddSeparator()
	m.open = systray.AddMenuItem("打开 Web 界面", "在默认浏览器中打开本地界面")
	m.refresh = systray.AddMenuItem("立即刷新", "触发一次本地状态轮询")
	m.login = systray.AddMenuItem("登录帮助", "打开本地设置页的登录说明")
	systray.AddSeparator()
	m.quit = systray.AddMenuItem("退出", "退出 DormWatt")

	t.mu.Lock()
	t.menu = m
	t.ready = true
	t.mu.Unlock()

	go t.loop(m.open.ClickedCh, t.actions.OpenWeb)
	go t.loop(m.refresh.ClickedCh, t.actions.Refresh)
	go t.loop(m.login.ClickedCh, t.actions.LoginHelp)
	go t.loop(m.quit.ClickedCh, t.actions.Quit)

	t.apply()
}

func (t *Tray) loop(ch <-chan struct{}, fn func()) {
	if fn == nil {
		return
	}
	for range ch {
		fn()
	}
}

func (t *Tray) apply() {
	t.mu.Lock()
	ready := t.ready
	state := t.state
	m := t.menu
	t.mu.Unlock()
	if !ready || m == nil {
		return
	}
	systray.SetIcon(platformIcon(iconPNG(state.IconKind())))
	tooltip := state.TooltipFor(t.appName)
	systray.SetTooltip(tooltip)
	if runtime.GOOS == "linux" {
		// Linux 托盘通常用标题而非 tooltip 显示文本.
		systray.SetTitle(tooltip)
	} else {
		systray.SetTitle("")
	}
	m.degree.SetTitle(state.FirstItem())
	m.status.SetTitle(state.StatusItem())
}
