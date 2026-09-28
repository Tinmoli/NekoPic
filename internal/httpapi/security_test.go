package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"nekopic/internal/catalog"
	"nekopic/internal/config"
	"nekopic/internal/middleware"
	"nekopic/internal/testutil"
	"nekopic/internal/webui"
)

func configuredAPI(t *testing.T, change func(*config.Config)) *API {
	t.Helper()
	cfg := config.Defaults()
	root := t.TempDir()
	cfg.Categories = []config.Category{
		{Name: "pc", Directory: filepath.Join(root, "pc")},
		{Name: "pe", Directory: filepath.Join(root, "pe")},
	}
	change(&cfg)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	manager, err := catalog.New(context.Background(), cfg, logger)
	if err != nil {
		t.Fatal(err)
	}
	pages, err := webui.Load(filepath.Join(root, "web"), logger)
	if err != nil {
		t.Fatal(err)
	}
	api, err := New(cfg, manager, middleware.NewLimiter(cfg.RateLimit), pages, logger)
	if err != nil {
		t.Fatal(err)
	}
	return api
}

func TestHomepageEscapingAndIcon(t *testing.T) {
	api := configuredAPI(t, func(cfg *config.Config) {
		cfg.Site.Name = `<script>alert("x")</script>`
		cfg.Site.IconURL = "https://example.com/icon.png?a=1&b=2"
	})
	response := request(api, "GET", "/", nil)
	if response.Code != 200 || strings.Contains(response.Body.String(), "<script>") || !strings.Contains(response.Body.String(), "&lt;script&gt;") {
		t.Fatalf("unsafe HTML: %s", response.Body.String())
	}
	if !strings.Contains(response.Header().Get("Content-Security-Policy"), "script-src 'none'") || response.Header().Get("X-Frame-Options") != "DENY" {
		t.Fatal("security headers missing")
	}
	if !strings.Contains(response.Body.String(), "icon.png?a=1&amp;b=2") {
		t.Fatal("icon URL not escaped")
	}
	if r := request(api, "GET", "/favicon.ico", nil); r.Code != 302 || r.Header().Get("Location") != "https://example.com/icon.png?a=1&b=2" {
		t.Fatal("favicon")
	}
	if r := request(api, "GET", "/assets/style.css", nil); r.Code != 200 {
		t.Fatal("stylesheet")
	}
}

func TestSecurityGuards(t *testing.T) {
	api := configuredAPI(t, func(cfg *config.Config) {
		cfg.Security.AllowedHosts = []string{"allowed.test"}
		cfg.Security.AllowedIPs = []string{"192.0.2.0/24"}
		cfg.Security.MaxURLBytes = 256
		cfg.Security.MaxBodyBytes = 16
	})
	call := func(target, peer, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", target, strings.NewReader(body))
		req.RemoteAddr = peer
		rec := httptest.NewRecorder()
		api.ServeHTTP(rec, req)
		return rec
	}
	for _, tc := range []struct {
		url, ip, body string
		status        int
	}{
		{"http://allowed.test/test", "192.0.2.1:80", "", 200},
		{"http://wrong.test/test", "192.0.2.1:80", "", 403},
		{"http://allowed.test/test", "198.51.100.1:80", "", 403},
		{"http://allowed.test/test?x=" + strings.Repeat("x", 300), "192.0.2.1:80", "", 414},
		{"http://allowed.test/test", "192.0.2.1:80", strings.Repeat("x", 17), 413},
	} {
		if r := call(tc.url, tc.ip, tc.body); r.Code != tc.status {
			t.Fatalf("got %d want %d: %s", r.Code, tc.status, r.Body.String())
		}
	}
}

func TestMismatchedImageServedWithRealMIME(t *testing.T) {
	api, manager, cfg := newTestAPI(t, false, false)
	testutil.Image(t, filepath.Join(pcDir(cfg), "really-png.jpg"), "png", 2, 2)
	if err := manager.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	r := request(api, "GET", "/pc/really-png.jpg", nil)
	if r.Code != 200 || r.Header().Get("Content-Type") != "image/png" {
		t.Fatal("MIME used filename instead of actual format")
	}
	r = request(api, "GET", "/pc?return=json", nil)
	if !strings.Contains(r.Body.String(), `"size":"png"`) {
		t.Fatal("JSON format must match actual content")
	}
}
