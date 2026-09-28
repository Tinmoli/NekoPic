// Package config 负责配置的默认值、读取和启动前检查。
package config

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/pelletier/go-toml/v2"
)

// MaxCategories 限制单实例分类数量，防止误配置拖慢扫描与路由。
const MaxCategories = 64

type Config struct {
	CDN        CDN        `toml:"cdn"`
	Site       Site       `toml:"site"`
	Security   Security   `toml:"security"`
	Server     Server     `toml:"server"`
	Catalog    Catalog    `toml:"catalog"`
	Categories []Category `toml:"categories"`
	Adaptive   Adaptive   `toml:"adaptive"`
	BaseURL    string     `toml:"base_url"`
	Image      Image      `toml:"image"`
	Static     Static     `toml:"static"`
	RateLimit  RateLimit  `toml:"ratelimit"`
	Log        Log        `toml:"log"`

	// base 记录主程序目录，仅用于校验图库目录不得是根目录本身。
	base string
}

// Base 返回解析配置时的主程序目录；手工构造的配置为空。
func (c Config) Base() string { return c.base }

// Category 是一个图库分类：name 同时是 URL 端点名。
type Category struct {
	Name      string `toml:"name"`
	Directory string `toml:"directory"`
}

type Catalog struct {
	AutoDetect bool `toml:"auto_detect"`
}

// Adaptive 决定 /api 与 /view/auto 按 User-Agent 指向的分类。
type Adaptive struct {
	Desktop string `toml:"desktop"`
	Mobile  string `toml:"mobile"`
}

type Server struct {
	ClientIPHeader    string        `toml:"client_ip_header"`
	Host              string        `toml:"host"`
	Port              int           `toml:"port"`
	ReadHeaderTimeout time.Duration `toml:"read_header_timeout"`
	ReadTimeout       time.Duration `toml:"read_timeout"`
	WriteTimeout      time.Duration `toml:"write_timeout"`
	IdleTimeout       time.Duration `toml:"idle_timeout"`
	ShutdownTimeout   time.Duration `toml:"shutdown_timeout"`
	MaxHeaderBytes    int           `toml:"max_header_bytes"`
	TrustedProxies    []string      `toml:"trusted_proxies"`
}

func (s Server) Address() string { return net.JoinHostPort(s.Host, strconv.Itoa(s.Port)) }

type Image struct {
	StrictExtension  bool     `toml:"strict_extension"`
	Extensions       []string `toml:"extensions"`
	RefreshInterval  int      `toml:"refresh_interval"`
	ScanWorkers      int      `toml:"scan_workers"`
	MaxFileBytes     int64    `toml:"max_file_bytes"`
	MaxPixels        int64    `toml:"max_pixels"`
	MaxMetadataBytes int64    `toml:"max_metadata_bytes"`
}

type Static struct {
	CacheMaxAge int `toml:"cache_max_age"`
}

type RateLimit struct {
	Enabled         bool          `toml:"enabled"`
	RequestsPer10s  int           `toml:"requests_per_10s"`
	BlockDuration   int           `toml:"block_duration"`
	CleanupInterval time.Duration `toml:"cleanup_interval"`
	IdleTTL         time.Duration `toml:"idle_ttl"`
	MaxClients      int           `toml:"max_clients"`
}

type Log struct {
	ClientIP bool   `toml:"client_ip"`
	Level    string `toml:"level"`
	Format   string `toml:"format"`
}

func Defaults() Config {
	return Config{
		CDN:      CDN{ClientIPHeader: "X-Forwarded-For"},
		Site:     Site{Name: "NekoPic 随机图片"},
		Security: Security{HeadersEnabled: true, InlineStyles: true, MaxURLBytes: 4096, MaxBodyBytes: 1024},
		Server: Server{
			ClientIPHeader: "X-Forwarded-For",
			TrustedProxies: []string{"127.0.0.1/32", "::1/128"},
			// 0.0.0.0 允许局域网其他设备直接访问；只想本机访问时改成 127.0.0.1。
			Host: "0.0.0.0", Port: 8505,
			ReadHeaderTimeout: 5 * time.Second,
			ReadTimeout:       15 * time.Second,
			WriteTimeout:      60 * time.Second,
			IdleTimeout:       60 * time.Second,
			ShutdownTimeout:   15 * time.Second,
			MaxHeaderBytes:    32 << 10,
		},
		Catalog: Catalog{AutoDetect: false},
		// 图库统一收在主程序旁的 view 文件夹里，保持主目录整洁。
		Categories: []Category{{Name: "pc", Directory: "view/pc"}, {Name: "pe", Directory: "view/pe"}},
		// Adaptive 留空，由校验阶段解析：显式填写优先，其次 pc/pe，最后首尾分类。
		Adaptive: Adaptive{},
		// BaseURL 留空时接口返回相对地址，本机、局域网、反代域名都能正常访问；
		// 填写域名后返回带域名的完整链接。
		BaseURL: "",
		Image: Image{
			Extensions:       []string{".jpg", ".jpeg", ".png", ".gif", ".webp"},
			RefreshInterval:  5,
			ScanWorkers:      4,
			MaxFileBytes:     50 << 20,
			MaxPixels:        0,
			MaxMetadataBytes: 8 << 20,
		},
		Static: Static{CacheMaxAge: 3600},
		RateLimit: RateLimit{
			Enabled: true, RequestsPer10s: 20, BlockDuration: 30,
			CleanupInterval: time.Minute, IdleTTL: 5 * time.Minute,
			MaxClients: 100000,
		},
		Log: Log{Level: "info", Format: "text", ClientIP: true},
	}
}

