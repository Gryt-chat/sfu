package websocket

import (
	"encoding/json"
	"strconv"

	"sfu-v2/pkg/types"
)

// sendAudioSenders tells everyone in a room which own-transport sender id is which user, each time
// a session joins or leaves it (GRYT-1830). A peer that has already left is skipped.
func (h *Handler) sendAudioSenders(roomID string, senders map[string]uint32) {
	users := make(map[string]string, len(senders))
	for peerID, id := range senders {
		if userID, ok := h.live.userOf(peerID); ok {
			users[strconv.FormatUint(uint64(id), 10)] = userID
		}
	}
	payload, err := json.Marshal(types.AudioSendersData{Senders: users})
	if err != nil {
		return
	}
	conns, err := h.roomManager.GetConnectionsInRoom(roomID)
	if err != nil {
		return
	}
	msg := &types.WebSocketMessage{Event: types.EventAudioSenders, Data: string(payload)}
	for clientID, conn := range conns {
		if err := conn.WriteJSON(msg); err != nil {
			h.debugLog("⚠️ audio_senders to %s failed: %v", clientID, err)
		}
	}
}
