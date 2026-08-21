package config

import (
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
	"github.com/pion/webrtc/v4"
)

const (
	// DefaultICEUDPMuxPort is where media goes when nothing says otherwise.
	// 3478 needs no privileged bind and is already open on hosts that run STUN.
	DefaultICEUDPMuxPort = 3478
	// DefaultMaxPeers is a guardrail, not a property of the transport.
	DefaultMaxPeers = 200
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

	disableSTUN, _ := strconv.ParseBool(os.Getenv("DISABLE_STUN"))

	stunServers := strings.Split(os.Getenv("STUN_SERVERS"), ",")
	if len(stunServers) == 1 && stunServers[0] == "" {
		// Default STUN servers if none provided
		stunServers = []string{"stun:stun.l.google.com:19302"}
	}

	iceServers := []webrtc.ICEServer{}
	if !disableSTUN {
		iceServers = []webrtc.ICEServer{
			{
				URLs: stunServers,
			},
		}
	}

	// One UDP port for all media. Unset it is 3478, which is unprivileged and
	// is the port people already open for STUN. Deployments that want media on
	// 443, where almost nothing blocks UDP, set it themselves.
	iceUDPMuxPort, _ := strconv.Atoi(os.Getenv("ICE_UDP_MUX_PORT"))
	if iceUDPMuxPort <= 0 || iceUDPMuxPort > 65535 {
		iceUDPMuxPort = DefaultICEUDPMuxPort
	}
	var iceAdvertiseIPs []string
	if raw := os.Getenv("ICE_ADVERTISE_IP"); raw != "" {
		for _, ip := range strings.Split(raw, ",") {
			if t := strings.TrimSpace(ip); t != "" {
				iceAdvertiseIPs = append(iceAdvertiseIPs, t)
			}
		}
	}

	// A muxed port accepts many peers, so this is a guardrail rather than a
	// limit the transport imposes. What runs out first is CPU and upload.
	maxPeers, _ := strconv.Atoi(os.Getenv("MAX_PEERS"))
	if maxPeers <= 0 {
		maxPeers = DefaultMaxPeers
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
	}, nil
}
