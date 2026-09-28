// Package webui 从磁盘加载可自定义页面；缺失或损坏时回退到内嵌默认。
package webui

import (
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"nekopic"
)

var funcMap = template.FuncMap{
	// "upper" 供自定义首页把分类名转成大写标签。
	"upper": strings.ToUpper,
}

// Pages 持有已解析的页面模板。模板启动时解析一次，改网页需重启。
type Pages struct {
	home        *template.Template
	viewer      *template.Template
	errPage     *template.Template
	cssPath     string
	cssFallback string
	log         *slog.Logger
}

// DefaultFiles 返回内嵌默认文件名到内容的映射。
func DefaultFiles() map[string]string {
	return map[string]string{
		"home.html":   nekopic.HomeHTML,
		"viewer.html": nekopic.ViewerHTML,
		"error.html":  nekopic.ErrorHTML,
		"style.css":   nekopic.StyleCSS,
	}
}

// Prepare 在 dir 下写出缺失的默认网页文件；已存在的文件绝不覆盖。
func Prepare(dir string) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create web directory: %w", err)
	}
	for name, content := range DefaultFiles() {
		file, err := os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("create web file %s: %w", name, err)
		}
		_, writeErr := io.WriteString(file, content)
		closeErr := file.Close()
		if writeErr != nil || closeErr != nil {
			return fmt.Errorf("write web file %s: %w", name, errors.Join(writeErr, closeErr))
		}
	}
	return nil
}

// Load 从 dir 读取页面模板；单个文件读取失败或模板语法错误时使用内嵌默认并记录警告。
func Load(dir string, logger *slog.Logger) (*Pages, error) {
	p := &Pages{
		cssPath:     filepath.Join(dir, "style.css"),
		cssFallback: nekopic.StyleCSS,
		log:         logger,
	}
	var err error
	if p.home, err = loadTemplate(dir, "home.html", nekopic.HomeHTML, logger); err != nil {
		return nil, err
	}
	if p.viewer, err = loadTemplate(dir, "viewer.html", nekopic.ViewerHTML, logger); err != nil {
		return nil, err
	}
	if p.errPage, err = loadTemplate(dir, "error.html", nekopic.ErrorHTML, logger); err != nil {
		return nil, err
	}
	return p, nil
}

func loadTemplate(dir, name, fallback string, logger *slog.Logger) (*template.Template, error) {
	path := filepath.Join(dir, name)
	data, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			logger.Warn("custom page unreadable; using embedded default", "file", path, "error", err)
		}
		data = nil
	}
	if data != nil {
		if parsed, parseErr := template.New(name).Funcs(funcMap).Parse(string(data)); parseErr == nil {
			return parsed, nil
		} else {
			logger.Warn("custom page has template errors; using embedded default", "file", path, "error", parseErr)
		}
	}
	parsed, err := template.New(name).Funcs(funcMap).Parse(fallback)
	if err != nil {
		return nil, fmt.Errorf("parse embedded %s: %w", name, err)
	}
	return parsed, nil
}

// Home 返回首页模板。
func (p *Pages) Home() *template.Template { return p.home }

// Viewer 返回浏览页模板。
func (p *Pages) Viewer() *template.Template { return p.viewer }

// Error 返回错误提示页模板。
func (p *Pages) Error() *template.Template { return p.errPage }

// CSS 返回样式表内容；每次调用都重新读磁盘，方便改样式后立即生效。
func (p *Pages) CSS() []byte {
	data, err := os.ReadFile(p.cssPath)
	if err != nil {
		return []byte(p.cssFallback)
	}
	return data
}
