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

A voice channel and a call are the same thing down here, and **only one of them
should be ended for being quiet**. A channel is a place, and sitting in one
alone is ordinary. A call is an event between named people, and being the only
one left in it means it is over.

The client ends its own call too. This is the half that catches the ones which
never will — closed laptops, wedged tabs, anything modified — which are exactly
the ones leaving rooms up.
*/

// callRoomID matches the ids the server gives a conversation, and only those.
//
// **The whole shape, not the `dm_` prefix.** An admin may name a channel
// `dm_anything`, and the server calls that cost cosmetic because there the
// worst case is a channel left out of a member list — here it is hanging up on
// somebody sitting in a channel.
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
// **Sweeping observes the state rather than trusting an "alone since" field.**
// Every path that changes who is in a room would have to keep one honest,
// including the stale-peer eviction inside AddPeerToRoom, and forgetting one
// means a call that never ends or one that ends while two people are talking.
// The cost is a deadline accurate to one sweep interval.
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

// StillHere restarts the alone clock for a room, and reports whether it did —
// somebody pressing "stay in the call" (GRYT-715).
//
// **It restarts rather than granting one reprieve, and there is no cap.**
// Somebody was at the keyboard when they pressed it, which is the entire thing
// the sweep is trying to find out; what it exists to catch is the client that
// never speaks.
//
// Writes `now` rather than deleting the entry, so the clock restarts here
// instead of on the next sweep.
func (m *Manager) StillHere(roomID string) bool {
	moved := false

	recovery.SafeExecuteWithContext("ROOM_MANAGER", "STILL_HERE", "", roomID, "Restarting the alone clock", func() error {
		m.mutex.Lock()
		defer m.mutex.Unlock()

		room, exists := m.rooms[roomID]
		if !exists {
			return nil
		}

		// Only calls, so a client cannot fill the map with entries for rooms
		// the sweep will never look at. A voice channel is never ended for
		// being quiet, so it has no clock to restart.
		room.mutex.RLock()
		isCall := IsCallRoom(roomID, room.ServerID)
		room.mutex.RUnlock()
		if !isCall {
			return nil
		}

		m.aloneSince[roomID] = time.Now()
		moved = true
		return nil
	})

	return moved
}
