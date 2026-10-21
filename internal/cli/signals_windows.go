//go:build windows

package cli

import (
	"context"
	"os"
	"os/signal"
)

// signalContext 返回响应 Ctrl+C 的上下文.
func signalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt)
}
