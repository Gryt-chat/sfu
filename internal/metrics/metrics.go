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
)
