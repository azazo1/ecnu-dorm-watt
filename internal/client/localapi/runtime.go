package localapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"
)

// Runtime 是本地守护进程写入 runtime.json 的状态.
type Runtime struct {
	PID       int       `json:"pid"`
	Port      int       `json:"port"`
	Token     string    `json:"token"`
	StartedAt time.Time `json:"started_at"`
}

// WriteRuntime 以 0600 权限原子写入 runtime.json.
func WriteRuntime(path string, rt Runtime) error {
	if err := ValidateToken(rt.Token); err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("创建运行目录失败: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".runtime-*.json")
	if err != nil {
		return fmt.Errorf("创建临时运行文件失败: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("设置运行文件权限失败: %w", err)
	}
	enc := json.NewEncoder(tmp)
	enc.SetIndent("", "  ")
	if err := enc.Encode(rt); err != nil {
		tmp.Close()
		return fmt.Errorf("写入运行文件失败: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("同步运行文件失败: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("关闭运行文件失败: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("替换运行文件失败: %w", err)
	}
	return nil
}

// ReadRuntime 读取并校验 runtime.json.
func ReadRuntime(path string) (*Runtime, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, os.ErrNotExist
		}
		return nil, fmt.Errorf("读取运行文件失败: %w", err)
	}
	var rt Runtime
	if err := json.Unmarshal(data, &rt); err != nil {
		return nil, fmt.Errorf("解析运行文件失败: %w", err)
	}
	if rt.PID <= 0 || rt.Port <= 0 || rt.Token == "" {
		return nil, fmt.Errorf("运行文件内容不完整")
	}
	if err := ValidateToken(rt.Token); err != nil {
		return nil, err
	}
	return &rt, nil
}

// RemoveRuntime 删除 runtime.json, 文件不存在时忽略.
func RemoveRuntime(path string) error {
	err := os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// RuntimeRunning 判断运行文件对应的进程与端口是否都存活.
func RuntimeRunning(rt *Runtime) bool {
	if rt == nil {
		return false
	}
	if !processAlive(rt.PID) {
		return false
	}
	return portReachable(rt.Port)
}

func portReachable(port int) bool {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", fmt.Sprintf("%d", port)), 300*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}
