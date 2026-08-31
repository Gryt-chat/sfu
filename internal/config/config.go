package config

import (
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/pion/webrtc/v4"
)

const (
	// DefaultICEUDPMuxPort is where media goes when nothing says otherwise.
	// 3478 needs no privileged bind and is already open on hosts that run STUN.
	DefaultICEUDPMuxPort = 3478
	// DefaultMaxPeers is a guardrail, not a property of the transport.
	DefaultMaxPeers = 200

	// DefaultPingInterval is how often the SFU pokes a quiet connection to
	// check somebody is still on the other end. It doubles as the only traffic
	// that travels server to client during a call, which is what keeps a NAT
	// or firewall mapping from being reaped as idle.
	DefaultPingInterval = 30 * time.Second

	// DefaultPongTimeout is how long a peer may say nothing at all before the
	// SFU gives up on it. Three ping intervals, so two pings have to go
	// unanswered in a row before anybody is hung up on — a single lost pong,
	// or one arriving late behind a stalled read, is not enough.
	DefaultPongTimeout = 90 * time.Second

	// DefaultCallAloneTimeout is how long one person may be the only one in a
	// call before the SFU ends it. Long enough that stepping away to let
	// somebody back in is not punished, short enough to be worth doing.
	//
	// Calls only. A voice channel is a place and sitting in one alone is
	// ordinary; internal/room/calls.go is where the two are told apart.
	DefaultCallAloneTimeout = 2 * time.Minute

	// DefaultCallSweepInterval is how often that is checked. It bounds the
	// error on the timeout above — a call ends between the timeout and one
	// interval after it — and it is the whole cost of the feature when nobody
	// is in a call at all, which is a map iteration.
	DefaultCallSweepInterval = 15 * time.Second
)

// Config holds the application configuration
type Config struct {
	Port        string
	STUNServers []string
	ICEServers  []webrtc.ICEServer
	Debug       bool
	VerboseLog  bool

	// WebRTC / ICE networking. Every participant's media shares one UDP port,
	// so this is the only port that has to be open for voice to work.
	ICEUDPMuxPort   int
	ICEAdvertiseIPs []string
	DisableSTUN     bool

	// Capacity guardrail. Nothing to do with ports any more: one muxed port
	// carries far more peers than a machine has CPU and upload for.
	MaxPeers int

	// Liveness. The SFU pings each WebSocket every PingInterval and gives up on
	// one that has said nothing for PongTimeout. internal/websocket/keepalive.go
	// has the reasoning; a PingInterval of zero turns both off.
	PingInterval time.Duration
	PongTimeout  time.Duration

	// How long somebody may be alone in a call before it ends. Zero turns it
	// off and leaves the room up until the peer goes on its own.
	CallAloneTimeout time.Duration
}

