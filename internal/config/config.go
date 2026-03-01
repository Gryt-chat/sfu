package config

import (
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
	"github.com/pion/webrtc/v4"
)

// Config holds the application configuration
type Config struct {
	Port        string
	STUNServers []string
	ICEServers  []webrtc.ICEServer
	Debug       bool
	VerboseLog  bool

	// WebRTC / ICE networking
	ICEUDPPortMin   int
	ICEUDPPortMax   int
	ICEUDPMuxPort   int
	ICEAdvertiseIPs []string
	DisableSTUN     bool

	// Capacity guardrail (primarily to match UDP port range)
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

	iceUDPPortMin, _ := strconv.Atoi(os.Getenv("ICE_UDP_PORT_MIN"))
	iceUDPPortMax, _ := strconv.Atoi(os.Getenv("ICE_UDP_PORT_MAX"))
	iceUDPMuxPort, _ := strconv.Atoi(os.Getenv("ICE_UDP_MUX_PORT"))
	var iceAdvertiseIPs []string
	if raw := os.Getenv("ICE_ADVERTISE_IP"); raw != "" {
		for _, ip := range strings.Split(raw, ",") {
			if t := strings.TrimSpace(ip); t != "" {
				iceAdvertiseIPs = append(iceAdvertiseIPs, t)
			}
		}
	}

	maxPeers, _ := strconv.Atoi(os.Getenv("MAX_PEERS"))
	if maxPeers <= 0 && iceUDPMuxPort > 0 {
		// With ICE UDP mux enabled, we can accept many peers on a single UDP port.
		// Keep it bounded by MAX_PEERS if provided; otherwise use a conservative default.
		maxPeers = 200
	} else if maxPeers <= 0 && iceUDPPortMin > 0 && iceUDPPortMax >= iceUDPPortMin {
		// Default capacity to the size of the pinned UDP port range.
		maxPeers = (iceUDPPortMax - iceUDPPortMin + 1)
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
		ICEUDPPortMin:   iceUDPPortMin,
		ICEUDPPortMax:   iceUDPPortMax,
		ICEUDPMuxPort:   iceUDPMuxPort,
		ICEAdvertiseIPs: iceAdvertiseIPs,
		DisableSTUN:     disableSTUN,
		MaxPeers:        maxPeers,
	}, nil
}
