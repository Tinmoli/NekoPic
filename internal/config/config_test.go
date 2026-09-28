package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"nekopic"
)

func TestParseDefaultsAndOverrides(t *testing.T) {
	cfg, err := Parse([]byte("base_url = \"https://images.example.com/\"\n[ratelimit]\nenabled = false\n[server]\nport = 9000\nread_timeout = \"7s\""))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RateLimit.Enabled || cfg.Server.Port != 9000 || cfg.Server.ReadTimeout != 7*time.Second || cfg.BaseURL != "https://images.example.com" {
		t.Fatalf("bad config: %+v", cfg)
	}
	if cfg.Image.MaxPixels != 0 || cfg.Image.StrictExtension {
		t.Fatal("default must permit large images and mismatched extensions")
	}
	if len(cfg.Categories) != 2 || cfg.Categories[0].Name != "pc" || cfg.Categories[0].Directory != "view/pc" {
		t.Fatalf("default categories: %+v", cfg.Categories)
	}
	if cfg.Server.Host != "0.0.0.0" {
		t.Fatalf("default host: %s", cfg.Server.Host)
	}
	if cfg.Adaptive.Desktop != "pc" || cfg.Adaptive.Mobile != "pe" {
		t.Fatalf("default adaptive: %+v", cfg.Adaptive)
	}
	if !cfg.Security.InlineStyles {
		t.Fatal("inline styles must default to enabled")
	}
}

func TestRejectInvalidConfig(t *testing.T) {
	for _, input := range []string{
		"", "[server]\nprot=1", "[server]\nport=0", "[server]\nport=99999", "[server]\nport=1\nport=2",
		"[server]\nread_timeout=\"0s\"", "[server]\nread_timeout=5", "[server]\ntrusted_proxies=[\"bad\"]",
		"[log]\nlevel=\"wrong\"", "[log]\nformat=\"wrong\"",
		"base_url=\"ftp://example.com\"", "base_url=\"https://user:pass@example.com\"", "base_url=\"https://example.com/a\"", "base_url=\"https://example.com/#\"", "base_url=\"https://example.com:99999\"",
		"[image]\nextensions=[]", "[image]\nextensions=[\".svg\"]", "[image]\nscan_workers=0", "[image]\nmax_pixels=-1", "[image]\nmax_metadata_bytes=0", "[image]\nrefresh_interval=-1",
		"[ratelimit]\nrequests_per_10s=0", "[ratelimit]\nblock_duration=0", "[ratelimit]\nidle_ttl=\"5s\"", "[ratelimit]\nmax_clients=0",
		"[directories]\npc=\"same\"\npe=\"same/sub\"",
		"[directories]\n[[categories]]\nname=\"acg\"",
		"[directories]\nfoo=\"x\"",
		"categories=[]",
		"[[categories]]\nname=\"PC\"",
		"[[categories]]\nname=\"9pic\"",
		"[[categories]]\nname=\"a b\"",
		"[[categories]]\nname=\"api\"",
		"[[categories]]\nname=\"dm\"",
		"[[categories]]\nname=\"pc\"\n[[categories]]\nname=\"pc\"",
		"[[categories]]\nname=\"pc\"\ndirectory=\"same\"\n[[categories]]\nname=\"pe\"\ndirectory=\"same\"",
		"[[categories]]\nname=\"pc\"\ndirectory=\"same\"\n[[categories]]\nname=\"pe\"\ndirectory=\"same/sub\"",
		"[[categories]]\nname=\"acg\"\n[adaptive]\ndesktop=\"nope\"",
		"[[categories]]\nname=\"acg\"\n[adaptive]\nmobile=\"nope\"",
		"[site]\nname=\"\"", "[site]\nicon_url=\"javascript:alert(1)\"", "[site]\nicon_url=\"data:image/svg+xml,test\"",
		"[security]\nmax_url_bytes=1", "[security]\nmax_body_bytes=-1", "[security]\nallowed_ips=[\"bad\"]", "[security]\nallowed_hosts=[\"https://example.com\"]",
	} {
		t.Run(strings.ReplaceAll(input, "\n", "_"), func(t *testing.T) {
			if _, err := Parse([]byte(input)); err == nil {
				t.Fatalf("accepted %q", input)
			}
		})
	}
}

