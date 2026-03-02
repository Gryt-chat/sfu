package svc

import (
	"log"
	"sync"
	"time"

	"github.com/pion/rtcp"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
)

// ReceiverState tracks a single downstream receiver's track and layer subscription.
type ReceiverState struct {
	Track           *webrtc.TrackLocalStaticRTP
	MaxTemporalLayer int // packets with temporal_id > this are dropped
	active           bool
}

// LayerForwarder reads RTP packets from a remote track, parses the Dependency
// Descriptor header extension (if present), and selectively forwards packets
// to per-receiver local tracks based on each receiver's subscribed temporal layer.
//
// For streams without SVC (no DD extension), it falls back to blind relay.
type LayerForwarder struct {
	mu sync.RWMutex

	remoteTrack *webrtc.TrackRemote
	senderPC    *webrtc.PeerConnection
	remoteSSRC  uint32

	codec    webrtc.RTPCodecCapability
	trackID  string
	streamID string

	ddParser *DDParser
	ddExtID  uint8 // negotiated RTP extension ID for DD; 0 = not yet detected
	hasSVC   bool  // true once a DD extension has been seen

	receivers map[string]*ReceiverState

	stopped chan struct{}
	debug   bool
}

// NewLayerForwarder creates a forwarder for the given remote track and starts
// the forwarding goroutine. The goroutine runs until the remote track ends or
// Stop is called.
func NewLayerForwarder(remote *webrtc.TrackRemote, senderPC *webrtc.PeerConnection, debug bool) *LayerForwarder {
	lf := &LayerForwarder{
		remoteTrack: remote,
		senderPC:    senderPC,
		remoteSSRC:  uint32(remote.SSRC()),
		codec:       remote.Codec().RTPCodecCapability,
		trackID:     remote.ID(),
		streamID:    remote.StreamID(),
		ddParser:    NewDDParser(),
		receivers:   make(map[string]*ReceiverState),
		stopped:     make(chan struct{}),
		debug:       debug,
	}
	go lf.run()
	return lf
}

// GetSenderPC returns the original sender's peer connection.
func (lf *LayerForwarder) GetSenderPC() *webrtc.PeerConnection {
	return lf.senderPC
}

// GetRemoteSSRC returns the SSRC of the remote track.
func (lf *LayerForwarder) GetRemoteSSRC() uint32 {
	return lf.remoteSSRC
}

// AddReceiver creates a per-receiver TrackLocalStaticRTP and registers it.
// maxTemporalLayer = -1 means forward all layers (no filtering).
func (lf *LayerForwarder) AddReceiver(receiverID string, maxTemporalLayer int) *webrtc.TrackLocalStaticRTP {
	lf.mu.Lock()
	defer lf.mu.Unlock()

	if r, ok := lf.receivers[receiverID]; ok && r.Track != nil {
		r.MaxTemporalLayer = maxTemporalLayer
		r.active = true
		return r.Track
	}

	track, err := webrtc.NewTrackLocalStaticRTP(lf.codec, lf.trackID, lf.streamID)
	if err != nil {
		lf.debugLog("failed to create per-receiver track for %s: %v", receiverID, err)
		return nil
	}

	lf.receivers[receiverID] = &ReceiverState{
		Track:            track,
		MaxTemporalLayer: maxTemporalLayer,
		active:           true,
	}

	lf.debugLog("added receiver %s (maxTemporal=%d, total=%d)", receiverID, maxTemporalLayer, len(lf.receivers))
	return track
}

// RemoveReceiver removes a receiver from the fanout.
func (lf *LayerForwarder) RemoveReceiver(receiverID string) {
	lf.mu.Lock()
	defer lf.mu.Unlock()
	delete(lf.receivers, receiverID)
	lf.debugLog("removed receiver %s (remaining=%d)", receiverID, len(lf.receivers))
}

// SetMaxTemporalLayer updates the temporal layer cap for a receiver.
func (lf *LayerForwarder) SetMaxTemporalLayer(receiverID string, layer int) {
	lf.mu.Lock()
	defer lf.mu.Unlock()
	if r, ok := lf.receivers[receiverID]; ok {
		r.MaxTemporalLayer = layer
		lf.debugLog("receiver %s maxTemporal → %d", receiverID, layer)
	}
}

// GetReceiverTrack returns the local track for a receiver, or nil if not found.
func (lf *LayerForwarder) GetReceiverTrack(receiverID string) *webrtc.TrackLocalStaticRTP {
	lf.mu.RLock()
	defer lf.mu.RUnlock()
	if r, ok := lf.receivers[receiverID]; ok {
		return r.Track
	}
	return nil
}

