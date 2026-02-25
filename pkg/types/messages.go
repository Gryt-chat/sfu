package types

// WebSocketMessage represents the structure for WebSocket messages
type WebSocketMessage struct {
	Event string `json:"event"`
	Data  string `json:"data"`
}

// ServerRegistrationData represents server registration information
type ServerRegistrationData struct {
	ServerID       string `json:"server_id"`
	ServerPassword string `json:"server_password"`
	RoomID         string `json:"room_id"`
}

// ClientJoinData represents client join information
type ClientJoinData struct {
	RoomID         string `json:"room_id"`
	ServerID       string `json:"server_id"`
	ServerPassword string `json:"server_password"`
	UserToken      string `json:"user_token"`
	UserID         string `json:"user_id"`
}

// DisconnectUserData is sent by the server to force-disconnect a user from a room.
type DisconnectUserData struct {
	RoomID         string `json:"room_id"`
	UserID         string `json:"user_id"`
	ServerID       string `json:"server_id"`
	ServerPassword string `json:"server_password"`
}

// PeerEventData is sent from SFU to server when a peer joins or leaves.
type PeerEventData struct {
	RoomID string `json:"room_id"`
	UserID string `json:"user_id"`
}

// RoomPeers describes connected users in a single room (used in sync responses).
type RoomPeers struct {
	RoomID  string   `json:"room_id"`
	UserIDs []string `json:"user_ids"`
}

// SyncResponseData is the payload for a sync_response from SFU to server.
type SyncResponseData struct {
	Rooms []RoomPeers `json:"rooms"`
}

// SyncRequestData is sent by the server to request the current peer state.
type SyncRequestData struct {
	ServerID       string `json:"server_id"`
	ServerPassword string `json:"server_password"`
}

// AudioControlData is sent by the server to update a user's mute/deafen state.
type AudioControlData struct {
	RoomID         string `json:"room_id"`
	UserID         string `json:"user_id"`
	ServerID       string `json:"server_id"`
	ServerPassword string `json:"server_password"`
	IsMuted        bool   `json:"is_muted"`
	IsDeafened     bool   `json:"is_deafened"`
}

// Supported WebSocket message events
const (
	EventOffer            = "offer"
	EventAnswer           = "answer"
	EventCandidate        = "candidate"
	EventServerRegister   = "server_register"
	EventClientJoin       = "client_join"
	EventRoomJoined       = "room_joined"
	EventRoomError        = "room_error"
	EventKeepAlive        = "keep_alive"
	EventDisconnectUser   = "disconnect_user"
	EventUserAudioControl = "user_audio_control"
	EventPeerJoined       = "peer_joined"
	EventPeerLeft         = "peer_left"
	EventRenegotiate      = "renegotiate"
	EventSyncRequest      = "sync_request"
	EventSyncResponse     = "sync_response"
)
