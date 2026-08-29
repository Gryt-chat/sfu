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

// SetLayerData lets a client manually set the max temporal layer for a track.
type SetLayerData struct {
	TrackID          string `json:"track_id"`
	MaxTemporalLayer int    `json:"max_temporal_layer"` // -1 = all layers, 0 = T0 only, 1 = T0+T1, 2 = T0+T1+T2
}

// RoomJoinedData is the payload of the room_joined a client gets on joining.
//
// The server connection gets a plain string on the same event, from
// sendSuccessToConnection. Only the client join carries this, because only a
// client has a countdown to draw.
type RoomJoinedData struct {
	Message string `json:"message"`

	// CallAloneTimeoutSeconds is how long this SFU lets one person sit alone in
	// a call before it ends it — SFU_CALL_ALONE_TIMEOUT, in seconds, with zero
	// meaning the sweep is off.
	//
	// Sent so the client stops guessing. The client used to carry its own copy
	// of the default, which was right until an operator changed theirs: raising
	// it made the client leave early, and turning it off left a client that
	// still hung up after two minutes on a call the SFU was happy to keep.
	CallAloneTimeoutSeconds int `json:"call_alone_timeout_seconds"`
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
	EventSetLayer         = "set_layer"
	EventStillHere        = "still_here"
)
