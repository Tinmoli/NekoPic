package httpapi

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"

	"github.com/gin-gonic/gin"

	"nekopic/internal/config"
	"nekopic/internal/middleware"
)

func securityPolicy(cfg config.Security, resolver *middleware.IPResolver) (gin.HandlerFunc, error) {
	hosts := make(map[string]bool, len(cfg.AllowedHosts))
	for _, host := range cfg.AllowedHosts {
		hosts[strings.ToLower(strings.TrimSuffix(host, "."))] = true
	}
	prefixes := make([]netip.Prefix, 0, len(cfg.AllowedIPs))
	for _, cidr := range cfg.AllowedIPs {
		prefix, err := netip.ParsePrefix(cidr)
		if err != nil {
			return nil, fmt.Errorf("invalid allowed IP CIDR: %w", err)
		}
		if prefix.Addr().Is4In6() && prefix.Bits() >= 96 {
			prefix = netip.PrefixFrom(prefix.Addr().Unmap(), prefix.Bits()-96)
		}
		prefixes = append(prefixes, prefix.Masked())
	}

	return func(c *gin.Context) {
		if cfg.HeadersEnabled {
			ancestors := "'none'"
			if cfg.AllowEmbedding {
				ancestors = "*"
			} else {
				c.Header("X-Frame-Options", "DENY")
			}
			// 自定义首页允许内联样式；脚本、对象、跳转等保持全禁。
			styles := "'self'"
			if cfg.InlineStyles {
				styles = "'self' 'unsafe-inline'"
			}
			c.Header("Content-Security-Policy", "default-src 'none'; script-src 'none'; style-src "+styles+"; img-src 'self' http: https:; connect-src 'self'; object-src 'none'; base-uri 'none'; form-action 'none'; frame-ancestors "+ancestors)
			c.Header("Referrer-Policy", "no-referrer")
			c.Header("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
			c.Header("X-Content-Type-Options", "nosniff")
		}

		if len(c.Request.RequestURI) > cfg.MaxURLBytes {
			fail(c, http.StatusRequestURITooLong, "414", "request URL too long")
			return
		}
		// 目前没有接收请求体的业务；未知长度的流式请求直接拒绝。
		if c.Request.ContentLength > cfg.MaxBodyBytes || len(c.Request.TransferEncoding) > 0 {
			fail(c, http.StatusRequestEntityTooLarge, "413", "request body too large")
			return
		}
		if c.Request.Body != nil {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, cfg.MaxBodyBytes)
		}

		host := c.Request.Host
		if value, _, err := net.SplitHostPort(host); err == nil {
			host = value
		}
		host = strings.ToLower(strings.TrimSuffix(strings.Trim(host, "[]"), "."))
		if len(hosts) > 0 && !hosts[host] {
			fail(c, http.StatusForbidden, "403", "host not allowed")
			return
		}
		if len(prefixes) > 0 {
			ip, err := netip.ParseAddr(resolver.ClientIP(c.Request))
			permitted := false
			if err == nil {
				for _, prefix := range prefixes {
					if prefix.Contains(ip.Unmap()) {
						permitted = true
						break
					}
				}
			}
			if !permitted {
				fail(c, http.StatusForbidden, "403", "client IP not allowed")
				return
			}
		}
		c.Next()
	}, nil
}
