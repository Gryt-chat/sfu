package audiotransport

import (
	"encoding/binary"
	"testing"
	"time"
)

func TestAVirtualMemberSpeaksAndHearsLikeASession(t *testing.T) {
	s := &Server{hub: newHub()}
	listener := &fakeSession{}
	real := s.hub.join("room-1", "client-a", listener, nil)

	var heard [][]byte
	v := s.JoinVirtual("room-1", "virtual:discord:1", func(b []byte) { heard = append(heard, b) })
	if _, ok := s.hub.senders("room-1")["virtual:discord:1"]; !ok {
		t.Fatal("the virtual member isn't in the room's sender map")
	}

	v.Send(packet(20))
	if len(listener.got) != 1 || binary.BigEndian.Uint32(listener.got[0]) != v.m.id {
		t.Fatalf("the room didn't get the virtual member's packet under its id: %v", listener.got)
	}

	s.hub.forward(real, packet(10), time.Now())
	if len(heard) != 1 || binary.BigEndian.Uint32(heard[0]) != real.id {
		t.Fatalf("a listening virtual member didn't hear the room: %v", heard)
	}

	v.Send(packet(0))
	if s.hub.dropped.malformed != 1 {
		t.Fatal("a malformed virtual packet was forwarded")
	}

	v.Leave()
	if _, ok := s.hub.senders("room-1")["virtual:discord:1"]; ok {
		t.Fatal("the virtual member stayed after leaving")
	}
}

func TestAVirtualMemberWithoutAListenerHearsNothing(t *testing.T) {
	s := &Server{hub: newHub()}
	real := s.hub.join("room-1", "client-a", &fakeSession{}, nil)
	s.JoinVirtual("room-1", "virtual:discord:2", nil)
	s.hub.forward(real, packet(10), time.Now())
}
