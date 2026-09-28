package autodetect

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"nekopic/internal/config"
	"nekopic/internal/testutil"
)

func TestDetect(t *testing.T) {
	base := t.TempDir()
	view := filepath.Join(base, "view")
	if err := os.MkdirAll(view, 0755); err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	existing := []config.Category{
		{Name: "pc", Directory: filepath.Join(base, "view", "pc")},
		{Name: "pe", Directory: filepath.Join(base, "view", "pe")},
	}
	for _, dir := range []string{"pc", "pe", "empty"} {
		if err := os.MkdirAll(filepath.Join(base, dir), 0755); err != nil {
			t.Fatal(err)
		}
	}
	seed := func(folder, format string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Join(view, folder), 0755); err != nil {
			t.Fatal(err)
		}
		ext := map[string]string{"png": ".png", "jpeg": ".jpg"}[format]
		testutil.Image(t, filepath.Join(view, folder, "a"+ext), format, 2, 2)
	}
	seed("acg", "png")
	seed("UPPER", "jpeg")
	seed(".hidden", "png")
	seed("logs", "png")
	seed("web", "png")
	seed("api", "png")
	seed("auto", "png")
	seed("pe", "png")
	if err := os.MkdirAll(filepath.Join(view, "fake"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(view, "fake", "a.png"), []byte("not an image"), 0644); err != nil {
		t.Fatal(err)
	}

	added := Detect(base, existing, []string{".png", ".jpg"}, 8<<20, logger)
	got := map[string]string{}
	for _, cat := range added {
		got[cat.Name] = cat.Directory
	}
	if len(got) != 2 {
		t.Fatalf("expected acg and upper, got %+v", added)
	}
	// 追加进配置的目录统一带 view/ 前缀。
	if got["acg"] != "view/acg" {
		t.Fatalf("acg detection: %+v", added)
	}
	// 大写文件夹名保留为目录，端点名用小写。
	if got["upper"] != "view/UPPER" {
		t.Fatalf("uppercase folder must keep its real directory name: %+v", added)
	}
}

func TestDetectWithoutProgramDirectory(t *testing.T) {
	base := t.TempDir()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if added := Detect(filepath.Join(base, "missing"), nil, []string{".png"}, 8<<20, logger); added != nil {
		t.Fatalf("missing base must yield nothing: %+v", added)
	}
}
