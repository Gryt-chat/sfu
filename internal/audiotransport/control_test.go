package audiotransport

import (
	"bytes"
	"encoding/binary"
	"testing"
	"time"

	"golang.org/x/crypto/chacha20poly1305"
)

func keyframeRequest(sender uint32, track byte) []byte {
	b := make([]byte, controlBytes)
	b[0], b[1], b[2] = controlVersion, controlKeyframe, track
	binary.BigEndian.PutUint32(b[4:], sender)
	return b
}

func resendRequest(sender uint32, track byte, pieces ...[2]uint16) []byte {
	b := make([]byte, controlBytes+4*len(pieces))
	b[0], b[1], b[2], b[3] = controlVersion, controlResend, track, byte(len(pieces))
	binary.BigEndian.PutUint32(b[4:], sender)
	for i, p := range pieces {
		binary.BigEndian.PutUint16(b[controlBytes+4*i:], p[0])
		binary.BigEndian.PutUint16(b[controlBytes+4*i+2:], p[1])
	}
	return b
}

func pieceOfFrame(track byte, frame, piece, pieces uint16) []byte {
	b := videoPacket(piece, pieces, 20)
	b[2] = track
	binary.BigEndian.PutUint16(b[4:], frame)
	return b
}

func TestControlDatagramsAreChecked(t *testing.T) {
	for _, c := range []struct {
		name string
		b    []byte
		want bool
	}{
		{"keyframe", keyframeRequest(1, 0), true},
		{"resend one", resendRequest(1, 0, [2]uint16{7, 1}), true},
		{"resend the most", resendRequest(1, 0, make([][2]uint16, maxResend)...), true},
		{"resend too many", resendRequest(1, 0, make([][2]uint16, maxResend+1)...), false},
		{"resend none", resendRequest(1, 0), false},
		{"keyframe with pieces", append(keyframeRequest(1, 0), 0, 0, 0, 0), false},
		{"layers", layersRequest(1, 0, 1), true},
		{"layers with pieces", append(layersRequest(1, 0, 1), 0, 0, 0, 0), false},
		{"short", keyframeRequest(1, 0)[:controlBytes-1], false},
		{"a piece", videoPacket(0, 1, 10), false},
	} {
		if got := validControl(c.b); got != c.want {
			t.Errorf("%s: got %v", c.name, got)
		}
	}
	h := newHub()
	a, b := &fakeSession{}, &fakeSession{}
	ma := h.join("room", "a", a, nil)
	h.join("room", "b", b, nil)
	h.forward(ma, keyframeRequest(2, 0), time.Now())
	if len(b.got) != 0 {
		t.Fatal("the audio hub passed on a video request")
	}
}

// Many viewers asking at once reach the sender as one request per gap, from sender id 0.
func TestKeyframeRequestsReachTheSenderOncePerGap(t *testing.T) {
	h := newVideoHub()
	a, b, c := &fakeSession{}, &fakeSession{}, &fakeSession{}
	ma := h.join("room", "a", a, nil)
	mb := h.join("room", "b", b, nil)
	mc := h.join("room", "c", c, nil)
	now := time.Now()
	h.forward(mb, keyframeRequest(ma.id, 1), now)
	h.forward(mc, keyframeRequest(ma.id, 1), now.Add(keyframeGap/2))
	if len(a.got) != 1 {
		t.Fatalf("the sender got %d requests in one gap, want 1", len(a.got))
	}
	if binary.BigEndian.Uint32(a.got[0]) != 0 || !bytes.Equal(a.got[0][senderBytes:], keyframeRequest(ma.id, 1)) {
		t.Fatalf("request arrived as %x", a.got[0])
	}
	h.forward(mc, keyframeRequest(ma.id, 2), now.Add(keyframeGap/2))
	h.forward(mb, keyframeRequest(ma.id, 1), now.Add(keyframeGap))
	if len(a.got) != 3 {
		t.Fatalf("another track and the next gap gave %d requests, want 3", len(a.got))
	}
	if len(b.got)+len(c.got) != 0 {
		t.Fatal("a request went to a viewer")
	}
	h.forward(mb, keyframeRequest(9999, 1), now.Add(time.Hour))
	h.forward(mb, keyframeRequest(mb.id, 1), now.Add(time.Hour))
	if len(a.got)+len(b.got) != 3 {
		t.Fatal("a request for an unknown sender or for itself went somewhere")
	}
}

