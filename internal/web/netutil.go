package web

import (
	"net"
	"strings"
)

// remoteHost extracts host from RemoteAddr ("host:port"), falling back to input.
func remoteHost(remoteAddr string) string {
	if remoteAddr == "" {
		return ""
	}
	host, _, err := net.SplitHostPort(remoteAddr)
	if err == nil {
		if i := strings.Index(host, "%"); i >= 0 {
			host = host[:i]
		}
		return host
	}
	return remoteAddr
}

// clientIPKey normalizes the request peer address for rate-limit bucketing.
func clientIPKey(remoteAddr string) string {
	host := strings.TrimSpace(remoteHost(remoteAddr))
	if host == "" {
		return "unknown"
	}
	return host
}

// isTrustedSSOSource restricts SSO header trust to loopback clients.
// This prevents direct Internet/LAN requests from spoofing Remote-User.
func isTrustedSSOSource(remoteAddr string) bool {
	host := strings.TrimSpace(remoteHost(remoteAddr))
	if host == "" {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
