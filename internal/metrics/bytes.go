package metrics

import (
	"sync"
	"sync/atomic"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Media bytes by the Gryt server a room belongs to, for per-server bandwidth (GRYT-2169).
// RTP and datagram payloads as sent; UDP, IP and SRTP overhead are not counted.
var (
	ingressBytes = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "gryt_sfu_ingress_bytes_total",
		Help: "Media bytes received, by the server the room belongs to",
	}, []string{"server_id"})
	egressBytes = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "gryt_sfu_egress_bytes_total",
		Help: "Media bytes sent, by the server the room belongs to",
	}, []string{"server_id"})
)

// RoomBytes counts one room's media under its server.
type RoomBytes struct{ In, Out prometheus.Counter }

var (
	resolver atomic.Pointer[func(roomID string) (string, bool)]
	byRoom   sync.Map // roomID -> *RoomBytes, once its server is known
)

// SetServerResolver says how to find a room's server. A room keeps its server for life.
func SetServerResolver(f func(roomID string) (string, bool)) { resolver.Store(&f) }

// ForRoom returns the counters for a room, resolving its server once and caching it.
func ForRoom(roomID string) *RoomBytes {
	if c, ok := byRoom.Load(roomID); ok {
		return c.(*RoomBytes)
	}
	server := "unknown"
	known := false
	if f := resolver.Load(); f != nil {
		server, known = (*f)(roomID)
		if !known {
			server = "unknown"
		}
	}
	c := &RoomBytes{In: ingressBytes.WithLabelValues(server), Out: egressBytes.WithLabelValues(server)}
	if known {
		byRoom.Store(roomID, c)
	}
	return c
}
