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

// Ending a call somebody is alone in is the one setting here that hangs up on
// a person who is doing nothing wrong, so both the default and the off switch
// have to be exactly what they say.

func TestCallAloneTimeoutDefaultsWhenUnset(t *testing.T) {
	t.Setenv("SFU_CALL_ALONE_TIMEOUT", "")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.CallAloneTimeout != DefaultCallAloneTimeout {
		t.Errorf("call alone timeout = %s, want %s", cfg.CallAloneTimeout, DefaultCallAloneTimeout)
	}
}

func TestCallAloneTimeoutIsTakenFromTheEnvironment(t *testing.T) {
	t.Setenv("SFU_CALL_ALONE_TIMEOUT", "300")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.CallAloneTimeout != 5*time.Minute {
		t.Errorf("call alone timeout = %s, want 5m", cfg.CallAloneTimeout)
	}
}

func TestCallAloneTimeoutZeroIsOff(t *testing.T) {
	// Zero has to survive as zero rather than falling back to the default,
	// which is the whole point of reading it the long way round: this is how a
	// server owner says a call should stay up until somebody closes it.
	t.Setenv("SFU_CALL_ALONE_TIMEOUT", "0")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.CallAloneTimeout != 0 {
		t.Errorf("call alone timeout = %s, want 0 — zero is the off switch, not an absence", cfg.CallAloneTimeout)
	}
}

func TestCallAloneTimeoutIgnoresNonsense(t *testing.T) {
	t.Setenv("SFU_CALL_ALONE_TIMEOUT", "two minutes")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.CallAloneTimeout != DefaultCallAloneTimeout {
		t.Errorf("call alone timeout = %s, want the default back", cfg.CallAloneTimeout)
	}
}

// Forcing an address turns discovery off by itself. Without this the SFU keeps
// gathering a server-reflexive candidate carrying whatever address its current
// egress happens to have, which is how a stale advertised address goes
// unnoticed: the reflexive one quietly carries the call instead. GRYT-768.
func TestForcingAnAddressDisablesSTUN(t *testing.T) {
	t.Setenv("ICE_ADVERTISE_IP", "203.0.113.10")
	t.Setenv("DISABLE_STUN", "")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.DisableSTUN {
		t.Fatal("DisableSTUN = false, want true when ICE_ADVERTISE_IP is set")
	}
	if len(cfg.ICEServers) != 0 {
		t.Fatalf("ICEServers = %v, want none so no reflexive candidate is gathered", cfg.ICEServers)
	}
}

// Nothing forced means nothing is known about the public address, which is
// exactly when discovery earns its place.
func TestSTUNStaysOnWithoutAnAdvertisedAddress(t *testing.T) {
	t.Setenv("ICE_ADVERTISE_IP", "")
	t.Setenv("DISABLE_STUN", "")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DisableSTUN {
		t.Fatal("DisableSTUN = true, want false when no address is forced")
	}
	if len(cfg.ICEServers) == 0 {
		t.Fatal("ICEServers is empty, want the STUN servers")
	}
}

// The default is a default, not a rule. An operator who wants both a forced
// address and discovery says so and gets it.
func TestExplicitDisableSTUNWinsBothWays(t *testing.T) {
	t.Setenv("ICE_ADVERTISE_IP", "203.0.113.10")
	t.Setenv("DISABLE_STUN", "false")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DisableSTUN {
		t.Fatal("DisableSTUN = true, want false when set explicitly alongside a forced address")
	}

	t.Setenv("ICE_ADVERTISE_IP", "")
	t.Setenv("DISABLE_STUN", "true")

	cfg, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.DisableSTUN {
		t.Fatal("DisableSTUN = false, want true when set explicitly with no forced address")
	}
}

// A value that is not an IP can never become a candidate, so it is dropped
// rather than carried into the rewrite rules where it would fail silently.
func TestAdvertisedAddressesThatAreNotIPsAreDropped(t *testing.T) {
	t.Setenv("ICE_ADVERTISE_IP", "203.0.113.10, sfu.example.com, ,198.51.100.4")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"203.0.113.10", "198.51.100.4"}
	if len(cfg.ICEAdvertiseIPs) != len(want) {
		t.Fatalf("advertised = %v, want %v", cfg.ICEAdvertiseIPs, want)
	}
	for i, w := range want {
		if cfg.ICEAdvertiseIPs[i] != w {
			t.Fatalf("advertised[%d] = %q, want %q", i, cfg.ICEAdvertiseIPs[i], w)
		}
	}
}

// A private address is kept, because a LAN peer really can use it, but it is
// worth warning about: on its own it means nobody outside the network can
// connect. A stale one is how that happens without anybody noticing.
func TestPrivateAdvertisedAddressIsKept(t *testing.T) {
	t.Setenv("ICE_ADVERTISE_IP", "203.0.113.10,192.168.1.50")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.ICEAdvertiseIPs) != 2 {
		t.Fatalf("advertised = %v, want both entries kept", cfg.ICEAdvertiseIPs)
	}
}

// A LAN address next to a public one is the documented multi-network setup, so
// both are kept: peers on the LAN take the short path, everybody else comes in
// over the public address. Dropping the private one would push LAN peers out to
// the public address and back.
func TestPublicAndPrivateAdvertisedAddressesAreBothKept(t *testing.T) {
	t.Setenv("ICE_ADVERTISE_IP", "203.0.113.10,192.168.50.147")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"203.0.113.10", "192.168.50.147"}
	for i, w := range want {
		if cfg.ICEAdvertiseIPs[i] != w {
			t.Fatalf("advertised[%d] = %q, want %q", i, cfg.ICEAdvertiseIPs[i], w)
		}
	}
}

// Defaulting this off meant every deployment ran with the legacy join path open
// until somebody flipped a flag they had no reason to know about. GRYT-772.
func TestClientTokensAreRequiredUnlessSaidOtherwise(t *testing.T) {
	t.Setenv("SFU_REQUIRE_CLIENT_TOKEN", "")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.RequireClientToken {
		t.Fatal("RequireClientToken = false, want true when the variable is unset")
	}
}

// The escape hatch a staged upgrade needs, and nothing more.
func TestTheLegacyPathCanBeReopenedDeliberately(t *testing.T) {
	t.Setenv("SFU_REQUIRE_CLIENT_TOKEN", "false")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RequireClientToken {
		t.Fatal("RequireClientToken = true, want false when explicitly disabled")
	}
}

// Rubbish must not read as "false" and silently reopen the old path.
func TestAnUnparseableValueStillRequiresTokens(t *testing.T) {
	t.Setenv("SFU_REQUIRE_CLIENT_TOKEN", "yes-please")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.RequireClientToken {
		t.Fatal("RequireClientToken = false, want true: an unparseable value must fail closed")
	}
}
