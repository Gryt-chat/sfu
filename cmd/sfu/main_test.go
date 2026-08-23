package main

import (
	"net"
	"testing"
)

func TestShouldBindICEUDPAddressSkipsLinkLocal(t *testing.T) {
	tests := []struct {
		name string
		ip   string
		want bool
	}{
		{name: "ipv6 link local", ip: "fe80::1", want: false},
		{name: "ipv4 link local", ip: "169.254.10.20", want: false},
		{name: "private ipv4", ip: "192.168.1.20", want: true},
		{name: "global ipv6", ip: "2001:db8::1", want: true},
		{name: "loopback", ip: "127.0.0.1", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldBindICEUDPAddress(net.ParseIP(tt.ip)); got != tt.want {
				t.Fatalf("shouldBindICEUDPAddress(%q) = %t, want %t", tt.ip, got, tt.want)
			}
		})
	}
}
