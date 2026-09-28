package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"
)

// AppendCategories 保留原有文本和注释，仅在末尾添加分类。
// 先检查完整的新配置，再用同目录临时文件替换，写入失败时保留原文件。
func AppendCategories(path, base string, categories []Category) error {
	if len(categories) == 0 {
		return nil
	}
	original, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	var raw map[string]any
	if err := toml.Unmarshal(original, &raw); err != nil {
		return fmt.Errorf("parse config: %w", err)
	}

	toAppend := append([]Category(nil), categories...)
	if _, explicit := raw["categories"]; !explicit {
		// 首次写出分类表时，也要写出此前省略的默认分类，防止重启后丢失。
		toAppend = append(Defaults().Categories, toAppend...)
	}
	block, err := toml.Marshal(map[string]any{"categories": toAppend})
	if err != nil {
		return fmt.Errorf("render categories: %w", err)
	}
	updated := append(append(append([]byte(nil), original...), '\n'), block...)
	if _, err := parseAt(updated, base); err != nil {
		return fmt.Errorf("validate updated config: %w", err)
	}

	temp, err := os.CreateTemp(filepath.Dir(path), ".nekopic-config-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	if err := temp.Chmod(info.Mode().Perm()); err != nil {
		temp.Close()
		return err
	}
	_, writeErr := temp.Write(updated)
	syncErr := temp.Sync()
	closeErr := temp.Close()
	if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
		return fmt.Errorf("write updated config: %w", err)
	}

	// 管理员在识别过程中修改了配置时，不用旧内容覆盖他们的新设置。
	current, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if !bytes.Equal(current, original) {
		return fmt.Errorf("config changed during automatic detection; restart to try again")
	}
	if err := os.Rename(temp.Name(), path); err != nil {
		return fmt.Errorf("replace config: %w", err)
	}
	return nil
}
