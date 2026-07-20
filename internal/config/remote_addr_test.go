package config

import (
	"net/http"
	"testing"
)

func TestClientRemoteAddr(t *testing.T) {
	req := func(remote string, xff string) *http.Request {
		r := &http.Request{
			RemoteAddr: remote,
			Header:     make(http.Header),
		}
		if xff != "" {
			r.Header.Set("X-Forwarded-For", xff)
		}
		return r
	}

	tests := []struct {
		name   string
		cfg    *Config
		remote string
		xff    string
		want   string
	}{
		{
			name:   "proxy off ignores xff",
			cfg:    &Config{Proxy: false},
			remote: "10.0.0.1:1234",
			xff:    "203.0.113.9",
			want:   "10.0.0.1:1234",
		},
		{
			name:   "proxy on uses single xff hop",
			cfg:    &Config{Proxy: true},
			remote: "10.0.0.1:1234",
			xff:    "203.0.113.9",
			want:   "203.0.113.9",
		},
		{
			name:   "proxy on uses first hop of chain",
			cfg:    &Config{Proxy: true},
			remote: "10.0.0.1:1234",
			xff:    "203.0.113.9, 10.0.0.5, 10.0.0.1",
			want:   "203.0.113.9",
		},
		{
			name:   "proxy on trims whitespace",
			cfg:    &Config{Proxy: true},
			remote: "10.0.0.1:1234",
			xff:    "  203.0.113.9  , 10.0.0.5",
			want:   "203.0.113.9",
		},
		{
			name:   "proxy on falls back when xff missing",
			cfg:    &Config{Proxy: true},
			remote: "10.0.0.1:1234",
			xff:    "",
			want:   "10.0.0.1:1234",
		},
		{
			name:   "proxy on falls back when xff blank",
			cfg:    &Config{Proxy: true},
			remote: "10.0.0.1:1234",
			xff:    "   ",
			want:   "10.0.0.1:1234",
		},
		{
			name:   "nil config uses remote addr",
			cfg:    nil,
			remote: "10.0.0.1:1234",
			xff:    "203.0.113.9",
			want:   "10.0.0.1:1234",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.cfg.ClientRemoteAddr(req(tt.remote, tt.xff))
			if got != tt.want {
				t.Fatalf("ClientRemoteAddr() = %q, want %q", got, tt.want)
			}
		})
	}
}
