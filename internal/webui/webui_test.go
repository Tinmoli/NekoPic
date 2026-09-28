package webui

import (
	"bytes"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrepareWritesDefaultsOnce(t *testing.T) {
	dir := t.TempDir()
	if err := Prepare(dir); err != nil {
		t.Fatal(err)
	}
	for name := range DefaultFiles() {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("missing %s", name)
		}
	}
	custom := "/* 用户样式 */\nbody { color: red; }"
	if err := os.WriteFile(filepath.Join(dir, "style.css"), []byte(custom), 0644); err != nil {
		t.Fatal(err)
	}
	// 再次 Prepare 绝不覆盖用户修改过的文件。
	if err := Prepare(dir); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(dir, "style.css"))
	if string(got) != custom {
		t.Fatal("user stylesheet overwritten")
	}
}

func TestLoadReadsDiskAndFallsBack(t *testing.T) {
	dir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	pages, err := Load(dir, logger)
	if err != nil {
		t.Fatal(err)
	}
	if string(pages.CSS()) == "" {
		t.Fatal("missing stylesheet must fall back to embedded default")
	}

	if err := Prepare(dir); err != nil {
		t.Fatal(err)
	}
	pages, err = Load(dir, logger)
	if err != nil {
		t.Fatal(err)
	}
	data := struct {
		Name, Icon string
		Total      int
		Categories []struct {
			Name  string
			Count int
		}
	}{Name: "站点"}
	var out bytes.Buffer
	if err := pages.Home().Execute(&out, data); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "<title>站点</title>") {
		t.Fatal("home template did not render from disk")
	}

	// 模板语法错误时回退内嵌默认，服务不中断。
	if err := os.WriteFile(filepath.Join(dir, "home.html"), []byte("{{.Name"), 0644); err != nil {
		t.Fatal(err)
	}
	pages, err = Load(dir, logger)
	if err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := pages.Home().Execute(&out, data); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "A LITTLE SURPRISE") {
		t.Fatal("broken custom page did not fall back to embedded default")
	}
}

func TestViewerTemplateOnDisk(t *testing.T) {
	dir := t.TempDir()
	if err := Prepare(dir); err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	pages, err := Load(dir, logger)
	if err != nil {
		t.Fatal(err)
	}
	data := struct{ Name, Icon, Image, Next string }{Name: "站点", Next: "/view/pc"}
	var out bytes.Buffer
	if err := pages.Viewer().Execute(&out, data); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "站点") {
		t.Fatal(io.Discard, "viewer did not render")
	}
}
