package notify

import (
	"crypto/sha256"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/azazo1/ecnu-dorm-watt/internal/appdirs"
)

// notificationIcon 是带透明留白的 256x256 通知图标.
// 源图和生成脚本随仓库保存, 运行时不需要图像处理依赖.
//
//go:embed assets/notification.png
var notificationIcon []byte

var (
	iconOnce sync.Once
	iconPath string
	iconErr  error
)

// EnsureIcon 将内嵌图标写入运行目录, 返回原生通知接口所需的文件路径.
func EnsureIcon() (string, error) {
	iconOnce.Do(func() {
		dir, err := appdirs.ClientRunDir()
		if err != nil {
			iconErr = err
			return
		}
		// 文件名随图像内容变化, 更新程序后不再命中旧图标缓存.
		sum := sha256.Sum256(notificationIcon)
		path := filepath.Join(dir, fmt.Sprintf("notify-icon-%x.png", sum[:8]))
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() && info.Size() == int64(len(notificationIcon)) {
			iconPath = path
			return
		}
		if err := os.WriteFile(path, notificationIcon, 0o600); err != nil {
			iconErr = err
			return
		}
		iconPath = path
	})
	return iconPath, iconErr
}
