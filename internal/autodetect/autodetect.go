// Package autodetect 在启动时把未配置、但含有图片的文件夹收编为图库分类。
package autodetect

import (
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	_ "golang.org/x/image/webp"

	"nekopic/internal/config"
	"nekopic/internal/safefile"
)

// Detect 枚举主程序旁 view 文件夹下的一级子文件夹，返回可以新增的分类。
// 候选必须：名字可作端点名、不与保留端点或已有分类冲突、
// 且一级文件中至少有一张扩展名在白名单内、真实格式受支持的图片。
// 返回的分类目录统一写成 view/ 开头的相对路径，方便直接追加进配置文件。
func Detect(base string, existing []config.Category, extensions []string, maxMetadataBytes int64, logger *slog.Logger) []config.Category {
	reserved := config.ReservedCategoryNames()
	takenNames := map[string]bool{}
	takenDirs := map[string]bool{}
	for _, cat := range existing {
		takenNames[cat.Name] = true
		takenDirs[strings.ToLower(cat.Directory)] = true
	}

	galleryDir := filepath.Join(base, "view")
	entries, err := os.ReadDir(galleryDir)
	if err != nil {
		logger.Warn("autodetect could not list gallery directory", "directory", galleryDir, "error", err)
		return nil
	}

	allowed := make(map[string]bool, len(extensions))
	for _, ext := range extensions {
		allowed[ext] = true
	}

	var added []config.Category
	for _, entry := range entries {
		name := entry.Name()
		if !entry.IsDir() || strings.HasPrefix(name, ".") {
			continue
		}
		lower := strings.ToLower(name)
		if lower == "logs" || lower == "web" || reserved[lower] || takenNames[lower] {
			continue
		}
		if !config.ValidCategoryName(lower) {
			logger.Debug("autodetect skipped folder with unusable name", "folder", name)
			continue
		}
		dir := filepath.Join(galleryDir, name)
		if takenDirs[strings.ToLower(dir)] {
			continue
		}
		if !hasImage(dir, allowed, maxMetadataBytes) {
			logger.Debug("autodetect skipped folder without images", "folder", name)
			continue
		}
		added = append(added, config.Category{Name: lower, Directory: "view/" + name})
		takenNames[lower] = true
		takenDirs[strings.ToLower(dir)] = true
		logger.Info("autodetect found image folder", "folder", "view/"+name, "category", lower)
	}
	return added
}

// hasImage 检查文件夹的一级文件里是否至少有一张受支持图片，与索引使用同一判定。
func hasImage(dir string, allowed map[string]bool, maxMetadataBytes int64) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return false
	}
	defer root.Close()

	for _, entry := range entries {
		name := entry.Name()
		if !entry.Type().IsRegular() || !allowed[strings.ToLower(filepath.Ext(name))] {
			continue
		}
		f, err := safefile.Open(root, name)
		if err != nil {
			continue
		}
		if _, _, err := image.DecodeConfig(io.LimitReader(f, maxMetadataBytes)); err != nil {
			f.Close()
			continue
		}
		f.Close()
		return true
	}
	return false
}
