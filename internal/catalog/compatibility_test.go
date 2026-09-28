package catalog

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"nekopic/internal/testutil"
)

func TestMismatchedExtensions(t *testing.T) {
	cfg, logger := setup(t)
	if err := os.MkdirAll(pcDir(cfg), 0755); err != nil {
		t.Fatal(err)
	}
	testutil.Image(t, filepath.Join(pcDir(cfg), "jpeg.png"), "jpeg", 3, 2)
	testutil.Image(t, filepath.Join(pcDir(cfg), "png.jpg"), "png", 2, 3)
	if err := os.WriteFile(filepath.Join(pcDir(cfg), "script.jpg"), []byte("<script>alert(1)</script>"), 0644); err != nil {
		t.Fatal(err)
	}

	manager, err := New(context.Background(), cfg, logger)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := manager.State().Snapshot
	if snapshot.Count("pc") != 2 {
		t.Fatal("valid mismatched images were not indexed or script was accepted")
	}
	for _, tc := range []struct{ name, format, ext string }{{"jpeg.png", "jpeg", "jpg"}, {"png.jpg", "png", "png"}} {
		img, ok := snapshot.Lookup("/pc/" + tc.name)
		if !ok || img.Format != tc.format || img.Extension != tc.ext || img.Filename != tc.name {
			t.Fatalf("wrong detection: %+v", img)
		}
	}
	cfg.Image.StrictExtension = true
	strict, err := New(context.Background(), cfg, logger)
	if err != nil {
		t.Fatal(err)
	}
	if strict.State().Snapshot.Count("pc") != 0 {
		t.Fatal("strict mode accepted mismatched files")
	}
}
