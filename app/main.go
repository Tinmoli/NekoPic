package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"

	"github.com/gin-gonic/gin"

	"nekopic/internal/autodetect"
	"nekopic/internal/catalog"
	"nekopic/internal/config"
	"nekopic/internal/httpapi"
	"nekopic/internal/logfile"
	"nekopic/internal/middleware"
	"nekopic/internal/webui"
)

var version = "0.4.1-dev"

func main() {
	path := flag.String("c", "", "TOML config path; defaults to config.toml beside executable")
	showVersion := flag.Bool("version", false, "print version")
	flag.Parse()
	if *showVersion {
		fmt.Println("nekopic", version)
		return
	}

	executable, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	base := filepath.Dir(executable)
	cfg, created, configPath, err := config.Prepare(executable, *path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "configuration error:", err)
		os.Exit(1)
	}
	// 网页副本在首次运行就生成，保证双击一次后所有东西都齐了。
	if err := webui.Prepare(filepath.Join(base, "web")); err != nil {
		fmt.Fprintln(os.Stderr, "无法准备网页文件：", err)
		os.Exit(1)
	}
	if created {
		fmt.Println("已生成配置：", configPath)
		fmt.Println("已准备图库与网页。修改配置后，再次运行即可启动服务。")
		return
	}

	logs, err := logfile.New(filepath.Join(base, "logs"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "无法创建日志文件：", err)
		os.Exit(1)
	}
	logger := newLogger(cfg.Log, io.MultiWriter(logs, os.Stdout))

	if err := prepareCategories(&cfg, configPath, base, logger); err != nil {
		logger.Error("configuration error", "error", err)
		_ = logs.Close()
		os.Exit(1)
	}

	gin.SetMode(gin.ReleaseMode)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err = run(ctx, cfg, base, logger)
	stop()
	if err != nil {
		logger.Error("service stopped", "error", err)
		_ = logs.Close()
		os.Exit(1)
	}
	if err := logs.Close(); err != nil {
		fmt.Fprintln(os.Stderr, "关闭日志文件失败：", err)
	}
}

// prepareCategories 在开启 auto_detect 时收编 view 文件夹下含图片的新文件夹：
// 写回配置文件并合并到当前配置，当次启动即生效。
func prepareCategories(cfg *config.Config, configPath, base string, logger *slog.Logger) error {
	if !cfg.Catalog.AutoDetect {
		return nil
	}
	if err := os.MkdirAll(filepath.Join(base, "view"), 0755); err != nil {
		return err
	}
	added := autodetect.Detect(base, cfg.Categories, cfg.Image.Extensions, cfg.Image.MaxMetadataBytes, logger)
	if len(added) == 0 {
		return nil
	}
	// 运行时统一使用主程序目录下的绝对路径，写入配置时仍保留相对路径。
	next := *cfg
	next.Categories = append([]config.Category(nil), cfg.Categories...)
	names := make([]string, 0, len(added))
	for _, cat := range added {
		names = append(names, cat.Name)
		if !filepath.IsAbs(cat.Directory) {
			cat.Directory = filepath.Join(base, cat.Directory)
		}
		next.Categories = append(next.Categories, cat)
	}
	if err := next.Validate(); err != nil {
		return err
	}
	if err := config.AppendCategories(configPath, base, added); err != nil {
		logger.Warn("autodetect could not persist categories; they apply to this run only", "error", err)
	}
	*cfg = next
	logger.Info("autodetect extended categories", "categories", strings.Join(names, ","))
	return nil
}

func newLogger(cfg config.Log, output io.Writer) *slog.Logger {
	var level slog.Level
	_ = level.UnmarshalText([]byte(cfg.Level))
	options := &slog.HandlerOptions{Level: level}
	var handler slog.Handler = slog.NewJSONHandler(output, options)
	if cfg.Format == "text" {
		handler = slog.NewTextHandler(output, options)
	}
	return slog.New(handler)
}

func run(ctx context.Context, cfg config.Config, base string, logger *slog.Logger) error {
	background, cancel := context.WithCancel(ctx)
	defer cancel()

	manager, err := catalog.New(background, cfg, logger)
	if err != nil {
		return err
	}
	pages, err := webui.Load(filepath.Join(base, "web"), logger)
	if err != nil {
		return err
	}
	limiter := middleware.NewLimiter(cfg.RateLimit)
	api, err := httpapi.New(cfg, manager, limiter, pages, logger)
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", cfg.Server.Address())
	if err != nil {
		return err
	}

	server := &http.Server{
		Handler:           api,
		ReadHeaderTimeout: cfg.Server.ReadHeaderTimeout,
		ReadTimeout:       cfg.Server.ReadTimeout,
		WriteTimeout:      cfg.Server.WriteTimeout,
		IdleTimeout:       cfg.Server.IdleTimeout,
		MaxHeaderBytes:    cfg.Server.MaxHeaderBytes,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelError),
	}
	var tasks sync.WaitGroup
	tasks.Go(func() { manager.Run(background) })
	tasks.Go(func() { limiter.Run(background) })
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	logger.Info("listening", "address", listener.Addr().String(), "base_url", cfg.BaseURL, "version", version)

	var serveErr error
	select {
	case <-ctx.Done():
	case serveErr = <-done:
	}
	api.Stop()
	cancel()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), cfg.Server.ShutdownTimeout)
	defer shutdownCancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Warn("graceful shutdown deadline reached", "error", err)
		_ = server.Close()
	}
	tasks.Wait()
	logger.Info("service stopped")
	if errors.Is(serveErr, http.ErrServerClosed) {
		return nil
	}
	return serveErr
}