func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}
	return Parse(data)
}

// Parse 读取 TOML 配置并检查每一项取值；
// 写错的字段名或取值会直接报错，避免带着错误配置启动。
func Parse(data []byte) (Config, error) {
	return parseAt(data, "")
}

func parseAt(data []byte, base string) (Config, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return Config{}, fmt.Errorf("config must not be empty")
	}
	var raw map[string]any
	if err := toml.Unmarshal(data, &raw); err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}

	// 旧版 [directories] 转换为等价的 [[categories]]，老配置无需修改即可继续使用。
	if err := convertLegacyDirectories(raw); err != nil {
		return Config{}, err
	}

	// 配置里的时长写的是 "5s"、"5m" 这类字符串，这里先转成数字，
	// 后面的统一解析才能识别这几个字段。
	for section, keys := range map[string][]string{
		"server":    {"read_header_timeout", "read_timeout", "write_timeout", "idle_timeout", "shutdown_timeout"},
		"ratelimit": {"cleanup_interval", "idle_ttl"},
	} {
		table, ok := raw[section].(map[string]any)
		if !ok {
			continue
		}
		for _, key := range keys {
			value, exists := table[key]
			if !exists {
				continue
			}
			text, ok := value.(string)
			if !ok {
				return Config{}, fmt.Errorf("%s.%s must be a duration string", section, key)
			}
			duration, err := time.ParseDuration(text)
			if err != nil {
				return Config{}, fmt.Errorf("%s.%s: %w", section, key, err)
			}
			table[key] = int64(duration)
		}
	}

	normalized, err := toml.Marshal(raw)
	if err != nil {
		return Config{}, err
	}
	cfg := Defaults()
	decoder := toml.NewDecoder(bytes.NewReader(normalized)).DisallowUnknownFields()
	if err := decoder.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}
	cfg.base = base
	for i := range cfg.Categories {
		// 没写 directory 的分类，图库默认放在 view 文件夹下；
		// 先补默认值再做拼接，否则 Join(base, "") 会变成根目录本身。
		if cfg.Categories[i].Directory == "" {
			cfg.Categories[i].Directory = "view/" + cfg.Categories[i].Name
		}
		if base != "" && !filepath.IsAbs(cfg.Categories[i].Directory) {
			cfg.Categories[i].Directory = filepath.Join(base, cfg.Categories[i].Directory)
		}
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func convertLegacyDirectories(raw map[string]any) error {
	legacy, hasLegacy := raw["directories"]
	_, hasCategories := raw["categories"]
	if !hasLegacy {
		return nil
	}
	if hasCategories {
		return fmt.Errorf("use either [[categories]] or the legacy [directories], not both")
	}
	table, ok := legacy.(map[string]any)
	if !ok {
		return fmt.Errorf("[directories] must be a table")
	}
	// 与旧版默认值保持一致：缺省键沿用 PC/PE，空值留给校验报错。
	values := map[string]any{"pc": "PC", "pe": "PE"}
	for key, value := range table {
		switch key {
		case "pc", "pe":
			text, ok := value.(string)
			if !ok {
				return fmt.Errorf("directories.%s must be a string", key)
			}
			values[key] = text
		default:
			return fmt.Errorf("unknown legacy [directories] key %q", key)
		}
	}
	raw["categories"] = []map[string]any{
		{"name": "pc", "directory": values["pc"]},
		{"name": "pe", "directory": values["pe"]},
	}
	delete(raw, "directories")
	return nil
}

type Site struct {
	Name    string `toml:"name"`
	IconURL string `toml:"icon_url"`
}

type CDN struct {
	TrustedProxies []string `toml:"trusted_proxies"`
	ClientIPHeader string   `toml:"client_ip_header"`
}

type Security struct {
	HeadersEnabled bool     `toml:"headers_enabled"`
	AllowEmbedding bool     `toml:"allow_embedding"`
	InlineStyles   bool     `toml:"inline_styles"`
	MaxURLBytes    int      `toml:"max_url_bytes"`
	MaxBodyBytes   int64    `toml:"max_body_bytes"`
	AllowedHosts   []string `toml:"allowed_hosts"`
	AllowedIPs     []string `toml:"allowed_ips"`
}
