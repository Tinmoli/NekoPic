package httpapi

import (
	"bufio"
	"net/http"
	"net/url"
	"regexp"
	"strconv"

	"github.com/gin-gonic/gin"

	"nekopic/internal/catalog"
	"nekopic/internal/model"
)

var mobileUA = regexp.MustCompile(
	`(?i)Android|iP(hone|od)|Windows ?CE|Symbian|Mobile|Opera Mobi|` +
		`BlackBerry|Palm(OS)?|PocketPC|SonyEricsson|Nokia|Vodafone|` +
		`HTC_|SAMSUNG-SGH|armv[56]l|Go\.Web|J2ME/MIDP`,
)

func (a *API) adaptive(c *gin.Context) {
	c.Header("Vary", "User-Agent")
	target := "/" + a.cfg.Adaptive.Desktop
	if mobileUA.MatchString(c.Request.UserAgent()) {
		target = "/" + a.cfg.Adaptive.Mobile
	}

	// 保持兼容：自适应入口不透传 return 等查询参数。
	c.Redirect(http.StatusFound, target)
}

func (a *API) allowRandom(c *gin.Context) bool {
	decision := a.limiter.Allow(a.resolver.ClientIP(c.Request))
	if decision.Status != 0 {
		c.Header("Retry-After", strconv.Itoa(decision.RetryAfter))
		if decision.Status == 429 {
			a.failPage(c, 429, "429", "请求太快啦，请稍后再试", decision.RetryAfter)
		} else {
			a.failPage(c, 503, "503", "此刻访问的人太多，请稍后再来", decision.RetryAfter)
		}
		return false
	}
	return true
}

func (a *API) random(c *gin.Context, category string) {
	if !a.allowRandom(c) {
		return
	}

	query, err := url.ParseQuery(c.Request.URL.RawQuery)
	if err != nil {
		fail(c, 400, "400", "invalid query string")
		return
	}
	values := query["return"]
	mode := query.Get("return")
	if len(values) > 1 || (mode != "" && mode != "json" && mode != "all") {
		fail(c, 400, "400", "invalid return parameter")
		return
	}

	// 每次请求只看一份图片清单，避免响应写到一半换成新一轮扫描的结果。
	snapshot := a.catalog.State().Snapshot
	count := snapshot.Count(category)
	if count == 0 {
		if mode == "json" {
			// 兼容旧接口约定：库空时 HTTP 仍是 200，用业务 code "500" 表示。
			fail(c, 200, "500", "no images available")
		} else {
			fail(c, 404, "404", "no images available")
		}
		return
	}

	if mode == "all" {
		a.writeAll(c, snapshot, category)
		return
	}

	img := snapshot.At(category, a.pick(count))
	if mode == "json" {
		jsonResponse(c, 200, model.ImageResponse{
			Code: "200", AcgURL: img.PublicURL,
			Width: strconv.Itoa(img.Width), Height: strconv.Itoa(img.Height),
			Size: img.Extension,
		})
		return
	}
	c.Redirect(http.StatusFound, img.PublicURL)
}

func (a *API) writeAll(c *gin.Context, snapshot *catalog.Snapshot, category string) {
	c.Header("Content-Type", "text/plain; charset=utf-8")
	c.Status(http.StatusOK)
	writer := bufio.NewWriter(c.Writer)

	for i := 0; i < snapshot.Count(category); i++ {
		if c.Request.Context().Err() != nil {
			return
		}
		if _, err := writer.WriteString(snapshot.At(category, i).PublicURL + "\n"); err != nil {
			a.log.Debug("list output stopped", "error", err)
			return
		}
	}

	// 已开始流式输出，失败时不能再向纯文本响应追加 JSON。
	if err := writer.Flush(); err != nil {
		a.log.Debug("list output stopped", "error", err)
	}
}