// HasReceiver returns true if the given receiver ID is registered.
func (lf *LayerForwarder) HasReceiver(receiverID string) bool {
	lf.mu.RLock()
	defer lf.mu.RUnlock()
	_, ok := lf.receivers[receiverID]
	return ok
}

// TrackID returns the logical track ID (same as the remote track).
func (lf *LayerForwarder) TrackID() string {
	return lf.trackID
}

// StreamID returns the stream ID (same as the remote track).
func (lf *LayerForwarder) StreamID() string {
	return lf.streamID
}

// Kind returns the track kind (audio or video).
func (lf *LayerForwarder) Kind() webrtc.RTPCodecType {
	return lf.remoteTrack.Kind()
}

// Stop terminates the forwarding goroutine.
func (lf *LayerForwarder) Stop() {
	select {
	case <-lf.stopped:
	default:
		close(lf.stopped)
	}
}

// run is the main forwarding loop. It reads raw RTP bytes from the remote track,
// parses the RTP header to check for a DD extension, and fans out to receivers.
func (lf *LayerForwarder) run() {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[SVC-FORWARDER] panic in forwarding loop for track %s: %v", lf.trackID, r)
		}
	}()

	// Periodic PLI for video tracks so late-joining receivers get keyframes.
	if lf.remoteTrack.Kind() == webrtc.RTPCodecTypeVideo {
		go lf.periodicPLI()
	}

	buf := make([]byte, 1500)
	var header rtp.Header

	for {
		select {
		case <-lf.stopped:
			return
		default:
		}

		n, _, readErr := lf.remoteTrack.Read(buf)
		if readErr != nil {
			lf.debugLog("track read ended: %v", readErr)
			return
		}

		raw := buf[:n]

		// Parse the RTP header to extract DD extension.
		headerLen, unmarshalErr := header.Unmarshal(raw)
		_ = headerLen
		temporalID := -1

		if unmarshalErr == nil {
			temporalID = lf.extractTemporalID(&header)
		}

		lf.mu.RLock()
		for _, rs := range lf.receivers {
			if !rs.active || rs.Track == nil {
				continue
			}

			if lf.hasSVC && temporalID >= 0 && rs.MaxTemporalLayer >= 0 && temporalID > rs.MaxTemporalLayer {
				continue
			}

			if _, writeErr := rs.Track.Write(raw); writeErr != nil {
				continue
			}
		}
		lf.mu.RUnlock()
	}
}

// extractTemporalID tries to find and parse the DD extension from the RTP header.
// Returns the temporal layer ID, or -1 if no DD extension is present.
func (lf *LayerForwarder) extractTemporalID(h *rtp.Header) int {
	// If we already know the extension ID, use it directly.
	if lf.ddExtID > 0 {
		ext := h.GetExtension(lf.ddExtID)
		if ext == nil {
			return -1
		}
		fi, err := lf.ddParser.Parse(ext)
		if err != nil {
			return -1
		}
		if !lf.hasSVC && fi.TemporalID >= 0 {
			lf.hasSVC = true
			lf.debugLog("SVC detected (temporal_id=%d)", fi.TemporalID)
		}
		return fi.TemporalID
	}

	// Auto-detect: scan all extensions for one that looks like a DD header.
	for _, id := range h.GetExtensionIDs() {
		ext := h.GetExtension(id)
		if len(ext) < 3 {
			continue
		}
		fi, err := lf.ddParser.Parse(ext)
		if err != nil {
			continue
		}
		if fi.TemporalID >= 0 {
			lf.ddExtID = id
			lf.hasSVC = true
			lf.debugLog("auto-detected DD extension ID=%d (temporal=%d)", id, fi.TemporalID)
			return fi.TemporalID
		}
		// Even if temporalID is -1 (no template yet), the parse succeeded which
		// means the extension is likely DD. Cache the ID for future packets.
		lf.ddExtID = id
		lf.debugLog("auto-detected DD extension ID=%d (template pending)", id)
		return -1
	}

	return -1
}

// periodicPLI sends PLI every 5 seconds to the sender for video tracks.
func (lf *LayerForwarder) periodicPLI() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-lf.stopped:
			return
		case <-ticker.C:
			if lf.senderPC.ConnectionState() == webrtc.PeerConnectionStateClosed {
				return
			}
			if writeErr := lf.senderPC.WriteRTCP([]rtcp.Packet{
				&rtcp.PictureLossIndication{MediaSSRC: lf.remoteSSRC},
			}); writeErr != nil {
				return
			}
		}
	}
}

func (lf *LayerForwarder) debugLog(format string, args ...interface{}) {
	if lf.debug {
		log.Printf("[SVC-FORWARDER] "+format, args...)
	}
}
