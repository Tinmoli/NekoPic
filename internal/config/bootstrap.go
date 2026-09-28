package config

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"nekopic"
)

// Prepare 将运行目录固定到可执行文件旁。缺少配置时仅生成，不启动服务。
// 显式 -c 的相对路径也按主程序目录解析，避免双击/终端出现不同图库。
func Prepare(executablePath, configPath string) (Config, bool, string, error) {
	executablePath, err := filepath.Abs(executablePath)
	if err != nil {
		return Config{}, false, "", err
	}
	base := filepath.Dir(executablePath)
	if configPath == "" {
		configPath = "config.toml"
	}
	if !filepath.IsAbs(configPath) {
		configPath = filepath.Join(base, configPath)
	}

	created, err := writeDefault(configPath)
	if err != nil {
		return Config{}, false, configPath, err
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		return Config{}, created, configPath, err
	}
	cfg, err := parseAt(data, base)
	if err != nil {
		return Config{}, created, configPath, err
	}

	for _, dir := range append(categoryPaths(cfg), filepath.Join(base, "logs")) {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return Config{}, created, configPath, fmt.Errorf("create image directory: %w", err)
		}
	}
	return cfg, created, configPath, nil
}

func categoryPaths(cfg Config) []string {
	paths := make([]string, 0, len(cfg.Categories))
	for _, cat := range cfg.Categories {
		paths = append(paths, cat.Directory)
	}
	return paths
}

func writeDefault(path string) (bool, error) {
	// 用独占方式创建配置文件：配置已存在时绝不覆盖，
	// 多个程序同时第一次启动也不会互相破坏。
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if errors.Is(err, os.ErrExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("create config: %w", err)
	}
	_, writeErr := io.WriteString(file, nekopic.DefaultConfig)
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		return true, fmt.Errorf("write config: %w", errors.Join(writeErr, closeErr))
	}
	return true, nil
}
