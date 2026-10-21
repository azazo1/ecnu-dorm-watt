//go:build !windows

package cli

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

// signalContext 返回响应 SIGINT 与 SIGTERM 的上下文.
func signalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}
