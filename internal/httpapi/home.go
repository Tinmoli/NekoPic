package httpapi

import (
	"bytes"
	"html/template"
	"net/http"

	"github.com/gin-gonic/gin"
)

type categoryCard struct {
	Name  string
	Count int
}

func (a *API) home(c *gin.Context) {
	snapshot := a.catalog.State().Snapshot
	cards := make([]categoryCard, 0, len(a.cfg.Categories))
	for _, category := range a.cfg.Categories {
		cards = append(cards, categoryCard{Name: category.Name, Count: snapshot.Count(category.Name)})
	}
	data := struct {
		Name, Icon string
		Total      int
		Categories []categoryCard
	}{
		Name: a.cfg.Site.Name, Icon: a.cfg.Site.IconURL,
		Total:      snapshot.Count("dm"),
		Categories: cards,
	}

	// 站点名等动态内容一律交给模板自动转义，不标记成"安全内容"绕过。
	a.render(c, a.pages.Home(), "render homepage", data)
}

func (a *API) favicon(c *gin.Context) {
	if a.cfg.Site.IconURL != "" {
		c.Redirect(http.StatusFound, a.cfg.Site.IconURL)
		return
	}
	c.Status(http.StatusNoContent)
}

func (a *API) style(c *gin.Context) {
	c.Data(http.StatusOK, "text/css; charset=utf-8", a.pages.CSS())
}

func (a *API) render(c *gin.Context, tmpl *template.Template, what string, data any) {
	var output bytes.Buffer
	if err := tmpl.Execute(&output, data); err != nil {
		a.log.Error(what, "error", err)
		fail(c, 500, "500", "internal server error")
		return
	}
	c.Data(http.StatusOK, "text/html; charset=utf-8", output.Bytes())
}
