package audiotransport

import "time"

// funcSender hands a virtual member's incoming datagrams to whoever added it.
type funcSender func([]byte)

func (f funcSender) SendDatagram(b []byte) error {
	f(b)
	return nil
}

// Virtual is a member whose audio comes from the Gryt server rather than a client, such as a Discord bridge.
type Virtual struct {
	hub *hub
	m   *member
}

// JoinVirtual adds peerID to roomID's audio. out gets the clients' datagrams, sender id in front, and never another virtual member's; nil drops them.
func (s *Server) JoinVirtual(roomID, peerID string, out func([]byte)) *Virtual {
	if out == nil {
		out = func([]byte) {}
	}
	m := s.hub.join(roomID, peerID, funcSender(out), nil)
	s.hub.mu.Lock()
	m.virtual = true
	s.hub.mu.Unlock()
	return &Virtual{hub: s.hub, m: m}
}

// Send fans one datagram out to the room, under the same checks and rate limit as a client's.
func (v *Virtual) Send(b []byte) { v.hub.forward(v.m, b, time.Now()) }

// Leave takes the member out of the room.
func (v *Virtual) Leave() { v.hub.leave(v.m) }
