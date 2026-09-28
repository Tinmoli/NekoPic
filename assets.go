// Package nekopic 保存随可执行文件发布的默认配置与网页，示例与生成内容共享同一来源。
package nekopic

import _ "embed"

//go:embed config.example.toml
var DefaultConfig string

//go:embed web/home.html
var HomeHTML string

//go:embed web/error.html
var ErrorHTML string

//go:embed web/viewer.html
var ViewerHTML string

//go:embed web/style.css
var StyleCSS string
