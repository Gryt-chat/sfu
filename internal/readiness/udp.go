package readiness

import (
	"crypto/rand"
	"log"
	"net"
	"strings"
	"sync/atomic"
	"time"
)

var udpVerified atomic.Bool

// IsUDPReady reports whether the outbound UDP connectivity check has passed.
func IsUDPReady() bool { return udpVerified.Load() }

// VerifyUDP sends STUN Binding Requests to the configured STUN servers in a
// background goroutine. The health endpoint should gate on IsUDPReady() so
// Docker does not mark the SFU as healthy before the container's networking
// stack is fully operational.
//
// This primarily mitigates a race condition on Docker Desktop (Windows/macOS)
// where UDP port-forwarding rules can lag behind container process startup,
// causing ICE failures on the very first docker compose up.
//
// When STUN is disabled or no servers are configured the flag is set
// immediately.
func VerifyUDP(stunServers []string, disableSTUN bool) {
	if disableSTUN || len(stunServers) == 0 {
		log.Printf("🧊 UDP readiness: STUN disabled or no servers configured — ready immediately")
		udpVerified.Store(true)
		return
	}

	go verifyLoop(stunServers)
}

func verifyLoop(stunServers []string) {
	const maxAttempts = 8
	delay := 1 * time.Second

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		for _, raw := range stunServers {
			addr := strings.TrimPrefix(raw, "stun:")
			if sendSTUNBinding(addr) {
				log.Printf("✅ UDP readiness verified via STUN (%s) on attempt %d", raw, attempt)
				udpVerified.Store(true)
				return
			}
		}

		if attempt < maxAttempts {
			log.Printf("⚠️  UDP readiness: STUN probe failed (attempt %d/%d), retrying in %v", attempt, maxAttempts, delay)
			time.Sleep(delay)
			if delay < 5*time.Second {
				delay *= 2
			}
		}
	}

	log.Printf("⚠️  UDP readiness: could not verify after %d attempts — proceeding anyway", maxAttempts)
	udpVerified.Store(true)
}

// sendSTUNBinding sends a minimal RFC 5389 Binding Request and checks for a
// Binding Success Response. Uses raw UDP so there is no extra dependency.
func sendSTUNBinding(serverAddr string) bool {
	conn, err := net.DialTimeout("udp", serverAddr, 3*time.Second)
	if err != nil {
		return false
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		return false
	}

	// STUN Binding Request (20 bytes):
	//   Type  = 0x0001  (Binding Request)
	//   Len   = 0x0000  (no attributes)
	//   Cookie = 0x2112A442
	//   TxID  = 12 random bytes
	var req [20]byte
	req[0], req[1] = 0x00, 0x01
	req[4], req[5], req[6], req[7] = 0x21, 0x12, 0xA4, 0x42
	if _, err := rand.Read(req[8:20]); err != nil {
		return false
	}

	if _, err := conn.Write(req[:]); err != nil {
		return false
	}

	buf := make([]byte, 256)
	n, err := conn.Read(buf)
	if err != nil || n < 20 {
		return false
	}

	// Binding Success Response type = 0x0101
	return buf[0] == 0x01 && buf[1] == 0x01
}
