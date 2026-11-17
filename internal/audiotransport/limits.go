package audiotransport

import (
	"encoding/binary"
	"time"
)

// The datagram the voice engine sends (@gryt/voice transport/packet.ts): an 8-byte header, version
// first, then one Opus packet. Only its size and version are checked here; the audio is not read.
const (
	packetVersion = 1
	headerBytes   = 8
	maxOpusBytes  = 1275
	// The sender id the hub puts in front of a forwarded packet.
	senderBytes = 4
)

// validDatagram is the only parsing the SFU does on audio: anything else is dropped unread.
func validDatagram(b []byte) bool {
	return len(b) > headerBytes && len(b) <= headerBytes+maxOpusBytes && b[0] == packetVersion
}

// One piece of one encoded frame: version 2, flags (1 keyframe), track, codec, frame u16, piece u16, pieces u16,
// temporal layer, a zero byte, a 90 kHz timestamp u32, then the piece. Only the version and piece numbers are read.
const (
	videoVersion     = 2
	videoHeaderBytes = 16
	maxPieceBytes    = 1100
	// A frame of 1024 pieces is about 1.1 MB, past any keyframe a screen share sends.
	maxPieces = 1024
)

func validVideoDatagram(b []byte) bool {
	if len(b) <= videoHeaderBytes || len(b) > videoHeaderBytes+maxPieceBytes || b[0] != videoVersion {
		return false
	}
	piece, pieces := binary.BigEndian.Uint16(b[6:]), binary.BigEndian.Uint16(b[8:])
	return pieces > 0 && pieces <= maxPieces && piece < pieces
}

// One sender at 2.5 ms frames sends 400 a second. A little over that, and a short burst, are allowed;
// more is dropped, so one client can't flood a room that fans it out to everyone.
const (
	ratePerSecond = 500
	rateBurst     = 50
	// About 35 Mbit/s of video, with room for one whole keyframe at once.
	videoRatePerSecond = 4000
	videoRateBurst     = maxPieces
)

// kind is what a hub carries: how long the clear header is, what counts as a packet, and how fast one may come.
type kind struct {
	header      int
	valid       func([]byte) bool
	rate, burst float64
}

var (
	audioKind = kind{header: headerBytes, valid: validDatagram, rate: ratePerSecond, burst: rateBurst}
	videoKind = kind{header: videoHeaderBytes, valid: validVideoDatagram, rate: videoRatePerSecond, burst: videoRateBurst}
)

type bucket struct {
	tokens float64
	last   time.Time
}

func (b *bucket) allow(now time.Time) bool { return b.allowAt(now, ratePerSecond, rateBurst) }

func (b *bucket) allowAt(now time.Time, rate, burst float64) bool {
	if b.last.IsZero() {
		b.tokens = burst
	} else {
		b.tokens += now.Sub(b.last).Seconds() * rate
		if b.tokens > burst {
			b.tokens = burst
		}
	}
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}
