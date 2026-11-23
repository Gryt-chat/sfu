package signaling

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pion/webrtc/v4"

	"sfu-v2/internal/room"
	"sfu-v2/internal/track"
	peerManager "sfu-v2/internal/webrtc"
	"sfu-v2/pkg/types"
)

type sentMessages struct {
	mu   sync.Mutex
	sent []*types.WebSocketMessage
}

func (s *sentMessages) WriteJSON(v interface{}) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if m, ok := v.(*types.WebSocketMessage); ok {
		s.sent = append(s.sent, m)
	}
	return nil
}

// GRYT-2244: a peer that never answered an offer was skipped as "not stable" for good.
func TestUnansweredOfferIsSentAgain(t *testing.T) {
	c := NewCoordinator(track.NewManager(false), peerManager.NewManager(false), room.NewManager(false), false)
	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()
	if _, err := pc.AddTransceiverFromKind(webrtc.RTPCodecTypeAudio); err != nil {
		t.Fatal(err)
	}
	conn := &sentMessages{}

	if err := c.processPeerConnection("viewer", pc, conn, nil, "room"); err != nil {
		t.Fatalf("first offer: %v", err)
	}
	if pc.SignalingState() != webrtc.SignalingStateHaveLocalOffer || len(conn.sent) != 1 {
		t.Fatalf("want one offer out and waiting, got state %v and %d messages", pc.SignalingState(), len(conn.sent))
	}

	// Fresh: still waiting for its answer, so it's left alone.
	if err := c.processPeerConnection("viewer", pc, conn, nil, "room"); err == nil {
		t.Fatal("a fresh unanswered offer should still count as not stable")
	}

	// Stale: the same offer goes out again, still waiting for its answer.
	c.offersMu.Lock()
	c.offerSentAt["viewer"] = time.Now().Add(-staleOfferAfter - time.Second)
	c.offersMu.Unlock()
	if err := c.processPeerConnection("viewer", pc, conn, nil, "room"); err != nil {
		t.Fatalf("stale offer: %v", err)
	}
	// The same offer, though it may carry ICE candidates gathered since, so the text can differ.
	if len(conn.sent) != 2 || conn.sent[1].Event != types.EventOffer || !strings.Contains(conn.sent[1].Data, `"type":"offer"`) {
		t.Fatalf("want the offer sent a second time, got %d messages", len(conn.sent))
	}
	if pc.SignalingState() != webrtc.SignalingStateHaveLocalOffer {
		t.Fatalf("want it still waiting for its answer, got %v", pc.SignalingState())
	}
	if c.offerStale("viewer") {
		t.Fatal("sending it again should have reset the clock")
	}
}
