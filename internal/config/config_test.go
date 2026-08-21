package config

import "testing"

// The mux port is the only UDP port now, so an unset one has to land somewhere
// rather than leaving pion to pick ephemeral ports nobody opened.
func TestMuxPortDefaultsWhenUnset(t *testing.T) {
	t.Setenv("ICE_UDP_MUX_PORT", "")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ICEUDPMuxPort != DefaultICEUDPMuxPort {
		t.Fatalf("mux port = %d, want %d", cfg.ICEUDPMuxPort, DefaultICEUDPMuxPort)
	}
}

func TestMuxPortIsTakenFromTheEnvironment(t *testing.T) {
	t.Setenv("ICE_UDP_MUX_PORT", "443")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ICEUDPMuxPort != 443 {
		t.Fatalf("mux port = %d, want 443", cfg.ICEUDPMuxPort)
	}
}

// A port number that cannot be bound falls back rather than failing to parse
// into a zero and leaving the mux unset.
func TestOutOfRangeMuxPortFallsBack(t *testing.T) {
	for _, value := range []string{"0", "-1", "70000", "not-a-port"} {
		t.Setenv("ICE_UDP_MUX_PORT", value)

		cfg, err := Load()
		if err != nil {
			t.Fatal(err)
		}
		if cfg.ICEUDPMuxPort != DefaultICEUDPMuxPort {
			t.Fatalf("%q gave mux port %d, want %d", value, cfg.ICEUDPMuxPort, DefaultICEUDPMuxPort)
		}
	}
}

// MAX_PEERS used to be derived from the size of the UDP port range. With the
// range gone it is a plain guardrail, set or defaulted.
func TestMaxPeersDefaultsWithoutAPortRange(t *testing.T) {
	t.Setenv("MAX_PEERS", "")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MaxPeers != DefaultMaxPeers {
		t.Fatalf("max peers = %d, want %d", cfg.MaxPeers, DefaultMaxPeers)
	}
}

func TestMaxPeersIsTakenFromTheEnvironment(t *testing.T) {
	t.Setenv("MAX_PEERS", "12")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MaxPeers != 12 {
		t.Fatalf("max peers = %d, want 12", cfg.MaxPeers)
	}
}
