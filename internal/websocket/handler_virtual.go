package websocket

import (
	"encoding/binary"
	"sync"

	"sfu-v2/internal/audiotransport"
	"sfu-v2/internal/recovery"
	"sfu-v2/pkg/types"
)

// A binary frame on the server connection: op, peer id length, peer id, then one own-transport datagram.
// Op 2, room to server, has the sender's user id (length, then id) in front of the datagram.
const (
	frameToRoom   = 1
	frameFromRoom = 2
	maxVirtualID  = 128
)

type virtualPeer struct {
	v      *audiotransport.Virtual
	roomID string
	stop   chan struct{}
}

// virtualSet is the virtual members one server connection added; they go when it does.
type virtualSet struct {
	mu    sync.Mutex
	peers map[string]virtualPeer
}

func virtualPeerID(userID string) string { return "virtual:" + userID }

func (h *Handler) handleVirtualJoin(conn *ThreadSafeWriter, set *virtualSet, data string) error {
	var req types.VirtualVoiceData
	if err := recovery.SafeJSONUnmarshal([]byte(data), &req); err != nil {
		return err
	}
	if h.audio == nil || req.UserID == "" || len(virtualPeerID(req.UserID)) > maxVirtualID {
		return nil
	}
	if !h.roomManager.ServerOwnsRoom(req.ServerID, req.ServerPassword, req.RoomID) {
		h.debugLog("❌ virtual_voice_join: server '%s' failed credentials or does not own room '%s'", req.ServerID, req.RoomID)
		return nil
	}
	peerID := virtualPeerID(req.UserID)
	var out func([]byte)
	var done chan struct{}
	if req.Listen {
		// The hub sends on the speaker's goroutine, so a slow server connection drops packets instead of holding the room.
		queue, stop := make(chan []byte, 256), make(chan struct{})
		done = stop
		go func() {
			for {
				select {
				case b := <-queue:
					_ = conn.WriteBinary(b)
				case <-stop:
					return
				}
			}
		}()
		// By user id rather than the hub's number, so the server can leave out someone switched off for the bridge.
		users := map[uint32]string{}
		var usersMu sync.Mutex
		out = func(b []byte) {
			if len(b) < 4 {
				return
			}
			id := binary.BigEndian.Uint32(b)
			usersMu.Lock()
			user, ok := users[id]
			if !ok {
				for peer, n := range h.audio.Senders(req.RoomID) {
					if u, live := h.live.userOf(peer); live {
						users[n] = u
					}
				}
				user, ok = users[id]
			}
			usersMu.Unlock()
			if !ok || len(user) > maxVirtualID {
				return
			}
			select {
			case queue <- frame(frameFromRoom, peerID, append(append([]byte{byte(len(user))}, user...), b[4:]...)):
			default:
			}
		}
	}
	set.mu.Lock()
	if set.peers == nil {
		set.peers = make(map[string]virtualPeer)
	}
	if old, ok := set.peers[peerID]; ok {
		old.v.Leave()
		if old.stop != nil {
			close(old.stop)
		}
	}
	// In live first, so the sender map the join triggers already names the user.
	h.live.add(peerID, req.RoomID, req.UserID, nil)
	set.peers[peerID] = virtualPeer{v: h.audio.JoinVirtual(req.RoomID, peerID, out), roomID: req.RoomID, stop: done}
	set.mu.Unlock()
	h.debugLog("🌉 virtual voice: %s joined room %s (listen=%v)", req.UserID, req.RoomID, req.Listen)
	return nil
}

func (h *Handler) handleVirtualLeave(set *virtualSet, data string) error {
	var req types.VirtualVoiceData
	if err := recovery.SafeJSONUnmarshal([]byte(data), &req); err != nil {
		return err
	}
	h.leaveVirtual(set, virtualPeerID(req.UserID))
	return nil
}

func (h *Handler) leaveVirtual(set *virtualSet, peerID string) {
	set.mu.Lock()
	p, ok := set.peers[peerID]
	delete(set.peers, peerID)
	set.mu.Unlock()
	if ok {
		p.v.Leave()
		if p.stop != nil {
			close(p.stop)
		}
		h.live.remove(peerID)
	}
}

func (h *Handler) leaveAllVirtual(set *virtualSet) {
	set.mu.Lock()
	ids := make([]string, 0, len(set.peers))
	for id := range set.peers {
		ids = append(ids, id)
	}
	set.mu.Unlock()
	for _, id := range ids {
		h.leaveVirtual(set, id)
	}
}

// handleVirtualFrame sends a datagram for a member this connection added; anything else is dropped.
func (h *Handler) handleVirtualFrame(set *virtualSet, b []byte) {
	if len(b) < 2 || b[0] != frameToRoom || len(b) < 2+int(b[1]) {
		return
	}
	peerID := string(b[2 : 2+int(b[1])])
	set.mu.Lock()
	p, ok := set.peers[peerID]
	set.mu.Unlock()
	if ok {
		p.v.Send(b[2+int(b[1]):])
	}
}

func frame(op byte, peerID string, payload []byte) []byte {
	out := make([]byte, 0, 2+len(peerID)+len(payload))
	out = append(out, op, byte(len(peerID)))
	out = append(out, peerID...)
	return append(out, payload...)
}
