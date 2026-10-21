//go:build notray

// Package tray 在 notray 构建中提供空实现, 保证无 cgo 环境可编译.
package tray

import "context"

// Tray 是无托盘构建的占位控制器.
type Tray struct {
	actions Actions

	state State
}

// New 创建占位托盘控制器.
func New(appName string, actions Actions) *Tray {
	return &Tray{actions: actions}
}

// Available 表示当前构建不包含托盘能力.
func Available() bool { return false }

// Update 在无托盘构建中只记录状态.
func (t *Tray) Update(state State) { t.state = state }

// Run 在无托盘构建中阻塞到 ctx 取消.
func (t *Tray) Run(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	<-ctx.Done()
	return nil
}
