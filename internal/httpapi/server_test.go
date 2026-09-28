package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"nekopic/internal/catalog"
	"nekopic/internal/config"
	"nekopic/internal/middleware"
	"nekopic/internal/testutil"
	"nekopic/internal/webui"
)

func init() { gin.SetMode(gin.TestMode) }

func pcDir(cfg config.Config) string { return cfg.Categories[0].Directory }

func peDir(cfg config.Config) string { return cfg.Categories[1].Directory }

func newTestAPI(t testing.TB, seed, limit bool) (*API, *catalog.Manager, config.Config) {
	t.Helper()
	cfg := config.Defaults()
	root := t.TempDir()
	cfg.Categories = []config.Category{
		{Name: "pc", Directory: filepath.Join(root, "pc")},
		{Name: "pe", Directory: filepath.Join(root, "pe")},
	}
	cfg.RateLimit.Enabled = limit
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	manager, err := catalog.New(context.Background(), cfg, logger)
	if err != nil {
		t.Fatal(err)
	}
	if seed {
		testutil.Image(t, filepath.Join(pcDir(cfg), "same.png"), "png", 3, 2)
		testutil.Image(t, filepath.Join(peDir(cfg), "same.png"), "png", 2, 3)
		if err := manager.Refresh(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	pages, err := webui.Load(filepath.Join(root, "web"), logger)
	if err != nil {
		t.Fatal(err)
	}
	api, err := New(cfg, manager, middleware.NewLimiter(cfg.RateLimit), pages, logger)
	if err != nil {
		t.Fatal(err)
	}
	return api, manager, cfg
}

func request(api http.Handler, method, target string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, nil)
	req.RemoteAddr = "192.0.2.1:12345"
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	rec := httptest.NewRecorder()
	api.ServeHTTP(rec, req)
	return rec
}

func TestModesAndErrors(t *testing.T) {
	api, _, _ := newTestAPI(t, true, false)
	api.pick = func(n int) int { return n - 1 }
	for _, category := range []string{"pc", "pe", "dm"} {
		t.Run(category, func(t *testing.T) {
			rec := request(api, "GET", "/"+category, nil)
			expectedCategory := category
			if category == "dm" {
				expectedCategory = "pe"
			}
			if rec.Code != 302 || rec.Header().Get("Location") != "/"+expectedCategory+"/same.png" || rec.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("redirect: %d %v", rec.Code, rec.Header())
			}
			rec = request(api, "GET", "/"+category+"?return=json", nil)
			var body map[string]string
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if rec.Code != 200 || len(body) != 5 || body["code"] != "200" || body["size"] != "png" || body["acgurl"] != "/"+expectedCategory+"/same.png" {
				t.Fatalf("json: %s", rec.Body.String())
			}
			if expectedCategory == "pc" && (body["width"] != "3" || body["height"] != "2") {
				t.Fatal("wrong dimensions")
			}
			rec = request(api, "GET", "/"+category+"?return=all", nil)
			want := "/" + category + "/same.png\n"
			if category == "dm" {
				want = "/pc/same.png\n/pe/same.png\n"
			}
			if rec.Code != 200 || rec.Body.String() != want || rec.Header().Get("Content-Type") != "text/plain; charset=utf-8" {
				t.Fatalf("all: %s", rec.Body.String())
			}
		})
	}
	for _, tc := range []struct {
		target string
		status int
		msg    string
	}{
		{"/pc?return=xml", 400, "invalid return parameter"}, {"/pc?return=JSON", 400, "invalid return parameter"},
		{"/pc?return=json&return=all", 400, "invalid return parameter"}, {"/pc?return=%GG", 400, "invalid query string"},
		{"/missing", 404, "not found"}, {"/PC", 404, "not found"}, {"/pc/", 404, "not found"},
	} {
		rec := request(api, "GET", tc.target, nil)
		if rec.Code != tc.status || !strings.Contains(rec.Body.String(), tc.msg) {
			t.Fatalf("%s: %d %s", tc.target, rec.Code, rec.Body.String())
		}
	}
	if rec := request(api, "GET", "/pc?return=&unused=1", nil); rec.Code != 302 {
		t.Fatal("empty return or unknown argument rejected")
	}
	// HEAD 必须和 GET 一样可用，且不返回正文。
	if rec := request(api, "HEAD", "/pc", nil); rec.Code != 302 || rec.Body.Len() != 0 {
		t.Fatalf("HEAD redirect: %d %d", rec.Code, rec.Body.Len())
	}
	if rec := request(api, "HEAD", "/health/ready", nil); rec.Code != 200 {
		t.Fatalf("HEAD health: %d", rec.Code)
	}
	for _, tc := range []struct{ path, allow string }{{"/pc", "GET, HEAD"}, {"/pc/same.png", "GET, HEAD"}} {
		rec := request(api, "POST", tc.path, nil)
		if rec.Code != 405 || rec.Header().Get("Allow") != tc.allow {
			t.Fatalf("method: %d %v", rec.Code, rec.Header())
		}
	}
}

