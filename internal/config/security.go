package config

import (
	"fmt"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"
)

func (c *Config) validateSiteAndSecurity() error {
	validHeader := func(header string) bool {
		switch http.CanonicalHeaderKey(header) {
		case "X-Forwarded-For", "X-Real-Ip", "Cf-Connecting-Ip", "True-Client-Ip", "X-Client-Ip":
			return true
		}
		return false
	}
	if !validHeader(c.Server.ClientIPHeader) || !validHeader(c.CDN.ClientIPHeader) {
		return fmt.Errorf("unsupported client_ip_header")
	}
	if len(c.CDN.TrustedProxies) > 0 && !strings.EqualFold(c.Server.ClientIPHeader, "X-Forwarded-For") {
		return fmt.Errorf("CDN chains require server.client_ip_header = X-Forwarded-For")
	}
	for _, cidr := range c.CDN.TrustedProxies {
		prefix, err := netip.ParsePrefix(cidr)
		if err != nil || prefix.Bits() == 0 || (prefix.Addr().Is4In6() && prefix.Bits() <= 96) {
			return fmt.Errorf("invalid or unrestricted CDN proxy CIDR %q", cidr)
		}
	}

	if strings.TrimSpace(c.Site.Name) == "" || utf8.RuneCountInString(c.Site.Name) > 100 {
		return fmt.Errorf("site.name must contain 1 to 100 characters")
	}
	if strings.IndexFunc(c.Site.Name, unicode.IsControl) >= 0 {
		return fmt.Errorf("site.name must not contain control characters")
	}

	if c.Site.IconURL != "" {
		icon, err := url.Parse(c.Site.IconURL)
		if err != nil || icon.Hostname() == "" || (icon.Scheme != "https" && icon.Scheme != "http") || icon.User != nil || len(c.Site.IconURL) > 2048 {
			return fmt.Errorf("site.icon_url must be an absolute http(s) URL without credentials")
		}
	}

	if c.Security.MaxURLBytes < 256 || c.Security.MaxURLBytes > 1<<20 {
		return fmt.Errorf("security.max_url_bytes must be between 256 and 1048576")
	}
	if c.Security.MaxBodyBytes < 0 {
		return fmt.Errorf("security.max_body_bytes must be non-negative")
	}
	for i, host := range c.Security.AllowedHosts {
		host = strings.ToLower(strings.TrimSuffix(host, "."))
		if host == "" || strings.ContainsAny(host, "/\\@?# \t\r\n") {
			return fmt.Errorf("invalid allowed host %q", host)
		}
		if strings.Contains(host, ":") {
			if _, err := netip.ParseAddr(host); err != nil {
				return fmt.Errorf("allowed_hosts entries must not include ports")
			}
		}
		c.Security.AllowedHosts[i] = host
	}
	for _, cidr := range c.Security.AllowedIPs {
		if _, err := netip.ParsePrefix(cidr); err != nil {
			return fmt.Errorf("invalid allowed IP CIDR %q", cidr)
		}
	}
	return nil
}
