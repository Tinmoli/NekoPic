package config

import (
	"fmt"
	"log/slog"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// categoryNamePattern 限制端点名：小写字母开头，只含小写字母、数字、下划线和连字符。
var categoryNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)

// ValidCategoryName 报告名字是否可作分类端点名。
func ValidCategoryName(name string) bool { return categoryNamePattern.MatchString(name) }

// ReservedCategoryNames 是内部端点占用的名字，分类不得使用。
func ReservedCategoryNames() map[string]bool {
	return map[string]bool{
		"api": true, "view": true, "test": true, "health": true,
		"assets": true, "favicon.ico": true, "dm": true, "auto": true, "web": true, "logs": true,
	}
}

// Validate 检查配置并把能自动补全的项补上；在开始监听之前调用。
func (c *Config) Validate() error {
	for _, check := range []func() error{
		c.validateServer, c.validateBaseURL, c.validateCategories, c.validateImage, c.validateRateLimit, c.validateSiteAndSecurity,
	} {
		if err := check(); err != nil {
			return err
		}
	}

	if c.Static.CacheMaxAge < 0 {
		return fmt.Errorf("static.cache_max_age must not be negative")
	}
	var level slog.Level
	if err := level.UnmarshalText([]byte(c.Log.Level)); err != nil {
		return fmt.Errorf("invalid log.level")
	}
	if c.Log.Format != "json" && c.Log.Format != "text" {
		return fmt.Errorf("log.format must be json or text")
	}
	return nil
}

func (c *Config) validateServer() error {
	if c.Server.Port < 1 || c.Server.Port > 65535 {
		return fmt.Errorf("server.port must be between 1 and 65535")
	}
	if c.Server.Host == "" {
		return fmt.Errorf("server.host must not be empty")
	}
	for name, value := range map[string]time.Duration{
		"read_header_timeout": c.Server.ReadHeaderTimeout,
		"read_timeout":        c.Server.ReadTimeout,
		"write_timeout":       c.Server.WriteTimeout,
		"idle_timeout":        c.Server.IdleTimeout,
		"shutdown_timeout":    c.Server.ShutdownTimeout,
	} {
		if value <= 0 {
			return fmt.Errorf("server.%s must be positive", name)
		}
	}
	if c.Server.MaxHeaderBytes <= 0 {
		return fmt.Errorf("server.max_header_bytes must be positive")
	}
	for _, cidr := range c.Server.TrustedProxies {
		if _, err := netip.ParsePrefix(cidr); err != nil {
			return fmt.Errorf("invalid trusted proxy CIDR %q", cidr)
		}
	}

	return nil
}

