package audiotransport

import (
	"bytes"
	"encoding/binary"
	"net"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/chacha20poly1305"
)

func videoPacket(piece, pieces uint16, size int) []byte {
	b := make([]byte, videoHeaderBytes+size)
	b[0] = videoVersion
	b[1] = 1
	b[3] = 1
	binary.BigEndian.PutUint16(b[4:], 7)
	binary.BigEndian.PutUint16(b[6:], piece)
	binary.BigEndian.PutUint16(b[8:], pieces)
	binary.BigEndian.PutUint32(b[12:], 90000)
	for i := videoHeaderBytes; i < len(b); i++ {
		b[i] = byte(i)
	}
	return b
}

func TestVideoDatagramsAreCheckedByVersionAndPieces(t *testing.T) {
	for _, c := range []struct {
		name string
		b    []byte
		want bool
	}{
		{"one piece of one", videoPacket(0, 1, 10), true},
		{"the last of many", videoPacket(maxPieces-1, maxPieces, maxPieceBytes), true},
		{"no payload", videoPacket(0, 1, 0), false},
		{"too long", videoPacket(0, 1, maxPieceBytes+1), false},
		{"piece past the count", videoPacket(3, 3, 10), false},
		{"no pieces", videoPacket(0, 0, 10), false},
		{"too many pieces", videoPacket(0, maxPieces+1, 10), false},
		{"an audio packet", packet(20), false},
	} {
		if got := validVideoDatagram(c.b); got != c.want {
			t.Errorf("%s: got %v", c.name, got)
		}
	}
	if validDatagram(videoPacket(0, 1, 10)) {
		t.Error("the audio hub took a video packet")
	}
}

// A whole keyframe may arrive at once; past that the bucket refills at the video rate.
func TestVideoRateAllowsAKeyframeBurst(t *testing.T) {
	k := newVideoHub().kind
	now := time.Now()
	var b bucket
	for i := 0; i < maxPieces; i++ {
		if !b.allowAt(now, k.rate, k.burst) {
			t.Fatalf("refused piece %d of a keyframe", i)
		}
	}
	if b.allowAt(now, k.rate, k.burst) {
		t.Fatal("allowed past the burst")
	}
	if !b.allowAt(now.Add(2*time.Second/videoRatePerSecond), k.rate, k.burst) {
		t.Fatal("didn't refill")
	}
}

// Over real UDP: A's video reaches B's video session with a 16-byte clear header, and C, in the same room on
// audio only, gets nothing. Audio sent on a video session is dropped.
func TestUDPVideoReachesVideoSessionsOnly(t *testing.T) {
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	u := &udpListener{conn: conn, hosts: []string{"127.0.0.1"}, port: conn.LocalAddr().(*net.UDPAddr).Port, hub: newHub(), video: newVideoHub(), sessions: map[uint32]*udpSession{}}
	go func() { _ = u.serve() }()

	oa, _ := u.offerVideo("a", "room", time.Now())
	ob, _ := u.offerVideo("b", "room", time.Now())
	oc, _ := u.offer("c", "room", time.Now())
	ca, sfu, keyA := dialOffer(t, oa)
	cb, _, keyB := dialOffer(t, ob)
	cc, _, keyC := dialOffer(t, oc)
	aeadA, _ := chacha20poly1305.New(keyA)
	aeadB, _ := chacha20poly1305.New(keyB)
	aeadC, _ := chacha20poly1305.New(keyC)

	hello := videoPacket(0, 1, 4)
	_, _ = cb.WriteToUDP(seal(aeadB, dirToSFU, ob.Session, 1, hello[:videoHeaderBytes], hello[videoHeaderBytes:]), sfu)
	audio := packet(20)
	_, _ = cc.WriteToUDP(seal(aeadC, dirToSFU, oc.Session, 1, audio[:headerBytes], audio[headerBytes:]), sfu)
	time.Sleep(50 * time.Millisecond)

	_, _ = ca.WriteToUDP(seal(aeadA, dirToSFU, oa.Session, 1, audio[:videoHeaderBytes], audio[videoHeaderBytes:]), sfu)
	pkt := videoPacket(2, 5, maxPieceBytes)
	if _, err := ca.WriteToUDP(seal(aeadA, dirToSFU, oa.Session, 2, pkt[:videoHeaderBytes], pkt[videoHeaderBytes:]), sfu); err != nil {
		t.Fatal(err)
	}

	_ = cb.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 2048)
	n, _, err := cb.ReadFromUDP(buf)
	if err != nil {
		t.Fatalf("B saw nothing: %v", err)
	}
	var r replay
	clear, secret, ok := open(aeadB, dirFromSFU, buf[:n], senderBytes+videoHeaderBytes, &r)
	if !ok {
		t.Fatal("B couldn't open what the SFU sent it")
	}
	if !bytes.Equal(clear[senderBytes:], pkt[:videoHeaderBytes]) || !bytes.Equal(secret, pkt[videoHeaderBytes:]) {
		t.Fatal("the piece changed on the way")
	}
	if want := u.video.senders("room")["a"]; binary.BigEndian.Uint32(clear) != want {
		t.Fatalf("sender id %d, want A's video id %d", binary.BigEndian.Uint32(clear), want)
	}
	_ = cb.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	if _, _, err := cb.ReadFromUDP(buf); err == nil {
		t.Fatal("B got a second packet, so the audio-shaped one wasn't dropped")
	}
	_ = cc.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	if _, _, err := cc.ReadFromUDP(buf); err == nil {
		t.Fatal("an audio-only session got video")
	}
	u.video.mu.Lock()
	malformed := u.video.dropped.malformed
	u.video.mu.Unlock()
	if malformed != 1 {
		t.Fatalf("video hub dropped %d malformed, want 1", malformed)
	}
}

func TestOfferCarriesVideoOnlyOnceTurnedOn(t *testing.T) {
	s := New(0, []string{"203.0.113.7"})
	off, err := s.Offer("a", "room")
	if err != nil {
		t.Fatal(err)
	}
	if off.Video != nil {
		t.Fatalf("video offered while off: %+v", off.Video)
	}
	s.EnableVideo()
	on, err := s.Offer("a", "room")
	if err != nil {
		t.Fatal(err)
	}
	if on.Video == nil || len(on.Video.URLs) != 1 || !strings.HasSuffix(on.Video.URLs[0], "/video") {
		t.Fatalf("video offer: %+v", on.Video)
	}
	if _, ok := s.tickets.redeem(on.Video.Ticket, time.Now()); ok {
		t.Fatal("the video ticket opens an audio session")
	}
	if _, ok := s.videoTickets.redeem(on.Video.Ticket, time.Now()); !ok {
		t.Fatal("the video ticket doesn't open a video session")
	}
}
