// Package httpapi 提供兼容随机图片接口及本地图片文件服务。
package httpapi

import (
	"bytes"
	"crypto/rand"
	"log/slog"
	mathrand "math/rand/v2"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"

	"nekopic/internal/catalog"
	"nekopic/internal/config"
	"nekopic/internal/middleware"
	"nekopic/internal/webui"
)

type API struct {
	engine   *gin.Engine
	cfg      config.Config
	catalog  *catalog.Manager
	limiter  *middleware.Limiter
	resolver *middleware.IPResolver
	pages    *webui.Pages
	log      *slog.Logger
	stopping atomic.Bool
	pick     func(int) int
}

func New(cfg config.Config, manager *catalog.Manager, limiter *middleware.Limiter, pages *webui.Pages, logger *slog.Logger) (*API, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	resolver, err := middleware.NewClientIPResolver(cfg.Server, cfg.CDN)
	if err != nil {
		return nil, err
	}
	guard, err := securityPolicy(cfg.Security, resolver)
	if err != nil {
		return nil, err
	}

	a := &API{
		cfg: cfg, catalog: manager, limiter: limiter, pages: pages,
		resolver: resolver, log: logger, pick: mathrand.IntN,
	}

	e := gin.New()
	e.RedirectTrailingSlash = false
	e.RedirectFixedPath = false
	e.HandleMethodNotAllowed = true

	// 客户端地址统一交给 IPResolver，避免继承 Gin 的默认代理信任行为。
	if err := e.SetTrustedProxies(nil); err != nil {
		return nil, err
	}

	e.Use(a.observe, guard)
	e.NoRoute(func(c *gin.Context) { a.failPage(c, 404, "404", "not found", 0) })
	e.NoMethod(func(c *gin.Context) {
		// 全站只提供 GET 和 HEAD，其余方式一律拒绝。
		c.Header("Allow", "GET, HEAD")
		a.failPage(c, 405, "405", "method not allowed", 0)
	})

	// 本服务全部是只读接口，每个地址同时支持 GET 和 HEAD。
	get := func(path string, handler gin.HandlerFunc) {
		e.GET(path, handler)
		e.HEAD(path, handler)
	}
	get("/", a.home)
	get("/favicon.ico", a.favicon)
	get("/assets/style.css", a.style)
	get("/api", a.adaptive)
	get("/dm", func(c *gin.Context) { a.random(c, "dm") })
	get("/view/dm", a.viewerFor("dm"))
	get("/view/auto", a.viewerFor("auto"))

	// 分类在启动期已知，逐个注册字面量路由；dm 之外每个分类都有完整端点。
	for _, category := range cfg.Categories {
		name := category.Name
		get("/"+name, func(c *gin.Context) { a.random(c, name) })
		get("/view/"+name, a.viewerFor(name))
		get("/"+name+"/*filename", a.staticFor(name))
	}

	get("/test", func(c *gin.Context) {
		c.Data(200, "text/plain; charset=utf-8", []byte("Hello World"))
	})
	get("/health/live", func(c *gin.Context) { jsonResponse(c, 200, gin.H{"status": "ok"}) })
	get("/health/ready", a.ready)

	a.engine = e
	return a, nil
}

func (a *API) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.engine.ServeHTTP(w, r)
}

// Stop 在停止监听前将就绪探针切为未就绪。
func (a *API) Stop() {
	a.stopping.Store(true)
}

func jsonResponse(c *gin.Context, status int, body any) {
	c.Header("Content-Type", "application/json; charset=utf-8")
	c.JSON(status, body)
}

func fail(c *gin.Context, status int, code, msg string) {
	c.Set("business_error", code)
	c.Header("Cache-Control", "no-store")
	jsonResponse(c, status, gin.H{"code": code, "msg": msg})
	c.Abort()
}

// wantsHTML 通过 Accept 头判断访问者是不是浏览器网页。
func wantsHTML(c *gin.Context) bool {
	for _, part := range strings.Split(c.GetHeader("Accept"), ",") {
		if strings.TrimSpace(strings.SplitN(part, ";", 2)[0]) == "text/html" {
			return true
		}
	}
	return false
}

// errorTitles 是错误页的默认标题，msg 仍会作为正文展示。
var errorTitles = map[string]string{
	"404": "找不到这个页面",
	"405": "请求方式不支持",
	"429": "休息一下再来看图",
	"503": "服务正忙",
}

// failPage 在浏览器访问时展示友好的错误页，程序调用仍返回 JSON。
// retry 大于 0 时，页面会在对应秒数后自动重试（配合限流的 Retry-After）。
func (a *API) failPage(c *gin.Context, status int, code, msg string, retry int) {
	if !wantsHTML(c) {
		fail(c, status, code, msg)
		return
	}
	c.Set("business_error", code)
	c.Header("Cache-Control", "no-store")
	title, ok := errorTitles[code]
	if !ok {
		title = msg
	}
	data := struct {
		Name, Icon, Code, Title, Message string
		Retry                            int
	}{
		Name: a.cfg.Site.Name, Icon: a.cfg.Site.IconURL,
		Code: code, Title: title, Message: msg, Retry: retry,
	}
	var output bytes.Buffer
	if err := a.pages.Error().Execute(&output, data); err != nil {
		a.log.Error("render error page", "error", err)
		fail(c, status, code, msg)
		return
	}
	c.Data(status, "text/html; charset=utf-8", output.Bytes())
	c.Abort()
}

func (a *API) observe(c *gin.Context) {
	start := time.Now()
	id := rand.Text()
	c.Header("X-Request-ID", id)
	c.Header("Cache-Control", "no-store")
	c.Header("X-Content-Type-Options", "nosniff")

	defer func() {
		if recovered := recover(); recovered != nil {
			a.log.Error("request panic", "request_id", id)
			if !c.Writer.Written() {
				fail(c, 500, "500", "internal server error")
			} else {
				c.Abort()
			}
		}

		route := c.FullPath()
		if route == "" {
			route = "unmatched"
		}
		fields := []any{"request_id", id, "route", route,
			"status", c.Writer.Status(), "business_error", c.GetString("business_error"), "duration", time.Since(start)}
		if a.cfg.Log.ClientIP {
			fields = append(fields, "client_ip", a.resolver.ClientIP(c.Request))
		}
		a.log.Info("request", fields...)
	}()

	c.Next()
}
