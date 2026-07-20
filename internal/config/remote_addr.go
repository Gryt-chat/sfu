package config

import (
	"net/http"
	"strings"
)

// ClientRemoteAddr returns a client address string for logs and recovery context.
//
// When Proxy is enabled and X-Forwarded-For is present, the first (leftmost) hop
// is returned — the original client when trusted proxies append to the chain.
// Otherwise r.RemoteAddr is returned (typically "ip:port" of the TCP peer).
//
// This is for observability only; do not use it for auth or ACL decisions.
func (c *Config) ClientRemoteAddr(r *http.Request) string {
	if c != nil && c.Proxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			if i := strings.IndexByte(xff, ','); i >= 0 {
				xff = xff[:i]
			}
			if addr := strings.TrimSpace(xff); addr != "" {
				return addr
			}
		}
	}
	return r.RemoteAddr
}
