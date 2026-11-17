package Middlewares

import (
	"os"
	"strings"
)

// Caddy runs on the same host in production and reaches the backend over loopback.
var defaultTrustedProxies = []string{"127.0.0.1", "::1"}

// TrustedProxies returns the proxies whose X-Forwarded-For header gin may believe,
// from the comma-separated TRUSTED_PROXIES env var (IPs or CIDRs), defaulting to loopback.
// Trusting anyone else lets clients spoof ClientIP() and bypass the rate limiter.
func TrustedProxies() []string {
	return parseTrustedProxies(os.Getenv("TRUSTED_PROXIES"))
}

func parseTrustedProxies(value string) []string {
	var proxies []string
	for _, p := range strings.Split(value, ",") {
		if p = strings.TrimSpace(p); p != "" {
			proxies = append(proxies, p)
		}
	}
	if len(proxies) == 0 {
		return defaultTrustedProxies
	}
	return proxies
}
