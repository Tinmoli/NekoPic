package httpapi

import (
	"fmt"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"

	"nekopic/internal/safefile"
)

func (a *API) staticFor(category string) gin.HandlerFunc {
	prefix := "/" + category + "/"
	return func(c *gin.Context) {
		path := c.Request.URL.Path
		// URL.Path 已由 net/http 解码一次，此处不能再次解码。
		if len(path) <= len(prefix) || !safefile.ValidName(path[len(prefix):]) {
			fail(c, 404, "404", "not found")
			return
		}

		snapshot := a.catalog.State().Snapshot
		img, ok := snapshot.Lookup(path)
		if !ok {
			fail(c, 404, "404", "not found")
			return
		}

		f, err := a.catalog.Open(img)
		if err != nil {
			if os.IsNotExist(err) {
				fail(c, 404, "404", "not found")
			} else {
				fail(c, 500, "500", "failed to read image")
			}
			return
		}
		defer f.Close()

		info, err := f.Stat()
		if err != nil {
			fail(c, 500, "500", "failed to read image")
			return
		}

		c.Header("Content-Type", "image/"+img.Format)
		c.Header("Cache-Control", fmt.Sprintf("public, max-age=%d", a.cfg.Static.CacheMaxAge))
		http.ServeContent(c.Writer, c.Request, img.Filename, info.ModTime(), f)
	}
}

func (a *API) ready(c *gin.Context) {
	state := a.catalog.State()
	snapshot := state.Snapshot
	counts := make(gin.H, len(a.cfg.Categories))
	total := 0
	for _, category := range a.cfg.Categories {
		n := snapshot.Count(category.Name)
		counts[category.Name] = n
		total += n
	}
	ready := !a.stopping.Load() && total > 0
	status, code := "ok", 200
	if !ready {
		status, code = "not_ready", 503
	}

	var version uint64
	var scanned any
	if snapshot != nil {
		version = snapshot.Version
		scanned = snapshot.ScannedAt
	}
	jsonResponse(c, code, gin.H{
		"status":           status,
		"categories":       counts,
		"total":            total,
		"snapshot_version": version, "last_successful_scan": scanned,
		"refresh_degraded": state.Degraded,
	})
}
