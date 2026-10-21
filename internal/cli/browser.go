package cli

import (
	"fmt"
	"os/exec"
	"runtime"
)

// openURL 用系统默认浏览器打开地址.
func openURL(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("打开浏览器失败: %w", err)
	}
	// 不等待浏览器退出, 避免阻塞命令.
	go func() { _ = cmd.Wait() }()
	return nil
}