func (c *Config) validateBaseURL() error {
	// 留空表示使用相对地址，任何方式访问都正常，不需要校验。
	if c.BaseURL == "" {
		return nil
	}
	u, err := url.Parse(c.BaseURL)
	if err != nil || u == nil {
		return fmt.Errorf("invalid base_url")
	}
	hasExtras := u.User != nil || u.RawQuery != "" || u.ForceQuery || strings.Contains(c.BaseURL, "#")
	hasSubpath := (u.Path != "" && u.Path != "/") || u.RawPath != ""
	if (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || hasExtras || hasSubpath {
		return fmt.Errorf("base_url must be an http(s) origin without credentials, subpath, query or fragment")
	}
	if port := u.Port(); port != "" {
		n, e := strconv.Atoi(port)
		if e != nil || n < 1 || n > 65535 {
			return fmt.Errorf("invalid base_url port")
		}
	}
	if strings.HasSuffix(u.Host, ":") {
		return fmt.Errorf("invalid base_url port")
	}
	c.BaseURL = strings.TrimSuffix(c.BaseURL, "/")

	return nil
}

// validateCategories 检查分类列表：没写目录的用端点名补上；
// 查重名、保留端点和目录冲突；最后解析 /api 按设备指向的分类。
func (c *Config) validateCategories() error {
	if len(c.Categories) < 1 {
		return fmt.Errorf("at least one [[categories]] entry is required")
	}
	if len(c.Categories) > MaxCategories {
		return fmt.Errorf("at most %d categories are allowed", MaxCategories)
	}
	reserved := ReservedCategoryNames()
	names := make(map[string]bool, len(c.Categories))
	dirs := make(map[string]bool, len(c.Categories))
	for i := range c.Categories {
		cat := &c.Categories[i]
		if !categoryNamePattern.MatchString(cat.Name) {
			return fmt.Errorf("categories[%d].name %q must match %s", i, cat.Name, categoryNamePattern.String())
		}
		if reserved[cat.Name] {
			return fmt.Errorf("categories[%d].name %q is reserved", i, cat.Name)
		}
		if names[cat.Name] {
			return fmt.Errorf("duplicate category name %q", cat.Name)
		}
		names[cat.Name] = true
		if cat.Directory == "" {
			cat.Directory = "view/" + cat.Name
		}
		key := cat.Directory
		if runtime.GOOS == "windows" {
			key = strings.ToLower(key)
		}
		if dirs[key] {
			return fmt.Errorf("category %q reuses directory %q", cat.Name, cat.Directory)
		}
		dirs[key] = true
	}
	if err := c.resolveAdaptive(names); err != nil {
		return err
	}
	return c.validateCategoryLayout()
}

func (c *Config) resolveAdaptive(names map[string]bool) error {
	if c.Adaptive.Desktop != "" && !names[c.Adaptive.Desktop] {
		return fmt.Errorf("adaptive.desktop references unknown category %q", c.Adaptive.Desktop)
	}
	if c.Adaptive.Mobile != "" && !names[c.Adaptive.Mobile] {
		return fmt.Errorf("adaptive.mobile references unknown category %q", c.Adaptive.Mobile)
	}
	if c.Adaptive.Desktop == "" {
		if names["pc"] {
			c.Adaptive.Desktop = "pc"
		} else {
			c.Adaptive.Desktop = c.Categories[0].Name
		}
	}
	if c.Adaptive.Mobile == "" {
		if names["pe"] {
			c.Adaptive.Mobile = "pe"
		} else {
			c.Adaptive.Mobile = c.Categories[len(c.Categories)-1].Name
		}
	}
	return nil
}

func (c *Config) validateCategoryLayout() error {
	resolved := make([]string, len(c.Categories))
	for i, cat := range c.Categories {
		path, err := realPath(cat.Directory)
		if err != nil {
			return fmt.Errorf("resolve category %q directory: %w", cat.Name, err)
		}
		resolved[i] = path
		if c.base != "" && sameDir(path, c.base) {
			return fmt.Errorf("category %q directory must not be the program directory itself", cat.Name)
		}
	}
	for i := 0; i < len(resolved); i++ {
		for j := i + 1; j < len(resolved); j++ {
			if err := checkNotNested(resolved[i], resolved[j], c.Categories[i].Name, c.Categories[j].Name); err != nil {
				return err
			}
		}
	}
	return nil
}

func checkNotNested(a, b, nameA, nameB string) error {
	if runtime.GOOS == "windows" {
		a = strings.ToLower(a)
		b = strings.ToLower(b)
	}
	for _, pair := range [][2]string{{a, b}, {b, a}} {
		rel, err := filepath.Rel(pair[0], pair[1])
		if err != nil {
			continue
		}
		nested := rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
		if rel == "." || nested {
			return fmt.Errorf("categories %q and %q must use distinct and non-nested directories", nameA, nameB)
		}
	}
	return nil
}

func sameDir(a, b string) bool {
	if runtime.GOOS == "windows" {
		a = strings.ToLower(a)
		b = strings.ToLower(b)
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

// CategoryDirectories 返回各分类的规范绝对路径，供索引层按名字取目录。
func CategoryDirectories(categories []Category) (map[string]string, error) {
	dirs := make(map[string]string, len(categories))
	for _, cat := range categories {
		path, err := realPath(cat.Directory)
		if err != nil {
			return nil, fmt.Errorf("resolve category %q directory: %w", cat.Name, err)
		}
		dirs[cat.Name] = path
	}
	return dirs, nil
}

func (c *Config) validateImage() error {
	if len(c.Image.Extensions) == 0 {
		return fmt.Errorf("image.extensions must not be empty")
	}
	for i, ext := range c.Image.Extensions {
		ext = strings.ToLower(ext)
		switch ext {
		case ".jpg", ".jpeg", ".png", ".gif", ".webp":
		default:
			return fmt.Errorf("unsupported image extension %q", ext)
		}
		c.Image.Extensions[i] = ext
	}
	if c.Image.RefreshInterval < 0 || int64(c.Image.RefreshInterval) > int64((1<<63-1)/time.Minute) {
		return fmt.Errorf("invalid image.refresh_interval")
	}
	if c.Image.ScanWorkers < 1 || c.Image.ScanWorkers > 256 {
		return fmt.Errorf("image.scan_workers must be between 1 and 256")
	}
	if c.Image.MaxFileBytes <= 0 || c.Image.MaxPixels < 0 || c.Image.MaxMetadataBytes <= 0 {
		return fmt.Errorf("file and metadata limits must be positive; max_pixels must be non-negative")
	}

	return nil
}

func (c *Config) validateRateLimit() error {
	r := c.RateLimit
	if r.Enabled {
		blockOverflow := int64(r.BlockDuration) > int64((1<<63-1)/time.Second)
		if r.RequestsPer10s < 1 || r.BlockDuration < 1 || blockOverflow ||
			r.MaxClients < 1 || r.CleanupInterval <= 0 {
			return fmt.Errorf("ratelimit limits and durations must be positive and representable")
		}
		if r.IdleTTL < 10*time.Second || r.IdleTTL < time.Duration(r.BlockDuration)*time.Second {
			return fmt.Errorf("ratelimit.idle_ttl must cover the window and block duration")
		}
	}

	return nil
}

// realPath 把路径解析成真实路径（逐级跟随符号链接）。
// 对还不存在的目录，解析其父目录的真实位置，
// 防止用符号链接让两个分类目录悄悄指向同一个地方。
func realPath(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err == nil {
		return resolved, nil
	}
	if !os.IsNotExist(err) {
		return "", err
	}
	parent := filepath.Dir(absolute)
	if parent == absolute {
		return "", err
	}
	resolved, err = realPath(parent)
	if err != nil {
		return "", err
	}
	return filepath.Join(resolved, filepath.Base(absolute)), nil
}