func TestEmptyCatalogAndHealth(t *testing.T) {
	api, manager, cfg := newTestAPI(t, false, false)
	for _, category := range []string{"pc", "pe", "dm"} {
		for _, mode := range []string{"", "all", "json"} {
			rec := request(api, "GET", "/"+category+"?return="+mode, nil)
			want := 404
			if mode == "json" {
				want = 200
			}
			if rec.Code != want || !strings.Contains(rec.Body.String(), "no images available") {
				t.Fatalf("empty %s %s: %d %s", category, mode, rec.Code, rec.Body.String())
			}
		}
	}
	if rec := request(api, "GET", "/pc?return=bad", nil); rec.Code != 400 {
		t.Fatal("empty catalog bypassed parameter validation")
	}
	if rec := request(api, "GET", "/test", nil); rec.Code != 200 || rec.Body.String() != "Hello World" {
		t.Fatal("test response changed")
	}
	if rec := request(api, "GET", "/health/live", nil); rec.Code != 200 {
		t.Fatal("not live")
	}
	if rec := request(api, "GET", "/health/ready", nil); rec.Code != 503 {
		t.Fatal("empty catalog ready")
	}
	testutil.Image(t, filepath.Join(pcDir(cfg), "one.png"), "png", 1, 1)
	if err := manager.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if rec := request(api, "GET", "/health/ready", nil); rec.Code != 200 {
		t.Fatal("one category should be sufficient")
	}
	if rec := request(api, "GET", "/dm?return=json", nil); rec.Code != 200 {
		t.Fatal("dm must use nonempty category")
	}
	api.Stop()
	if rec := request(api, "GET", "/health/ready", nil); rec.Code != 503 {
		t.Fatal("stopping instance remained ready")
	}
}

func TestAdaptiveAndSharedQuota(t *testing.T) {
	api, _, _ := newTestAPI(t, true, true)
	for _, tc := range []struct{ ua, target string }{{"Android", "/pe"}, {"iPhone", "/pe"}, {"Windows NT", "/pc"}, {"", "/pc"}, {"iPad", "/pc"}, {"Go.Web", "/pe"}} {
		rec := request(api, "GET", "/api?return=json", map[string]string{"User-Agent": tc.ua})
		if rec.Code != 302 || rec.Header().Get("Location") != tc.target || rec.Header().Get("Vary") != "User-Agent" {
			t.Fatalf("UA %q: %v", tc.ua, rec.Header())
		}
	}
	for i := 0; i < 20; i++ {
		path := []string{"/pc", "/pe", "/dm"}[i%3]
		if rec := request(api, "GET", path, nil); rec.Code != 302 {
			t.Fatalf("quota consumed by adaptive endpoint at %d", i)
		}
	}
	rec := request(api, "GET", "/pc?return=invalid", nil)
	if rec.Code != 429 || rec.Header().Get("Retry-After") != "30" {
		t.Fatalf("shared quota: %d %s", rec.Code, rec.Body.String())
	}
	for _, path := range []string{"/test", "/health/live", "/pc/same.png"} {
		if rec := request(api, "GET", path, nil); rec.Code != 200 {
			t.Fatalf("exempt path %s blocked", path)
		}
	}
	rec = request(api, "GET", "/pe", map[string]string{"X-Forwarded-For": "198.51.100.7"})
	if rec.Code != 429 {
		t.Fatal("forged forwarded header bypassed quota")
	}
}

