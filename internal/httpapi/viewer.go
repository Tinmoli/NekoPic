package httpapi

import (
	"github.com/gin-gonic/gin"
)

func (a *API) viewerFor(category string) gin.HandlerFunc {
	return func(c *gin.Context) {
		actual := category
		if category == "auto" {
			actual = a.cfg.Adaptive.Desktop
			if mobileUA.MatchString(c.Request.UserAgent()) {
				actual = a.cfg.Adaptive.Mobile
			}
			c.Header("Vary", "User-Agent")
		}
		a.viewer(c, actual)
	}
}

func (a *API) viewer(c *gin.Context, category string) {
	if category != "dm" && !a.knownCategory(category) {
		fail(c, 404, "404", "not found")
		return
	}
	if !a.allowRandom(c) {
		return
	}

	snapshot := a.catalog.State().Snapshot
	data := struct {
		Name, Icon, Image, Next string
	}{
		Name: a.cfg.Site.Name, Icon: a.cfg.Site.IconURL,
		Next: "/view/" + category,
	}
	if count := snapshot.Count(category); count > 0 {
		data.Image = snapshot.At(category, a.pick(count)).PublicURL
	}

	a.render(c, a.pages.Viewer(), "render image page", data)
}

func (a *API) knownCategory(name string) bool {
	for _, category := range a.cfg.Categories {
		if category.Name == name {
			return true
		}
	}
	return false
}
