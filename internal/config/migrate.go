package config

import (
	"fmt"
	"os"
)

// MigrateServer 将旧版服务端配置迁移到当前版本.
func MigrateServer(cfg *ServerConfig, path string) (bool, error) {
	if cfg.Version > CurrentVersion {
		return false, fmt.Errorf("config version %d is newer than supported %d", cfg.Version, CurrentVersion)
	}
	if cfg.Version == CurrentVersion {
		return false, nil
	}
	backup, err := backupFile(path)
	if err != nil {
		return false, err
	}
	if backup != "" {
		fmt.Fprintf(os.Stderr, "config backup: %s\n", backup)
	}
	cfg.Version = CurrentVersion
	return true, nil
}

// MigrateClient 将旧版客户端配置迁移到当前版本.
func MigrateClient(cfg *ClientConfig, path string) (bool, error) {
	if cfg.Version > CurrentVersion {
		return false, fmt.Errorf("config version %d is newer than supported %d", cfg.Version, CurrentVersion)
	}
	if cfg.Version == CurrentVersion {
		return false, nil
	}
	backup, err := backupFile(path)
	if err != nil {
		return false, err
	}
	if backup != "" {
		fmt.Fprintf(os.Stderr, "config backup: %s\n", backup)
	}
	cfg.Version = CurrentVersion
	return true, nil
}

func backupFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	backup := path + ".bak"
	if err := os.WriteFile(backup, data, 0o600); err != nil {
		return "", err
	}
	return backup, nil
}
