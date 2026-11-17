package audiotransport

import (
	"encoding/binary"
	"time"
)

// A viewer's control datagram on the video hub: version 3, type, track, count, the sender's video id u32, eight more
// bytes, then count pairs of frame u16 and piece u16 for a resend. Only a layers request reads byte 8: the top layer.
const (
	controlVersion  = 3
	controlBytes    = videoHeaderBytes
	controlKeyframe = 1
	controlResend   = 2
	controlLayers   = 3
	maxResend       = 32
	layersAll       = 0xff
	layersNone      = 0xfe
	// One keyframe request per sender and track gets through in this long, however many viewers ask.
	keyframeGap = 300 * time.Millisecond
	// Pieces kept per sender for resends, about a second of a busy screen share.
	resendKeep = 4096
)

type pieceKey struct {
	track        uint8
	frame, piece uint16
}

type keptPiece struct {
	key pieceKey
	out []byte
}

// videoMember is what the video hub keeps for one sender: its recent pieces and when a keyframe was last asked for.
type videoMember struct {
	ring  []keptPiece
	next  int
	index map[pieceKey]int
	asked map[uint8]time.Time
}

func newVideoMember() *videoMember {
	return &videoMember{ring: make([]keptPiece, resendKeep), index: map[pieceKey]int{}, asked: map[uint8]time.Time{}}
}

func pieceOf(b []byte) pieceKey {
	return pieceKey{track: b[2], frame: binary.BigEndian.Uint16(b[4:]), piece: binary.BigEndian.Uint16(b[6:])}
}

// keep holds a forwarded piece, sender id in front, overwriting the oldest.
func (v *videoMember) keep(key pieceKey, out []byte) {
	if old := v.ring[v.next]; old.out != nil && v.index[old.key] == v.next {
		delete(v.index, old.key)
	}
	v.ring[v.next] = keptPiece{key: key, out: out}
	v.index[key] = v.next
	v.next = (v.next + 1) % len(v.ring)
}

func (v *videoMember) find(key pieceKey) []byte {
	if i, ok := v.index[key]; ok {
		return v.ring[i].out
	}
	return nil
}

func validControl(b []byte) bool {
	if len(b) < controlBytes || b[0] != controlVersion {
		return false
	}
	count := int(b[3])
	switch b[1] {
	case controlKeyframe, controlLayers:
		return count == 0 && len(b) == controlBytes
	case controlResend:
		return count > 0 && count <= maxResend && len(b) == controlBytes+4*count
	}
	return false
}

// control answers a viewer's request. Called with h.mu held; returns what to send once it's released.
func (h *hub) control(m *member, b []byte, now time.Time) []func() {
	if !validControl(b) {
		h.dropped.malformed++
		return nil
	}
	senderID := binary.BigEndian.Uint32(b[4:])
	var sender *member
	for _, o := range h.rooms[m.roomID] {
		if o.id == senderID && o != m {
			sender = o
		}
	}
	if sender == nil {
		return nil
	}
	track := b[2]
	if b[1] == controlLayers {
		m.setLayers(senderID, track, b[8])
		return nil
	}
	if sender.video == nil {
		sender.video = newVideoMember()
	}
	if b[1] == controlKeyframe {
		if last, ok := sender.video.asked[track]; ok && now.Sub(last) < keyframeGap {
			return nil
		}
		sender.video.asked[track] = now
		// From the SFU, so sender id 0 in front.
		out := make([]byte, senderBytes+controlBytes)
		copy(out[senderBytes:], b)
		to := sender.out
		return []func(){func() { _ = to.SendDatagram(out) }}
	}
	var sends []func()
	to := m.out
	for i := 0; i < int(b[3]); i++ {
		at := controlBytes + 4*i
		key := pieceKey{track: track, frame: binary.BigEndian.Uint16(b[at:]), piece: binary.BigEndian.Uint16(b[at+2:])}
		if out := sender.video.find(key); out != nil {
			sends = append(sends, func() { _ = to.SendDatagram(out) })
		}
	}
	return sends
}

// setLayers records the highest temporal layer m wants of one sender's track. layersAll forgets it.
func (m *member) setLayers(sender uint32, track, top uint8) {
	if top == layersAll {
		delete(m.wants[sender], track)
		return
	}
	if m.wants == nil {
		m.wants = map[uint32]map[uint8]uint8{}
	}
	if m.wants[sender] == nil {
		m.wants[sender] = map[uint8]uint8{}
	}
	m.wants[sender][track] = top
}

// wantsPiece is whether m takes a piece from sender, by the piece's temporal layer. layersNone is above any layer.
func (m *member) wantsPiece(sender uint32, b []byte) bool {
	top, ok := m.wants[sender][b[2]]
	return !ok || (top != layersNone && b[10] <= top)
}
