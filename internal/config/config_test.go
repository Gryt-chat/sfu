package config

import (
	"testing"
	"time"
)

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

// The liveness settings decide who gets hung up on, so the two ways of getting
// them wrong both have to be safe: a value nobody set, and a value somebody set
// too tight.

func TestPingDefaultsWhenUnset(t *testing.T) {
	t.Setenv("SFU_PING_INTERVAL", "")
	t.Setenv("SFU_PONG_TIMEOUT", "")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PingInterval != DefaultPingInterval {
		t.Errorf("ping interval = %s, want %s", cfg.PingInterval, DefaultPingInterval)
	}
	if cfg.PongTimeout != DefaultPongTimeout {
		t.Errorf("pong timeout = %s, want %s", cfg.PongTimeout, DefaultPongTimeout)
	}
}

func TestPingIsTakenFromTheEnvironment(t *testing.T) {
	t.Setenv("SFU_PING_INTERVAL", "10")
	t.Setenv("SFU_PONG_TIMEOUT", "45")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PingInterval != 10*time.Second {
		t.Errorf("ping interval = %s, want 10s", cfg.PingInterval)
	}
	if cfg.PongTimeout != 45*time.Second {
		t.Errorf("pong timeout = %s, want 45s", cfg.PongTimeout)
	}
}

// Zero is a value here rather than a missing one: it is how the whole mechanism
// is switched off without a release, which is the escape hatch if it ever turns
// out to be hanging up on people who were fine.
func TestZeroPingIntervalIsKept(t *testing.T) {
	t.Setenv("SFU_PING_INTERVAL", "0")
	t.Setenv("SFU_PONG_TIMEOUT", "")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PingInterval != 0 {
		t.Fatalf("ping interval = %s, want it left at 0", cfg.PingInterval)
	}
}

// A timeout under two intervals hangs up on a peer that lost a single pong,
// which is everybody, eventually.
func TestATooTightTimeoutIsRaised(t *testing.T) {
	t.Setenv("SFU_PING_INTERVAL", "30")
	t.Setenv("SFU_PONG_TIMEOUT", "20")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PongTimeout != 60*time.Second {
		t.Fatalf("pong timeout = %s, want it raised to 60s", cfg.PongTimeout)
	}
}

// Nothing to raise when there are no pings to miss.
func TestTimeoutIsLeftAloneWhenPingingIsOff(t *testing.T) {
	t.Setenv("SFU_PING_INTERVAL", "0")
	t.Setenv("SFU_PONG_TIMEOUT", "5")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PongTimeout != 5*time.Second {
		t.Fatalf("pong timeout = %s, want 5s", cfg.PongTimeout)
	}
}

func TestUnreadablePingValuesFallBack(t *testing.T) {
	for _, value := range []string{"-1", "thirty", "30s"} {
		t.Setenv("SFU_PING_INTERVAL", value)
		t.Setenv("SFU_PONG_TIMEOUT", "")

		cfg, err := Load()
		if err != nil {
			t.Fatal(err)
		}
		if cfg.PingInterval != DefaultPingInterval {
			t.Errorf("%q gave ping interval %s, want %s", value, cfg.PingInterval, DefaultPingInterval)
		}
	}
}
