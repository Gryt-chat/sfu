package websocket

import (
	"testing"

	"github.com/pion/webrtc/v4"

	"sfu-v2/internal/auth"
)

func TestScreenSlotsAddAPairPerWindowAndStopAtTheCeiling(t *testing.T) {
	pc := newSFUPeer(t)
	base := len(pc.GetTransceivers())

	if err := ensureScreenSlots(pc, 3); err != nil {
		t.Fatal(err)
	}
	if got := len(pc.GetTransceivers()) - base; got != 4 {
		t.Fatalf("three windows added %d transceivers, want 4 (two more pairs)", got)
	}
	// Asking again for the same number, or fewer, adds nothing.
	if err := ensureScreenSlots(pc, 2); err != nil {
		t.Fatal(err)
	}
	if got := len(pc.GetTransceivers()) - base; got != 4 {
		t.Fatalf("a smaller request changed the count to %d", got)
	}
	if err := ensureScreenSlots(pc, 1000); err != nil {
		t.Fatal(err)
	}
	if got := len(pc.GetTransceivers()) - base; got != (maxScreenShares-1)*2 {
		t.Fatalf("a huge request added %d, want the ceiling's %d", got, (maxScreenShares-1)*2)
	}
}

// The added pairs answer to share_screen like the first, never to share_video.
func TestAddedScreenSlotsAnswerToShareScreen(t *testing.T) {
	pc := newSFUPeer(t)
	if err := ensureScreenSlots(pc, 2); err != nil {
		t.Fatal(err)
	}
	tr := pc.GetTransceivers()
	video, audio := tr[len(tr)-2], tr[len(tr)-1]
	if video.Kind() != webrtc.RTPCodecTypeVideo || audio.Kind() != webrtc.RTPCodecTypeAudio {
		t.Fatalf("the pair is %s, %s", video.Kind(), audio.Kind())
	}
	screenOnly := claimsOf(auth.CapSpeak, auth.CapVideoChecked, auth.CapShareScreen)
	cameraOnly := claimsOf(auth.CapSpeak, auth.CapVideoChecked, auth.CapShareVideo)
	if got := refusedBy(screenOnly, pc, video.Receiver(), video.Kind()); got != "" {
		t.Errorf("screen-only token refused the second window: %q", got)
	}
	if got := refusedBy(cameraOnly, pc, video.Receiver(), video.Kind()); got != auth.CapShareScreen {
		t.Errorf("camera-only token: got %q, want share_screen", got)
	}
	if got := refusedBy(cameraOnly, pc, audio.Receiver(), audio.Kind()); got != auth.CapShareScreen {
		t.Errorf("camera-only token on the second window's sound: got %q, want share_screen", got)
	}

	forgetScreenSlots(pc)
	if _, ok := extraScreenReceivers.Load(video.Receiver()); ok {
		t.Error("a closed peer's slots stayed in the registry")
	}
}
