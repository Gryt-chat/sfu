package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	RoomsActive = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "gryt_sfu_rooms_active",
		Help: "Number of active rooms",
	})

	PeersActive = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "gryt_sfu_peers_active",
		Help: "Total number of connected peers across all rooms",
	})

	WebSocketConnections = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "gryt_sfu_websocket_connections_active",
		Help: "Number of active WebSocket connections",
	})

	TracksActive = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "gryt_sfu_tracks_active",
		Help: "Number of active media tracks being forwarded",
	})

	// Microphone tracks dropped because the client's token did not grant
	// `speak`. Expected to be non-zero on any server using the permission, and
	// worth an alert only if it climbs on a server that grants it to everyone —
	// which would mean tokens are being minted without the capability rather
	// than people being correctly refused.
	TracksRefused = promauto.NewCounter(prometheus.CounterOpts{
		Name: "gryt_sfu_tracks_refused_total",
		Help: "Media tracks refused because the client token did not grant the capability",
	})

	// The number worth watching after the ping went in. A handful a day is
	// peers whose network died, which is the case this is for. A step change
	// after a deploy means the deadline is too tight for somebody's network,
	// and SFU_PING_INTERVAL=0 turns it off without a release.
	PingTimeouts = promauto.NewCounter(prometheus.CounterOpts{
		Name: "gryt_sfu_websocket_ping_timeouts_total",
		Help: "WebSocket connections closed because the peer stopped answering pings",
	})
)