func TestLegacyDirectoriesConversion(t *testing.T) {
	cfg, err := Parse([]byte("[directories]\npc=\"PC\"\npe=\"PE\""))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Categories) != 2 || cfg.Categories[0].Name != "pc" || cfg.Categories[0].Directory != "PC" ||
		cfg.Categories[1].Name != "pe" || cfg.Categories[1].Directory != "PE" {
		t.Fatalf("legacy conversion: %+v", cfg.Categories)
	}
	cfg, err = Parse([]byte("[directories]"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Categories[0].Directory != "PC" || cfg.Categories[1].Directory != "PE" {
		t.Fatalf("legacy defaults: %+v", cfg.Categories)
	}
}

func TestCategoryAndAdaptiveResolution(t *testing.T) {
	cfg, err := Parse([]byte("[[categories]]\nname=\"acg\"\n[[categories]]\nname=\"walls\""))
	if err != nil {
		t.Fatal(err)
	}
	// 没有 pc/pe 时，/api 自动落到第一个和最后一个分类。
	if cfg.Adaptive.Desktop != "acg" || cfg.Adaptive.Mobile != "walls" {
		t.Fatalf("adaptive fallback: %+v", cfg.Adaptive)
	}
	cfg, err = Parse([]byte("[[categories]]\nname=\"acg\"\n[adaptive]\ndesktop=\"acg\"\nmobile=\"acg\""))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Adaptive.Desktop != "acg" || cfg.Adaptive.Mobile != "acg" {
		t.Fatalf("adaptive explicit: %+v", cfg.Adaptive)
	}
}

func TestExampleConfig(t *testing.T) {
	cfg, err := Parse([]byte(nekopic.DefaultConfig))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.Port != 8505 || cfg.BaseURL != "" || cfg.Image.MaxPixels != 0 {
		t.Fatal("incorrect example defaults")
	}
	if len(cfg.Categories) != 2 || cfg.Categories[0].Directory != "view/pc" || cfg.Categories[1].Directory != "view/pe" {
		t.Fatalf("example categories: %+v", cfg.Categories)
	}
	if cfg.Catalog.AutoDetect {
		t.Fatal("auto_detect must default to off")
	}
}

func TestBootstrapBesideExecutable(t *testing.T) {
	base := t.TempDir()
	t.Chdir(t.TempDir())
	executable := filepath.Join(base, "nekopic.exe")
	cfg, created, path, err := Prepare(executable, "")
	if err != nil {
		t.Fatal(err)
	}
	if !created || path != filepath.Join(base, "config.toml") {
		t.Fatalf("bootstrap: %v %s", created, path)
	}
	if cfg.Categories[0].Directory != filepath.Join(base, "view", "pc") || cfg.Categories[1].Directory != filepath.Join(base, "view", "pe") {
		t.Fatal("directories depend on working directory")
	}
	for _, dir := range []string{cfg.Categories[0].Directory, cfg.Categories[1].Directory} {
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			t.Fatalf("missing directory %s", dir)
		}
	}
	data := []byte("[site]\nname=\"我的图片\"\n")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	cfg, created, _, err = Prepare(executable, "")
	if err != nil || created || cfg.Site.Name != "我的图片" {
		t.Fatalf("existing config overwritten: %v", err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != string(data) {
		t.Fatal("modified existing config")
	}
}

func TestExplicitConfigStillUsesExecutableDirectory(t *testing.T) {
	base := t.TempDir()
	path := filepath.Join(t.TempDir(), "custom.toml")
	if err := os.WriteFile(path, []byte("[site]\nname=\"custom\""), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, created, _, err := Prepare(filepath.Join(base, "nekopic"), path)
	if err != nil || created {
		t.Fatalf("prepare: %v", err)
	}
	if cfg.Categories[0].Directory != filepath.Join(base, "view", "pc") {
		t.Fatal("relative image path follows config location")
	}
}

func TestCategoryDirectoryMustNotBeProgramRoot(t *testing.T) {
	base := t.TempDir()
	path := filepath.Join(base, "config.toml")
	if err := os.WriteFile(path, []byte("[[categories]]\nname=\"pc\"\ndirectory=\".\""), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := Prepare(filepath.Join(base, "nekopic.exe"), ""); err == nil {
		t.Fatal("accepted program root as gallery directory")
	}
}

func TestAppendCategoriesKeepsComments(t *testing.T) {
	base := t.TempDir()
	path := filepath.Join(base, "config.toml")
	original := "# 顶部注释\n[site]\nname=\"站点\" # 行内注释\n[[categories]]\nname=\"pc\"\n"
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	if err := AppendCategories(path, filepath.Dir(path), []Category{{Name: "acg", Directory: "acg"}}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "# 顶部注释") || !strings.Contains(string(data), "# 行内注释") {
		t.Fatalf("comments lost: %s", data)
	}
	cfg, err := Parse(data)
	if err != nil {
		t.Fatalf("appended config no longer parses: %v", err)
	}
	if len(cfg.Categories) != 2 || cfg.Categories[1].Name != "acg" || cfg.Categories[1].Directory != "acg" {
		t.Fatalf("appended category: %+v", cfg.Categories)
	}
}

func TestDirectorySymlinkOverlap(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	alias := filepath.Join(root, "alias")
	if err := os.Mkdir(target, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, alias); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	cfg := Defaults()
	cfg.Categories = []Category{
		{Name: "pc", Directory: target},
		{Name: "pe", Directory: filepath.Join(alias, "new")},
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("allowed nested directory through symlink")
	}
}