func TestStaticBytesRangeHeadAndDeletion(t *testing.T) {
	api, manager, cfg := newTestAPI(t, false, false)
	name := "中文 #%图.png"
	data := testutil.Image(t, filepath.Join(pcDir(cfg), name), "png", 3, 2)
	if err := manager.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	path := "/pc/" + url.PathEscape(name)
	rec := request(api, "GET", path, nil)
	if rec.Code != 200 || !bytes.Equal(rec.Body.Bytes(), data) || rec.Header().Get("Content-Type") != "image/png" || rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("static: %d %v", rec.Code, rec.Header())
	}
	modified := rec.Header().Get("Last-Modified")
	rec = request(api, "GET", path, map[string]string{"If-Modified-Since": modified})
	if rec.Code != 304 || rec.Body.Len() != 0 {
		t.Fatal("conditional request failed")
	}
	rec = request(api, "HEAD", path, nil)
	if rec.Code != 200 || rec.Body.Len() != 0 || rec.Header().Get("Content-Length") == "" {
		t.Fatal("HEAD failed")
	}
	rec = request(api, "GET", path, map[string]string{"Range": "bytes=0-7"})
	if rec.Code != 206 || !bytes.Equal(rec.Body.Bytes(), data[:8]) {
		t.Fatal("range failed")
	}
	testutil.Image(t, filepath.Join(pcDir(cfg), "unindexed.png"), "png", 1, 1)
	for _, path := range []string{"/pc/unindexed.png", "/pc/../config.yaml", "/pc/%2e%2e/config.yaml", "/pc/a%2fb.png", "/pc/a%5cb.png", "/pc/a%00.png", "/pc/a:stream", "/pc/.hidden.png", "/pc/%252e%252e%252fconfig.yaml"} {
		if rec := request(api, "GET", path, nil); rec.Code != 404 {
			t.Fatalf("unsafe path %q returned %d", path, rec.Code)
		}
	}
	if err := os.Remove(filepath.Join(pcDir(cfg), name)); err != nil {
		t.Fatal(err)
	}
	if rec := request(api, "GET", path, nil); rec.Code != 404 {
		t.Fatal("deleted image not 404")
	}
}

func TestHTMLErrorPages(t *testing.T) {
	api, _, _ := newTestAPI(t, true, true)
	html := map[string]string{"Accept": "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8"}
	// 浏览器访问不存在的路径：返回好看的错误页；程序调用仍是 JSON。
	rec := request(api, "GET", "/missing", html)
	if rec.Code != 404 || !strings.Contains(rec.Header().Get("Content-Type"), "text/html") ||
		!strings.Contains(rec.Body.String(), "找不到这个页面") {
		t.Fatalf("html 404: %d %s", rec.Code, rec.Body.String())
	}
	rec = request(api, "GET", "/missing", nil)
	if !strings.Contains(rec.Body.String(), "\"code\":\"404\"") {
		t.Fatalf("json 404 changed: %s", rec.Body.String())
	}
	// 耗尽配额后：浏览器拿到带自动重试的 429 页面，程序拿到 JSON。
	for i := 0; i < 20; i++ {
		if rec := request(api, "GET", "/pc", nil); rec.Code != 302 {
			t.Fatalf("quota warm-up failed at %d: %d", i, rec.Code)
		}
	}
	rec = request(api, "GET", "/pc", html)
	if rec.Code != 429 || !strings.Contains(rec.Header().Get("Content-Type"), "text/html") ||
		!strings.Contains(rec.Body.String(), `http-equiv="refresh"`) {
		t.Fatalf("html 429: %d %s", rec.Code, rec.Body.String())
	}
	rec = request(api, "GET", "/pc", nil)
	if rec.Code != 429 || strings.Contains(rec.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("json 429: %d %s", rec.Code, rec.Body.String())
	}
}

func TestRealHTTPRedirectChain(t *testing.T) {
	server := httptest.NewUnstartedServer(nil)
	defer server.Close()
	cfg := config.Defaults()
	cfg.BaseURL = "http://" + server.Listener.Addr().String()
	cfg.RateLimit.Enabled = false
	root := t.TempDir()
	cfg.Categories = []config.Category{
		{Name: "pc", Directory: filepath.Join(root, "pc")},
		{Name: "pe", Directory: filepath.Join(root, "pe")},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	manager, err := catalog.New(context.Background(), cfg, logger)
	if err != nil {
		t.Fatal(err)
	}
	expected := testutil.Image(t, filepath.Join(pcDir(cfg), "hello #%.png"), "png", 2, 2)
	if err := manager.Refresh(context.Background()); err != nil {
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
	server.Config.Handler = api
	server.Start()
	res, err := server.Client().Get(server.URL + "/api")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != 200 || !bytes.Equal(data, expected) {
		t.Fatalf("redirect chain failed: %d %q", res.StatusCode, data)
	}
}

func BenchmarkRandomJSON(b *testing.B) {
	api, _, _ := newTestAPI(b, true, false)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		rec := request(api, "GET", "/dm?return=json", nil)
		if rec.Code != 200 {
			b.Fatal(rec.Code)
		}
	}
}