// Load reads configuration from environment variables
func Load() (*Config, error) {
	if err := godotenv.Load("config.env"); err != nil {
		if err2 := godotenv.Load(); err2 != nil {
			log.Printf("Warning: No config.env or .env file found: %v", err2)
		}
	}

	port := os.Getenv("SFU_PORT")
	if port == "" {
		port = os.Getenv("PORT")
	}
	if port == "" {
		port = "5005"
	}

	stunServers := strings.Split(os.Getenv("STUN_SERVERS"), ",")
	if len(stunServers) == 1 && stunServers[0] == "" {
		// Default STUN servers if none provided
		stunServers = []string{"stun:stun.l.google.com:19302"}
	}

	// One UDP port for all media. Unset it is 3478: the IANA STUN port,
	// unprivileged, and the UDP port a locked-down network is most likely to
	// have opened already, since Teams requires outbound 3478-3481.
	//
	// 443 is the tempting alternative and usually the wrong one. Firewall
	// vendors recommend blocking UDP 443 precisely because QUIC there cannot be
	// TLS-inspected, so on the networks you would choose it for it is the port
	// most likely to be shut on purpose.
	iceUDPMuxPort, _ := strconv.Atoi(os.Getenv("ICE_UDP_MUX_PORT"))
	if iceUDPMuxPort <= 0 || iceUDPMuxPort > 65535 {
		iceUDPMuxPort = DefaultICEUDPMuxPort
	}

	// Parsed before the STUN decision below, because whether an operator has
	// forced an address is what decides the default for DISABLE_STUN.
	//
	// Entries are validated rather than passed through. A value that is not an
	// IP at all can never become a candidate, so it is dropped and said out
	// loud — the failure is otherwise invisible, because the SFU keeps working
	// on whatever else is in the list and nobody finds out it is wrong.
	var iceAdvertiseIPs []string
	hasRoutableAdvertiseIP := false
	if raw := os.Getenv("ICE_ADVERTISE_IP"); raw != "" {
		for _, entry := range strings.Split(raw, ",") {
			t := strings.TrimSpace(entry)
			if t == "" {
				continue
			}
			parsed := net.ParseIP(t)
			if parsed == nil {
				log.Printf("Warning: ICE_ADVERTISE_IP entry %q is not an IP address; ignoring it", t)
				continue
			}
			iceAdvertiseIPs = append(iceAdvertiseIPs, t)
			if !(parsed.IsLoopback() || parsed.IsPrivate() || parsed.IsLinkLocalUnicast()) {
				hasRoutableAdvertiseIP = true
			}
		}
	}

	// A private address alongside a public one is the ordinary multi-network
	// setup: peers on the LAN take the short path and everybody else comes in
	// over the public address. Warning about that would be noise. A list with
	// no routable address in it at all is the real problem, because the SFU
	// then has nothing to offer anybody outside the network, and it fails the
	// same quiet way the rest of this guards against.
	if len(iceAdvertiseIPs) > 0 && !hasRoutableAdvertiseIP {
		log.Printf("Warning: no ICE_ADVERTISE_IP entry is routable from outside this network (%s); peers elsewhere will not be able to connect",
			strings.Join(iceAdvertiseIPs, ", "))
	}

	// STUN discovers the address the internet sees this host as. That is the
	// right default when nothing else knows it, and the wrong one once an
	// operator has said what to advertise: discovery then adds a candidate
	// nobody chose, carrying whatever address the current egress path happens
	// to have. If that path changes — a tunnel goes down and traffic falls back
	// to the ordinary route — the SFU quietly starts handing out the new
	// address instead of failing, and calls keep working, so nothing surfaces
	// it. GRYT-768.
	//
	// So forcing an address turns discovery off unless the operator says
	// otherwise. Setting DISABLE_STUN explicitly always wins, in either
	// direction.
	disableSTUN := len(iceAdvertiseIPs) > 0
	if raw := strings.TrimSpace(os.Getenv("DISABLE_STUN")); raw != "" {
		parsed, err := strconv.ParseBool(raw)
		if err != nil {
			log.Printf("Warning: DISABLE_STUN=%q is not a boolean; using %t", raw, disableSTUN)
		} else {
			disableSTUN = parsed
		}
	} else if disableSTUN {
		log.Printf("ICE_ADVERTISE_IP is set, so STUN discovery is off; set DISABLE_STUN=false to gather server-reflexive candidates as well")
	}

	iceServers := []webrtc.ICEServer{}
	if !disableSTUN {
		iceServers = []webrtc.ICEServer{
			{
				URLs: stunServers,
			},
		}
	}

	// A muxed port accepts many peers, so this is a guardrail rather than a
	// limit the transport imposes. What runs out first is CPU and upload.
	maxPeers, _ := strconv.Atoi(os.Getenv("MAX_PEERS"))
	if maxPeers <= 0 {
		maxPeers = DefaultMaxPeers
	}

	// Both in whole seconds. SFU_PING_INTERVAL=0 switches liveness checking off
	// entirely, read deadline included — the escape hatch if this ever starts
	// hanging up on people who were fine.
	pingInterval := durationSecondsFromEnv("SFU_PING_INTERVAL", DefaultPingInterval)
	pongTimeout := durationSecondsFromEnv("SFU_PONG_TIMEOUT", DefaultPongTimeout)

	// Also whole seconds, and zero is off — a server owner who would rather a
	// call stayed up until somebody closed it says so without a rebuild.
	callAloneTimeout := durationSecondsFromEnv("SFU_CALL_ALONE_TIMEOUT", DefaultCallAloneTimeout)

	// A timeout shorter than two ping intervals disconnects healthy peers: the
	// deadline fires before a second ping has even gone out, so one lost pong
	// is fatal. Raise it rather than refusing to start — an SFU that will not
	// boot because somebody typed a small number is worse than one that says
	// what it did instead.
	if pingInterval > 0 && pongTimeout < 2*pingInterval {
		log.Printf("Warning: SFU_PONG_TIMEOUT (%s) is under two ping intervals (%s); using %s, the least that survives a single lost pong",
			pongTimeout, pingInterval, 2*pingInterval)
		pongTimeout = 2 * pingInterval
	}

	// Debug configuration
	debug, _ := strconv.ParseBool(os.Getenv("DEBUG"))
	verboseLog, _ := strconv.ParseBool(os.Getenv("VERBOSE_LOG"))

	// Default to debug mode if not specified
	if os.Getenv("DEBUG") == "" {
		debug = true
	}

	return &Config{
		Port:            port,
		STUNServers:     stunServers,
		ICEServers:      iceServers,
		Debug:           debug,
		VerboseLog:      verboseLog,
		ICEUDPMuxPort:   iceUDPMuxPort,
		ICEAdvertiseIPs: iceAdvertiseIPs,
		DisableSTUN:     disableSTUN,
		MaxPeers:        maxPeers,
		PingInterval:    pingInterval,
		PongTimeout:     pongTimeout,

		CallAloneTimeout: callAloneTimeout,
	}, nil
}

// durationSecondsFromEnv reads a whole number of seconds, and keeps the default
// when the variable is unset or unreadable.
//
// Zero is a value, not an absence — it is how liveness checking is turned off —
// so an unset variable and an explicit 0 have to be told apart, which is why
// this does not go through strconv.Atoi's zero-on-error.
func durationSecondsFromEnv(name string, fallback time.Duration) time.Duration {
	raw := os.Getenv(name)
	if raw == "" {
		return fallback
	}

	seconds, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || seconds < 0 {
		log.Printf("Warning: ignoring %s=%q — want a whole number of seconds; using %s", name, raw, fallback)
		return fallback
	}

	return time.Duration(seconds) * time.Second
}
