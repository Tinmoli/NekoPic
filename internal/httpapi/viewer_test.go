package httpapi

import (
	"strings"
	"testing"

	"nekopic/internal/config"
)

func TestViewerKeepsSiteTitleAndAPIContract(t *testing.T) {
	api, _, _ := newTestAPI(t, true, false)
	for _, category := range []string{"pc", "pe", "dm", "auto"} {
		r := request(api, "GET", "/view/"+category, nil)
		body := r.Body.String()
		if r.Code != 200 || !strings.Contains(body, "<title>"+api.cfg.Site.Name+"</title>") ||
			!strings.Contains(body, "viewer-image") || strings.Contains(body, "<script") {
			t.Fatalf("bad viewer %s: %d %s", category, r.Code, body)
		}
	}
	if r := request(api, "GET", "/pc", nil); r.Code != 302 {
		t.Fatal("image API no longer redirects")
	}
	if r := request(api, "GET", "/view/other", nil); r.Code != 404 {
		t.Fatal("unknown category accepted")
	}
	home := request(api, "GET", "/", nil).Body.String()
	if strings.Contains(home, "<details") || !strings.Contains(home, "/dm?return=json") ||
		!strings.Contains(home, "/view/dm") || strings.Contains(home, "ฅ") {
		t.Fatal("homepage navigation or API documentation is wrong")
	}
}

func TestEmptyViewerEscapesName(t *testing.T) {
	api := configuredAPI(t, func(cfg *config.Config) { cfg.Site.Name = "<img onerror=alert(1)>" })
	r := request(api, "GET", "/view/pc", nil)
	body := r.Body.String()
	if r.Code != 200 || !strings.Contains(body, "暂时没有图片") ||
		strings.Contains(body, "<img onerror") || !strings.Contains(body, "&lt;img") {
		t.Fatal("empty viewer or escaping failed")
	}
}
