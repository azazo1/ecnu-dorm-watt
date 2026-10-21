package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

const CurrentVersion = 1

// LoadServer 读取服务端配置. 文件不存在且 create 为 true 时写入默认配置.
func LoadServer(path string, create bool) (*ServerConfig, bool, error) {
	cfg := DefaultServer()
	data, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) || !create {
			return nil, false, err
		}
		if err := WriteServer(path, &cfg); err != nil {
			return nil, false, err
		}
		return &cfg, true, nil
	}
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return nil, false, fmt.Errorf("parse %s: %w", path, err)
	}
	created, err := MigrateServer(&cfg, path)
	if err != nil {
		return nil, false, err
	}
	if err := ValidateServer(&cfg); err != nil {
		return nil, false, err
	}
	if created {
		if err := WriteServer(path, &cfg); err != nil {
			return nil, false, err
		}
	}
	return &cfg, false, nil
}

// LoadClient 读取客户端配置.
func LoadClient(path string, create bool) (*ClientConfig, bool, error) {
	cfg := DefaultClient()
	data, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) || !create {
			return nil, false, err
		}
		if err := WriteClient(path, &cfg); err != nil {
			return nil, false, err
		}
		return &cfg, true, nil
	}
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return nil, false, fmt.Errorf("parse %s: %w", path, err)
	}
	migrated, err := MigrateClient(&cfg, path)
	if err != nil {
		return nil, false, err
	}
	if err := ValidateClient(&cfg); err != nil {
		return nil, false, err
	}
	if migrated {
		if err := WriteClient(path, &cfg); err != nil {
			return nil, false, err
		}
	}
	return &cfg, false, nil
}

// WriteServer 原子写入服务端配置, 权限 0600.
func WriteServer(path string, cfg *ServerConfig) error {
	if cfg.Version == 0 {
		cfg.Version = CurrentVersion
	}
	return writeTOML(path, cfg)
}

// WriteClient 原子写入客户端配置, 权限 0600.
func WriteClient(path string, cfg *ClientConfig) error {
	if cfg.Version == 0 {
		cfg.Version = CurrentVersion
	}
	return writeTOML(path, cfg)
}

func writeTOML(path string, v any) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".config-*.toml")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if err := toml.NewEncoder(tmp).Encode(v); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
