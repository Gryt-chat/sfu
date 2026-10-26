package websocket

import (
	"sync"

	"github.com/pion/webrtc/v4"
)

// maxScreenShares bounds what one client can make the SFU allocate. Not a product limit: a
// share costs the sender an encoder and upload long before it reaches this.
const maxScreenShares = 8

// The pairs added past the join's one, by receiver, so slotOf can still name them as screen.
var extraScreenReceivers sync.Map // *webrtc.RTPReceiver -> webrtc.RTPCodecType

// screensRequest is renegotiate's optional data: how many windows the client means to share.
type screensRequest struct {
	Screens int `json:"screens"`
}

// ensureScreenSlots adds receive-only screen video and audio pairs until pc has want of them.
func ensureScreenSlots(pc *webrtc.PeerConnection, want int) error {
	if want > maxScreenShares {
		want = maxScreenShares
	}
	extra := 0
	for _, tr := range pc.GetTransceivers() {
		if _, ok := extraScreenReceivers.Load(tr.Receiver()); ok {
			extra++
		}
	}
	for have := 1 + extra/2; have < want; have++ {
		for _, kind := range []webrtc.RTPCodecType{webrtc.RTPCodecTypeVideo, webrtc.RTPCodecTypeAudio} {
			tr, err := pc.AddTransceiverFromKind(kind, webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionRecvonly})
			if err != nil {
				return err
			}
			extraScreenReceivers.Store(tr.Receiver(), kind)
		}
	}
	return nil
}

// forgetScreenSlots drops a closing peer's pairs from the registry.
func forgetScreenSlots(pc *webrtc.PeerConnection) {
	for _, tr := range pc.GetTransceivers() {
		extraScreenReceivers.Delete(tr.Receiver())
	}
}
