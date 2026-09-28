package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"nekopic/internal/catalog"
	"nekopic/internal/config"
	"nekopic/internal/httpapi"
	"nekopic/internal/middleware"
	"nekopic/internal/testutil"
	"nekopic/internal/webui"
)

func categorySetup(t *testing.T, text string) (config.Config, string, string, *slog.Logger) {
	t.Helper()
	base := t.TempDir()
	path := filepath.Join(base, "config.toml")
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, _, _, err := config.Prepare(filepath.Join(base, "nekopic.exe"), path)
	if err != nil {
		t.Fatal(err)
	}
	return cfg, base, path, slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestAutoDetectUsesExecutableDirectory(t *testing.T) {
	cfg, base, path, logger := categorySetup(t, "[catalog]\nauto_detect=true\n")
	t.Chdir(t.TempDir())
	if err := os.MkdirAll(filepath.Join(base, "view", "acg"), 0755); err != nil {
		t.Fatal(err)
	}
	testutil.Image(t, filepath.Join(base, "view", "acg", "one.png"), "png", 2, 2)
	if err := prepareCategories(&cfg, path, base, logger); err != nil {
		t.Fatal(err)
	}
	manager, err := catalog.New(context.Background(), cfg, logger)
	if err != nil {
		t.Fatal(err)
	}
	if n := manager.State().Snapshot.Count("acg"); n != 1 {
		t.Fatalf("auto-detected executable/view/acg image missing: count=%d; configured directory=%s", n, cfg.Categories[len(cfg.Categories)-1].Directory)
	}
}

func TestAutoDetectPreservesImplicitCategories(t *testing.T) {
	cfg, base, path, logger := categorySetup(t, "[catalog]\nauto_detect=true\n")
	t.Chdir(base)
	if err := os.MkdirAll(filepath.Join(base, "view", "acg"), 0755); err != nil {
		t.Fatal(err)
	}
	testutil.Image(t, filepath.Join(base, "view", "acg", "one.png"), "png", 2, 2)
	if err := prepareCategories(&cfg, path, base, logger); err != nil {
		t.Fatal(err)
	}
	after, _, _, err := config.Prepare(filepath.Join(base, "nekopic.exe"), path)
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Categories) != len(cfg.Categories) {
		t.Fatalf("before restart=%v; after restart=%v", cfg.Categories, after.Categories)
	}
}

func TestReservedAutoCategoryRejected(t *testing.T) {
	if _, err := config.Parse([]byte("[[categories]]\nname='auto'\n")); err == nil {
		t.Fatal("auto must be rejected before route registration")
	}
}

func TestAdaptiveAPIUsesConfiguredCategories(t *testing.T) {
	cfg, base, _, logger := categorySetup(t, "[[categories]]\nname='desktop'\n[[categories]]\nname='mobile'\n[adaptive]\ndesktop='desktop'\nmobile='mobile'\n")
	manager, err := catalog.New(context.Background(), cfg, logger)
	if err != nil {
		t.Fatal(err)
	}
	pages, err := webui.Load(filepath.Join(base, "web"), logger)
	if err != nil {
		t.Fatal(err)
	}
	api, err := httpapi.New(cfg, manager, middleware.NewLimiter(cfg.RateLimit), pages, logger)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ ua, want string }{{"Mozilla Windows", "/desktop"}, {"Android Mobile", "/mobile"}} {
		req := httptest.NewRequest("GET", "/api", nil)
		req.Header.Set("User-Agent", tc.ua)
		rec := httptest.NewRecorder()
		api.ServeHTTP(rec, req)
		if got := rec.Header().Get("Location"); got != tc.want {
			t.Errorf("%s redirects to %s, want %s", tc.ua, got, tc.want)
		}
	}
}

func TestAutoDetectValidatesBeforePersisting(t *testing.T) {
	cfg, base, path, logger := categorySetup(t, "[catalog]\nauto_detect=true\n")
	t.Chdir(base)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 65; i++ {
		dir := filepath.Join(base, "view", fmt.Sprintf("extra%d", i))
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		testutil.Image(t, filepath.Join(dir, "one.png"), "png", 2, 2)
	}
	err = prepareCategories(&cfg, path, base, logger)
	after, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if err != nil && string(after) != string(before) {
		t.Fatalf("failed startup modified config: %v", err)
	}
	if _, _, _, err := config.Prepare(filepath.Join(base, "nekopic.exe"), path); err != nil {
		t.Fatalf("configuration left invalid: %v", err)
	}
}

func TestAutoDetectionDisabledLeavesConfigUntouched(t *testing.T) {
	cfg, base, path, logger := categorySetup(t, "[catalog]\nauto_detect=false\n")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(base, "view", "acg"), 0755); err != nil {
		t.Fatal(err)
	}
	testutil.Image(t, filepath.Join(base, "view", "acg", "one.png"), "png", 2, 2)
	if err := prepareCategories(&cfg, path, base, logger); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) || len(cfg.Categories) != 2 {
		t.Fatal("disabled detection changed configuration")
	}
}
