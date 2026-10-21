package appdirs

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

const appName = "dormwatt"

// Name 返回目录名, debug 构建追加 -debug 后缀.
func Name() string {
	if isDebugBuild() {
		return appName + "-debug"
	}
	return appName
}

func isDebugBuild() bool {
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	return filepath.Base(exe) == "dormwatt-debug"
}

// ConfigDir 返回配置目录, 并确保存在.
func ConfigDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return ensure(filepath.Join(base, Name()))
}

// DataDir 返回数据目录, 并确保存在.
func DataDir() (string, error) {
	var base string
	var err error
	switch runtime.GOOS {
	case "linux", "freebsd", "openbsd", "netbsd":
		base = os.Getenv("XDG_DATA_HOME")
		if base == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", err
			}
			base = filepath.Join(home, ".local", "share")
		}
	case "windows":
		base = os.Getenv("LOCALAPPDATA")
		if base == "" {
			base, _ = os.UserConfigDir()
		}
	default:
		base, err = os.UserConfigDir()
		if err != nil {
			return "", err
		}
	}
	return ensure(filepath.Join(base, Name()))
}

// LogDir 返回日志目录.
func LogDir() (string, error) {
	data, err := DataDir()
	if err != nil {
		return "", err
	}
	return ensure(filepath.Join(data, "logs"))
}

// ClientRunDir 返回客户端运行状态目录.
func ClientRunDir() (string, error) {
	data, err := DataDir()
	if err != nil {
		return "", err
	}
	return ensure(filepath.Join(data, "client"))
}

// RuntimePath 返回本地守护进程 runtime.json 路径.
func RuntimePath() (string, error) {
	dir, err := ClientRunDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "runtime.json"), nil
}

// DatabasePath 返回服务端数据库路径.
func DatabasePath() (string, error) {
	data, err := DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(data, "dormwatt.db"), nil
}

// ServerConfigPath 返回服务端配置路径.
func ServerConfigPath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "server.toml"), nil
}

// ClientConfigPath 返回客户端配置路径.
func ClientConfigPath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "client.toml"), nil
}

func ensure(path string) (string, error) {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return "", fmt.Errorf("create %s: %w", path, err)
	}
	return path, nil
}
