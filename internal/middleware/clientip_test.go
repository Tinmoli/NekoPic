package middleware

import (
	"net/http/httptest"
	"testing"

	"nekopic/internal/config"
)

func TestCDNAndReverseProxyChains(t *testing.T) {
	server := config.Server{TrustedProxies: []string{"127.0.0.1/32", "10.0.0.0/8"}, ClientIPHeader: "X-Forwarded-For"}
	cdn := config.CDN{TrustedProxies: []string{"203.0.113.0/24"}, ClientIPHeader: "CF-Connecting-IP"}
	resolver, err := NewClientIPResolver(server, cdn)
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct{ name, peer, xff, cf, want string }{
		{"direct CDN", "203.0.113.8:80", "198.51.100.1", "198.51.100.1", "198.51.100.1"},
		{"CDN then Nginx", "127.0.0.1:80", "198.51.100.2, 203.0.113.8", "198.51.100.2", "198.51.100.2"},
		{"CDN then multiple proxies", "127.0.0.1:80", "198.51.100.3, 203.0.113.8, 10.0.0.2", "198.51.100.3", "198.51.100.3"},
		{"direct spoof", "192.0.2.1:80", "203.0.113.8", "198.51.100.4", "192.0.2.1"},
		{"spoof through Nginx", "127.0.0.1:80", "203.0.113.8, 192.0.2.1", "198.51.100.4", "192.0.2.1"},
		{"invalid CDN header fallback", "203.0.113.8:80", "198.51.100.5", "not-an-ip", "198.51.100.5"},
		{"missing chain through Nginx", "127.0.0.1:80", "", "198.51.100.6", "127.0.0.1"},
		{"IPv6 client", "203.0.113.8:80", "2001:db8::1", "2001:db8::1", "2001:db8::1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			req.RemoteAddr = tc.peer
			req.Header.Set("X-Forwarded-For", tc.xff)
			req.Header.Set("CF-Connecting-IP", tc.cf)
			if got := resolver.ClientIP(req); got != tc.want {
				t.Fatalf("got %s want %s", got, tc.want)
			}
		})
	}
}

func TestHeaderChoicesAndTrust(t *testing.T) {
	for _, header := range []string{"X-Forwarded-For", "X-Real-IP", "CF-Connecting-IP", "True-Client-IP", "X-Client-IP"} {
		t.Run(header, func(t *testing.T) {
			server := config.Server{TrustedProxies: []string{"127.0.0.1/32"}, ClientIPHeader: header}
			resolver, err := NewClientIPResolver(server, config.CDN{})
			if err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest("GET", "/", nil)
			req.RemoteAddr = "127.0.0.1:80"
			req.Header.Set(header, "198.51.100.2")
			if got := resolver.ClientIP(req); got != "198.51.100.2" {
				t.Fatal(got)
			}
			server.TrustedProxies = nil
			disabled, _ := NewClientIPResolver(server, config.CDN{})
			if got := disabled.ClientIP(req); got != "127.0.0.1" {
				t.Fatal("disabled proxy trusted a header")
			}
		})
	}
	server := config.Server{ClientIPHeader: "X-Forwarded-For"}
	cdn := config.CDN{TrustedProxies: []string{"203.0.113.0/24"}, ClientIPHeader: "CF-Connecting-IP"}
	disabled, _ := NewClientIPResolver(server, cdn)
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "203.0.113.8:80"
	req.Header.Set("CF-Connecting-IP", "198.51.100.2")
	if disabled.ClientIP(req) != "198.51.100.2" {
		t.Fatal("configured CDN did not resolve client")
	}
}
