//go:build !windows

package localapi

import (
	"os"
	"syscall"
)

// processAlive 用信号 0 判断进程是否存在.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return p.Signal(syscall.Signal(0)) == nil
}