// A viewer that missed a piece gets that piece again, byte for byte, and nobody else does.
func TestResendGivesTheMissingPieceToTheOneWhoAsked(t *testing.T) {
	h := newVideoHub()
	a, b, c := &fakeSession{}, &fakeSession{}, &fakeSession{}
	ma := h.join("room", "a", a, nil)
	mb := h.join("room", "b", b, nil)
	h.join("room", "c", c, nil)
	now := time.Now()
	for piece := uint16(0); piece < 3; piece++ {
		h.forward(ma, pieceOfFrame(1, 7, piece, 3), now)
	}
	original := b.got[1]
	h.forward(mb, resendRequest(ma.id, 1, [2]uint16{7, 1}, [2]uint16{7, 9}, [2]uint16{8, 0}), now)
	if len(b.got) != 4 || !bytes.Equal(b.got[3], original) {
		t.Fatalf("B got %d packets, want the 3 pieces and piece 1 again", len(b.got))
	}
	if len(c.got) != 3 || len(a.got) != 0 {
		t.Fatal("a resend went to someone who didn't ask")
	}
	h.forward(mb, resendRequest(ma.id, 2, [2]uint16{7, 1}), now)
	if len(b.got) != 4 {
		t.Fatal("a piece came back for the wrong track")
	}
}

func TestOnlyTheNewestPiecesAreKept(t *testing.T) {
	v := newVideoMember()
	first := pieceKey{track: 1, frame: 0, piece: 0}
	v.keep(first, []byte{1})
	for i := 1; i <= resendKeep; i++ {
		v.keep(pieceKey{track: 1, frame: uint16(i / 100), piece: uint16(i % 100)}, []byte{2})
	}
	if v.find(first) != nil {
		t.Fatal("the oldest piece outlived the buffer")
	}
	if v.find(pieceKey{track: 1, frame: uint16(resendKeep / 100), piece: uint16(resendKeep % 100)}) == nil {
		t.Fatal("the newest piece is gone")
	}
	if len(v.index) != resendKeep {
		t.Fatalf("index holds %d, want %d", len(v.index), resendKeep)
	}
}

// The SFU's keyframe request to a native sender is all header, so it seals with an empty payload.
func TestARequestWithNoPayloadSealsAndOpens(t *testing.T) {
	aead, _ := chacha20poly1305.New(make([]byte, 32))
	clear := append(make([]byte, senderBytes), keyframeRequest(5, 1)...)
	var r replay
	got, secret, ok := open(aead, dirFromSFU, seal(aead, dirFromSFU, 1, 1, clear, nil), senderBytes+videoHeaderBytes, &r)
	if !ok || len(secret) != 0 || !bytes.Equal(got, clear) {
		t.Fatalf("ok %v, secret %x, clear %x", ok, secret, got)
	}
}

func layersRequest(sender uint32, track, top byte) []byte {
	b := keyframeRequest(sender, track)
	b[1], b[8] = controlLayers, top
	return b
}

func pieceOnLayer(track, layer byte, frame uint16) []byte {
	b := pieceOfFrame(track, frame, 0, 1)
	b[10] = layer
	return b
}

// A viewer capped at layer 0 gets only layer 0 of that track, one that hid it gets none, and the rest stay whole.
func TestViewersGetOnlyTheLayersTheyAskFor(t *testing.T) {
	h := newVideoHub()
	a, b, c := &fakeSession{}, &fakeSession{}, &fakeSession{}
	ma := h.join("room", "a", a, nil)
	mb := h.join("room", "b", b, nil)
	mc := h.join("room", "c", c, nil)
	now := time.Now()
	h.forward(mb, layersRequest(ma.id, 1, 0), now)
	h.forward(mc, layersRequest(ma.id, 1, layersNone), now)
	for frame, layer := range []byte{0, 2, 1, 2, 0} {
		h.forward(ma, pieceOnLayer(1, layer, uint16(frame)), now)
	}
	h.forward(ma, pieceOnLayer(2, 2, 9), now)
	if len(b.got) != 3 {
		t.Fatalf("B capped at layer 0 got %d pieces, want the 2 on layer 0 and track 2's", len(b.got))
	}
	if len(c.got) != 1 {
		t.Fatalf("C hid track 1 and got %d pieces, want only track 2's", len(c.got))
	}
	h.forward(mc, layersRequest(ma.id, 1, layersAll), now)
	h.forward(ma, pieceOnLayer(1, 2, 10), now)
	if len(c.got) != 2 {
		t.Fatal("asking for every layer again didn't bring them back")
	}
	if len(a.got) != 0 {
		t.Fatal("a layers request went to the sender")
	}
	h.forward(mb, layersRequest(mb.id, 1, 0), now)
	h.forward(mb, layersRequest(9999, 1, 0), now)
	if len(mb.wants) != 1 {
		t.Fatalf("B holds wants for %d senders, want only A", len(mb.wants))
	}
	h.leave(ma)
	if len(mb.wants) != 0 {
		t.Fatal("B's wants outlived the sender")
	}
}
