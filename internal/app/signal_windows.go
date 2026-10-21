//go:build windows

package app

import (
	"context"
	"os"
	"os/signal"
)

func notifyContext(parent context.Context) (context.Context, context.CancelFunc) {
	return signal.NotifyContext(parent, os.Interrupt)
}
