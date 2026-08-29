package room

import (
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"sfu-v2/internal/recovery"
)

/*
Ending a call that one person is sitting in on their own (GRYT-711).

A voice channel and a call are the same thing down here — an SFU room with
peers in it — and only one of them should be ended for being quiet.

A channel is a place. Sitting in one alone is an ordinary thing to do: you are
waiting for somebody, or you like having it open. Hanging that up would be
wrong. A call is an event between named people, and being the only one left in
it means it is over. What usually happens is that somebody walks off, or a tab
stays open behind forty others, and the room, its peer connection and its
socket stay up until the process restarts.

The client ends its own call too, and says so before it does. This is the half
that catches the clients which never will: closed laptops, wedged tabs, and
anything modified. Those are exactly the ones leaving rooms up, so this cannot
be left to good manners.
*/

// callRoomID matches the ids the server gives a conversation, and only those.
//
// `directConversationId` produces `dm_` and 32 hex characters;
// `createGroupConversation` produces `dm_g` and 32 more. Both are in
// `db/sqlite/conversations.ts`, which also points out that a prefix test can be
// fooled — an admin may name a channel `dm_anything`.
//
// The server calls that cost cosmetic, because there the worst case is a
// channel left out of a member list. Here the worst case is hanging up on
// somebody sitting in a channel, so the whole shape is matched rather than the
// first three characters. Naming a channel `dm_` followed by exactly 32 hex
// digits is no longer something anybody does by accident.
var callRoomID = regexp.MustCompile(`^dm_g?[0-9a-f]{32}$`)

// IsCallRoom reports whether an SFU room is a call rather than a voice channel.
//
// The SFU knows a room as `serverID_channelID` — `sfuRoomId` on the server
// side — and a server id may itself contain an underscore, so the prefix is
// taken off by the server id the room already carries rather than by splitting
// on the separator.
func IsCallRoom(roomID, serverID string) bool {
	if serverID == "" {
		return false
	}
	channelID := strings.TrimPrefix(roomID, serverID+"_")
	if channelID == roomID {
		return false
	}
	return callRoomID.MatchString(channelID)
}

// EndAbandonedCalls hangs up on calls that one person has been alone in for
// longer than timeout. A timeout of zero switches it off.
//
// When the clock starts is decided here rather than by the room, and that is
// deliberate. Every path that changes who is in a room would otherwise have to
// remember to keep an "alone since" field honest — including the eviction of a
// stale peer that happens in the middle of AddPeerToRoom — and the failure
// mode of forgetting one is a call that never ends or one that ends while two
// people are talking. Sweeping observes the state instead of trusting anybody
// to maintain it, and the cost is that the deadline is accurate to one sweep
// interval.
func (m *Manager) EndAbandonedCalls(timeout time.Duration) {
	if timeout <= 0 {
		return
	}

	recovery.SafeExecuteWithContext("ROOM_MANAGER", "END_ABANDONED_CALLS", "", "", fmt.Sprintf("Alone for: %v", timeout), func() error {
		now := time.Now()

		type victim struct {
			roomID   string
			clientID string
			userID   string
			conn     JSONWriter
			alone    time.Duration
		}
		var victims []victim

		m.mutex.Lock()
		seen := map[string]bool{}

		for roomID, room := range m.rooms {
			room.mutex.RLock()
			peers := len(room.PeerConnections)
			serverID := room.ServerID

			// Only the one-peer case. Zero is already handled: the room empties
			// and CleanupEmptyRooms takes it away.
			if peers != 1 || !IsCallRoom(roomID, serverID) {
				room.mutex.RUnlock()
				continue
			}

			seen[roomID] = true
			since, known := m.aloneSince[roomID]
			if !known {
				m.aloneSince[roomID] = now
				room.mutex.RUnlock()
				continue
			}

			if now.Sub(since) > timeout {
				for clientID, conn := range room.Connections {
					victims = append(victims, victim{
						roomID:   roomID,
						clientID: clientID,
						userID:   room.UserIDs[clientID],
						conn:     conn,
						alone:    now.Sub(since),
					})
					break
				}
			}
			room.mutex.RUnlock()
		}

		// Anything that filled up again, emptied, or was deleted. Left behind,
		// a room that went back to two people and later back to one would be
		// hung up on immediately, using a clock from the previous time.
		for roomID := range m.aloneSince {
			if !seen[roomID] {
				delete(m.aloneSince, roomID)
			}
		}
		m.mutex.Unlock()

		// Closing outside the lock. The close runs the connection's own
		// teardown, which calls back into RemovePeerFromRoom and takes m.mutex.
		for _, v := range victims {
			m.debugLog("📴 Ending call '%s': user '%s' has been alone in it for %v", v.roomID, v.userID, v.alone.Round(time.Second))
			if closer, ok := v.conn.(io.Closer); ok {
				go closer.Close()
			}
		}

		return nil
	})
}
