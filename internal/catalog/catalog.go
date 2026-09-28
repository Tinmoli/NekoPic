// Package catalog 管理图片索引：每次扫描生成一份完整的图片清单，
// 整体替换旧清单；刷新失败时继续使用旧清单，服务不受影响。
package catalog

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"nekopic/internal/config"
	"nekopic/internal/model"
	"nekopic/internal/safefile"
)

type State struct {
	Snapshot *Snapshot
	Degraded bool
}

type Manager struct {
	cfg       config.Config
	log       *slog.Logger
	dirs      map[string]string
	order     []string
	state     atomic.Pointer[State]
	refreshMu sync.Mutex
}

func New(ctx context.Context, cfg config.Config, logger *slog.Logger) (*Manager, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	dirs, err := config.CategoryDirectories(cfg.Categories)
	if err != nil {
		return nil, err
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("create image directory: %w", err)
		}
	}

	order := make([]string, 0, len(cfg.Categories))
	for _, cat := range cfg.Categories {
		order = append(order, cat.Name)
	}
	m := &Manager{cfg: cfg, log: logger, dirs: dirs, order: order}
	m.state.Store(&State{})
	if err := m.Refresh(ctx); err != nil {
		return nil, err
	}
	return m, nil
}

func (m *Manager) State() State { return *m.state.Load() }

func (m *Manager) Refresh(ctx context.Context) error {
	m.refreshMu.Lock()
	defer m.refreshMu.Unlock()

	start := time.Now()
	old := m.state.Load()
	next := &Snapshot{
		order:      m.order,
		categories: make(map[string][]model.ImageInfo, len(m.order)),
		byPath:     make(map[string]model.ImageInfo),
	}
	if old.Snapshot != nil {
		next.Version = old.Snapshot.Version
	}
	next.Version++

	skipped := 0
	for _, category := range m.order {
		images, n, err := m.scan(ctx, category)
		skipped += n
		if err != nil {
			// 单个根目录失败时不发布半份新索引，全部分类一起保留。
			m.state.Store(&State{Snapshot: old.Snapshot, Degraded: true})
			m.log.Error("catalog refresh failed", "category", category, "error", err, "duration", time.Since(start))
			return err
		}
		next.categories[category] = images
		for _, img := range images {
			next.byPath["/"+category+"/"+img.Filename] = img
		}
	}

	if err := ctx.Err(); err != nil {
		return err
	}
	next.ScannedAt = time.Now().UTC()
	m.state.Store(&State{Snapshot: next})
	m.log.Info("catalog refreshed",
		"version", next.Version, "categories", len(m.order), "total", next.Count("dm"),
		"skipped", skipped, "duration", time.Since(start),
	)
	return nil
}

func (m *Manager) Run(ctx context.Context) {
	if m.cfg.Image.RefreshInterval == 0 {
		return
	}

	// 扫描完成后重新计时，较慢的扫描不会堆积新的刷新任务。
	timer := time.NewTimer(time.Duration(m.cfg.Image.RefreshInterval) * time.Minute)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			_ = m.Refresh(ctx)
			timer.Reset(time.Duration(m.cfg.Image.RefreshInterval) * time.Minute)
		}
	}
}

func (m *Manager) Open(img model.ImageInfo) (*os.File, error) {
	root, ok := m.dirs[img.Category]
	if !ok {
		return nil, os.ErrNotExist
	}
	openRoot, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer openRoot.Close()
	return safefile.Open(openRoot, img.Filename)
}
