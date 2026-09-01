package websocket

import (
	"testing"

	"github.com/pion/webrtc/v4"

	peerManager "sfu-v2/internal/webrtc"
)

// isMicrophone decides whether a `speak` denial applies to an arriving track.
// Getting it wrong in one direction lets a denied person talk; in the other it
// silently kills somebody's camera or their screen share's audio, which is a
// permission they were never denied.
//
// The peer connection is the real one the SFU builds, not a hand-made stand-in,
// because the thing under test is the order CreatePeerConnection adds
// transceivers in — a stand-in that added its own would be testing the test.
func transceiverKinds(t *testing.T, pc *webrtc.PeerConnection) []webrtc.RTPCodecType {
	t.Helper()
	var kinds []webrtc.RTPCodecType
	for _, tr := range pc.GetTransceivers() {
		kinds = append(kinds, tr.Kind())
	}
	return kinds
}

func newSFUPeer(t *testing.T) *webrtc.PeerConnection {
	t.Helper()
	pc, err := peerManager.CreatePeerConnection(webrtc.NewAPI(), webrtc.Configuration{})
	if err != nil {
		t.Fatalf("create peer connection: %v", err)
	}
	t.Cleanup(func() { _ = pc.Close() })
	return pc
}

// The arrangement the gate depends on. If this fails, isMicrophone is matching
// the wrong slot and the failure would otherwise show up as "screen share has
// no sound" long after the change that caused it.
func TestTheSFUOffersMicrophoneFirst(t *testing.T) {
	kinds := transceiverKinds(t, newSFUPeer(t))

	want := []webrtc.RTPCodecType{
		webrtc.RTPCodecTypeAudio, // microphone
		webrtc.RTPCodecTypeVideo, // camera
		webrtc.RTPCodecTypeVideo, // screen
		webrtc.RTPCodecTypeAudio, // screen audio
	}
	if len(kinds) != len(want) {
		t.Fatalf("transceivers = %v, want %d of them", kinds, len(want))
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("transceiver %d is %v, want %v (full order: %v)", i, kinds[i], want[i], kinds)
		}
	}
}

func TestOnlyTheFirstAudioTransceiverIsTheMicrophone(t *testing.T) {
	pc := newSFUPeer(t)
	transceivers := pc.GetTransceivers()

	if !isMicrophone(pc, transceivers[0].Receiver()) {
		t.Fatal("the first transceiver is the microphone and was not recognised as one")
	}

	// The other three are a camera, a screen and that screen's audio. None of
	// them is gated by `speak` — screen audio is `share_screen` on the server,
	// and it arrives here as audio too, which is exactly the confusion this
	// guards against.
	for i, tr := range transceivers[1:] {
		if isMicrophone(pc, tr.Receiver()) {
			t.Fatalf("transceiver %d (%v) was treated as the microphone", i+1, tr.Kind())
		}
	}
}

// A receiver belonging to some other peer connection is not this one's
// microphone. Fails closed the safe way: unrecognised means "not the mic", so
// the track is forwarded rather than dropped.
func TestAnUnknownReceiverIsNotTheMicrophone(t *testing.T) {
	if isMicrophone(newSFUPeer(t), newSFUPeer(t).GetTransceivers()[0].Receiver()) {
		t.Fatal("a receiver from another peer connection was treated as the microphone")
	}
	if isMicrophone(newSFUPeer(t), nil) {
		t.Fatal("a nil receiver was treated as the microphone")
	}
}
