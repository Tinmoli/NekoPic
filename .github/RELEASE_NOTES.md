NekoPic 是一个用 Go 编写的随机图片服务。下载程序、放入图片即可运行，无需数据库，可用于随机壁纸、网站配图或个人图库。

### 功能

- 自定义图片分类，支持按电脑或手机选择分类，也能从全部分类中随机选图。
- 提供随机图片跳转、JSON 图片信息和全部图片地址接口。
- 自带首页和居中图片浏览页，网站名称、图标及网页样式均可修改。
- 支持 Nginx、1Panel OpenResty 和 CDN，通过可信代理获取访客 IP。
- 支持请求限流、访问 IP 限制、图库定时刷新，日志达到 100 MB 自动压缩。

### 下载哪个文件

| 系统 | 文件 |
|---|---|
| Windows，常见 Intel / AMD 电脑 | nekopic-windows-amd64.exe |
| Windows ARM64 | nekopic-windows-arm64.exe |
| Linux x86-64 | nekopic-linux-amd64 |
| Linux ARM64 | nekopic-linux-arm64 |
| Linux ARMv7 | nekopic-linux-armv7 |

SHA256SUMS 用于校验下载文件。Source code 是源码，直接使用请下载上表中的程序。

### 开始使用

1. 把程序放入独立文件夹。Windows 可直接双击；Linux 先赋予执行权限再运行。
2. 首次运行自动生成 config.toml、view、web、logs，然后退出。
3. 将图片放入 view/pc 和 view/pe，再次运行程序。
4. 浏览器打开 http://localhost:8505/。局域网设备使用服务器 IP 访问。

接口 /api 按设备选择图片，/dm 从全部分类中随机选图；网页浏览使用 /view/auto 或 /view/dm。

程序不附带图库。支持 JPEG、PNG、GIF、WebP，默认不限制像素数。具体配置与接口说明请看 [使用文档](https://github.com/Tinmoli/NekoPic#readme)。

### 更新已有安装

关闭旧程序后替换可执行文件，保留 config.toml、view、web 和 logs。配置和已修改的网页不会被新程序覆盖；开启自动分类识别时，程序会在检查通过后追加新分类。
