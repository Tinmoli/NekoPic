package catalog

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"nekopic/internal/config"
	"nekopic/internal/testutil"
)

func setup(t testing.TB) (config.Config, *slog.Logger) {
	t.Helper()
	cfg := config.Defaults()
	root := t.TempDir()
	cfg.Categories = []config.Category{
		{Name: "pc", Directory: filepath.Join(root, "pc")},
		{Name: "pe", Directory: filepath.Join(root, "pe")},
	}
	return cfg, slog.New(slog.NewTextHandler(io.Discard, nil))
}

func pcDir(cfg config.Config) string { return cfg.Categories[0].Directory }

func peDir(cfg config.Config) string { return cfg.Categories[1].Directory }

func TestScanAndRefresh(t *testing.T) {
	cfg, logger := setup(t)
	cfg.Image.StrictExtension = true
	m, err := New(context.Background(), cfg, logger)
	if err != nil {
		t.Fatal(err)
	}
	if m.State().Snapshot.Count("dm") != 0 {
		t.Fatal("new directories not empty")
	}
	testutil.Image(t, filepath.Join(pcDir(cfg), "中文 #%图.PNG"), "png", 3, 2)
	testutil.Image(t, filepath.Join(peDir(cfg), "photo.jpeg"), "jpeg", 2, 4)
	testutil.Image(t, filepath.Join(peDir(cfg), "motion.gif"), "gif", 2, 3)
	testutil.Image(t, filepath.Join(pcDir(cfg), "fake.jpg"), "png", 1, 1)
	testutil.Image(t, filepath.Join(pcDir(cfg), ".hidden.png"), "png", 1, 1)
	if err := os.WriteFile(filepath.Join(pcDir(cfg), "broken.png"), []byte("bad"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := m.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	old := m.State().Snapshot
	if old.Count("pc") != 1 || old.Count("pe") != 2 {
		t.Fatalf("pc=%d pe=%d", old.Count("pc"), old.Count("pe"))
	}
	first := old.At("dm", 0)
	if first.Width != 3 || first.Height != 2 || first.Extension != "png" || first.PublicURL != "/pc/%E4%B8%AD%E6%96%87%20%23%25%E5%9B%BE.PNG" {
		t.Fatalf("bad metadata: %+v", first)
	}
	if old.At("dm", 1).Category != "pe" || old.At("dm", 2).Category != "pe" {
		t.Fatal("dm category mapping incorrect")
	}
	if err := os.Remove(filepath.Join(pcDir(cfg), first.Filename)); err != nil {
		t.Fatal(err)
	}
	if err := m.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if m.State().Snapshot.Count("pc") != 0 || old.Count("pc") != 1 {
		t.Fatal("snapshot mutated or deletion not published")
	}
	// A root-directory failure preserves both categories of the previous snapshot.
	previous := m.State().Snapshot
	if err := os.Rename(peDir(cfg), peDir(cfg)+"-away"); err != nil {
		t.Fatal(err)
	}
	if err := m.Refresh(context.Background()); err == nil {
		t.Fatal("missing root accepted")
	}
	if m.State().Snapshot != previous || !m.State().Degraded {
		t.Fatal("failed scan destroyed old snapshot")
	}
	if err := os.Rename(peDir(cfg)+"-away", peDir(cfg)); err != nil {
		t.Fatal(err)
	}
	if err := m.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if m.State().Degraded {
		t.Fatal("successful scan did not clear degraded state")
	}
}

func TestImageLimits(t *testing.T) {
	for _, kind := range []string{"pixels", "file", "metadata"} {
		t.Run(kind, func(t *testing.T) {
			cfg, logger := setup(t)
			if err := os.MkdirAll(pcDir(cfg), 0755); err != nil {
				t.Fatal(err)
			}
			testutil.Image(t, filepath.Join(pcDir(cfg), "large.png"), "png", 8, 8)
			switch kind {
			case "pixels":
				cfg.Image.MaxPixels = 10
			case "file":
				cfg.Image.MaxFileBytes = 10
			case "metadata":
				cfg.Image.MaxMetadataBytes = 10
			}
			m, err := New(context.Background(), cfg, logger)
			if err != nil {
				t.Fatal(err)
			}
			if m.State().Snapshot.Count("dm") != 0 {
				t.Fatal("limit ignored")
			}
		})
	}
}

func TestConcurrentSnapshotReaders(t *testing.T) {
	cfg, logger := setup(t)
	m, err := New(context.Background(), cfg, logger)
	if err != nil {
		t.Fatal(err)
	}
	testutil.Image(t, filepath.Join(pcDir(cfg), "one.png"), "png", 1, 1)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Go(func() {
			for j := 0; j < 100; j++ {
				s := m.State().Snapshot
				if s.Count("dm") > 0 {
					_ = s.At("dm", 0)
					s.Lookup("/pc/one.png")
				}
			}
		})
	}
	for i := 0; i < 10; i++ {
		if err := m.Refresh(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	wg.Wait()
}

func TestWebPMetadata(t *testing.T) {
	cfg, logger := setup(t)
	if err := os.MkdirAll(pcDir(cfg), 0755); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile("testdata/sample.webp")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pcDir(cfg), "sample.webp"), data, 0644); err != nil {
		t.Fatal(err)
	}
	m, err := New(context.Background(), cfg, logger)
	if err != nil {
		t.Fatal(err)
	}
	s := m.State().Snapshot
	if s.Count("pc") != 1 {
		t.Fatal("WebP was not indexed")
	}
	img := s.At("pc", 0)
	if img.Width != 75 || img.Height != 100 || img.Format != "webp" || img.Extension != "webp" {
		t.Fatalf("WebP metadata: %+v", img)
	}
}
