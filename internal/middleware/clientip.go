package middleware

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"

	"nekopic/internal/config"
)

// IPResolver 将自有反向代理和 CDN 网段分开，防止直连者伪造 CDN 专用头。
type IPResolver struct {
	trusted     []netip.Prefix
	cdn         []netip.Prefix
	proxyHeader string
	cdnHeader   string
}

func NewIPResolver(cidrs []string) (*IPResolver, error) {
	return NewClientIPResolver(config.Server{TrustedProxies: cidrs, ClientIPHeader: "X-Forwarded-For"}, config.CDN{})
}

func NewClientIPResolver(server config.Server, cdn config.CDN) (*IPResolver, error) {
	trusted, err := parsePrefixes(server.TrustedProxies)
	if err != nil {
		return nil, err
	}
	resolver := &IPResolver{trusted: trusted, proxyHeader: server.ClientIPHeader, cdnHeader: cdn.ClientIPHeader}
	resolver.cdn, err = parsePrefixes(cdn.TrustedProxies)
	if err != nil {
		return nil, err
	}
	return resolver, nil
}

func parsePrefixes(cidrs []string) ([]netip.Prefix, error) {
	var prefixes []netip.Prefix
	for _, cidr := range cidrs {
		prefix, err := netip.ParsePrefix(cidr)
		if err != nil {
			return nil, fmt.Errorf("invalid proxy CIDR: %w", err)
		}
		if prefix.Addr().Is4In6() && prefix.Bits() >= 96 {
			prefix = netip.PrefixFrom(prefix.Addr().Unmap(), prefix.Bits()-96)
		}
		prefixes = append(prefixes, prefix.Masked())
	}
	return prefixes, nil
}

func contains(prefixes []netip.Prefix, ip netip.Addr) bool {
	for _, prefix := range prefixes {
		if prefix.Contains(ip) {
			return true
		}
	}
	return false
}

func parseIP(text string) (netip.Addr, bool) {
	ip, err := netip.ParseAddr(strings.TrimSpace(text))
	if err != nil || ip.Zone() != "" {
		return netip.Addr{}, false
	}
	return ip.Unmap(), true
}

func forwardedChain(req *http.Request) ([]netip.Addr, bool) {
	header := strings.Join(req.Header.Values("X-Forwarded-For"), ",")
	if header == "" {
		return nil, false
	}
	parts := strings.Split(header, ",")
	chain := make([]netip.Addr, len(parts))
	for i, part := range parts {
		ip, ok := parseIP(part)
		if !ok {
			return nil, false
		}
		chain[i] = ip
	}
	return chain, true
}

func singleHeaderIP(req *http.Request, header string) (netip.Addr, bool) {
	values := req.Header.Values(header)
	if len(values) != 1 {
		return netip.Addr{}, false
	}
	return parseIP(values[0])
}

func (r *IPResolver) ClientIP(req *http.Request) string {
	host, _, err := net.SplitHostPort(req.RemoteAddr)
	if err != nil {
		host = req.RemoteAddr
	}
	peer, ok := parseIP(host)
	if !ok {
		return "unknown"
	}
	peerTrusted := contains(r.trusted, peer)
	peerCDN := contains(r.cdn, peer)
	if !peerTrusted && !peerCDN {
		return peer.String()
	}

	chain, chainOK := forwardedChain(req)
	// 从右往左跳过自己信任的代理，找出真正的访客来源；
	// 访客自己伪造的转发头内容不会被信任。
	edge := peer
	if peerTrusted && !peerCDN && chainOK {
		for i := len(chain) - 1; i >= 0; i-- {
			edge = chain[i]
			if !contains(r.trusted, edge) {
				break
			}
		}
	}
	if len(r.cdn) > 0 && contains(r.cdn, edge) && !strings.EqualFold(r.cdnHeader, "X-Forwarded-For") {
		if ip, ok := singleHeaderIP(req, r.cdnHeader); ok {
			return ip.String()
		}
	}

	// 没有 CDN 链时，也可接受可信 Nginx/OpenResty 覆盖写入的 X-Real-IP。
	if len(r.cdn) == 0 && peerTrusted && !strings.EqualFold(r.proxyHeader, "X-Forwarded-For") {
		if ip, ok := singleHeaderIP(req, r.proxyHeader); ok {
			return ip.String()
		}
		return peer.String()
	}
	if !chainOK {
		return peer.String()
	}

	for i := len(chain) - 1; i >= 0; i-- {
		ip := chain[i]
		if (!contains(r.trusted, ip) && !contains(r.cdn, ip)) || i == 0 {
			return ip.String()
		}
	}
	return peer.String()
}
