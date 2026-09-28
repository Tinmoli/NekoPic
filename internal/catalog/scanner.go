package catalog

import (
	"context"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	_ "golang.org/x/image/webp"

	"nekopic/internal/model"
	"nekopic/internal/safefile"
)

func (m *Manager) scan(ctx context.Context, category string) ([]model.ImageInfo, int, error) {
	root, err := os.OpenRoot(m.dirs[category])
	if err != nil {
		return nil, 0, err
	}
	defer root.Close()

	dir, err := root.Open(".")
	if err != nil {
		return nil, 0, err
	}
	entries, err := dir.ReadDir(-1)
	dir.Close()
	if err != nil {
		return nil, 0, err
	}

	allowed := make(map[string]bool)
	for _, ext := range m.cfg.Image.Extensions {
		allowed[ext] = true
	}

	jobs := make(chan string)
	var mu sync.Mutex
	var images []model.ImageInfo
	skipped := 0
	var workers sync.WaitGroup

	for i := 0; i < m.cfg.Image.ScanWorkers; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for name := range jobs {
				if ctx.Err() != nil {
					continue
				}
				img, err := m.readImage(root, category, name)
				mu.Lock()
				if err != nil {
					skipped++
				} else {
					images = append(images, img)
				}
				mu.Unlock()
				if err != nil {
					m.log.Warn("image skipped", "category", category, "filename", name, "error", err)
				}
			}
		}()
	}

send:
	for _, entry := range entries {
		name := entry.Name()
		if !safefile.ValidName(name) || !entry.Type().IsRegular() || !allowed[strings.ToLower(filepath.Ext(name))] {
			continue
		}
		select {
		case jobs <- name:
		case <-ctx.Done():
			break send
		}
	}

	close(jobs)
	workers.Wait()
	if err := ctx.Err(); err != nil {
		return nil, skipped, err
	}
	sort.Slice(images, func(i, j int) bool { return images[i].Filename < images[j].Filename })
	return images, skipped, nil
}

func (m *Manager) readImage(root *os.Root, category, name string) (model.ImageInfo, error) {
	var result model.ImageInfo
	f, err := safefile.Open(root, name)
	if err != nil {
		return result, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return result, err
	}
	if info.Size() <= 0 || info.Size() > m.cfg.Image.MaxFileBytes {
		return result, fmt.Errorf("file size exceeds permitted bounds")
	}

	// 只读取图片开头的少量字节来识别格式和尺寸，不把整张图读进内存。
	dimensions, format, err := image.DecodeConfig(io.LimitReader(f, m.cfg.Image.MaxMetadataBytes))
	if err != nil {
		return result, fmt.Errorf("decode metadata: %w", err)
	}

	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(name)), ".")
	expected := ext
	if expected == "jpg" {
		expected = "jpeg"
	}
	if format != "jpeg" && format != "png" && format != "gif" && format != "webp" {
		return result, fmt.Errorf("unsupported image format")
	}
	if m.cfg.Image.StrictExtension && format != expected {
		return result, fmt.Errorf("file extension does not match detected format")
	}
	// 文件名和 URL 保持不变；兼容字段 size 与真实内容类型一致。
	if format != expected {
		ext = format
		if ext == "jpeg" {
			ext = "jpg"
		}
	}
	if dimensions.Width <= 0 || dimensions.Height <= 0 {
		return result, fmt.Errorf("invalid image dimensions")
	}
	if m.cfg.Image.MaxPixels > 0 && int64(dimensions.Width) > m.cfg.Image.MaxPixels/int64(dimensions.Height) {
		return result, fmt.Errorf("invalid or excessive image dimensions")
	}

	return model.ImageInfo{
		Category: category, Filename: name,
		Width: dimensions.Width, Height: dimensions.Height,
		Extension: ext, Format: format,
		Bytes: info.Size(), ModTime: info.ModTime(),
		PublicURL: m.cfg.BaseURL + "/" + category + "/" + url.PathEscape(name),
	}, nil
}
