package websocket

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pion/webrtc/v4"

	"sfu-v2/internal/config"
	"sfu-v2/internal/metrics"
	"sfu-v2/internal/recovery"
	"sfu-v2/internal/room"
	"sfu-v2/internal/track"
	peerManager "sfu-v2/internal/webrtc"
	"sfu-v2/pkg/types"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// ErrServerFull is the capacity guardrail in handler_join.go, given a name so
// the close frame can be 1013 "try again later" rather than a flat 1011. A
// client that is turned away because the box is full should be able to tell
// that apart from one that hit a bug.
var ErrServerFull = errors.New("server full")

// ErrPeerUnresponsive is the read deadline in keepalive.go firing: the peer
// stopped answering pings, so the SFU stopped waiting for it. Given a name for
// the same reason ErrServerFull has one — "we gave up on you" and "we hit a
// bug" are different things and should not arrive as the same close code.
var ErrPeerUnresponsive = errors.New("peer unresponsive")

// closeCodePingTimeout says the SFU hung up because nothing came back.
//
// A private-use code rather than one of the 1000-series, none of which means
// "you stopped answering" — 1011 is a bug on our side, 1013 means full, 1001 is
// the server going away, and reusing one puts this case in the same log line as
// something it is not.
//
// Most of the time nobody is there to read the frame, so its real audience is
// this SFU's log. When the peer is merely slow it arrives, and the client
// treats anything but a clean 1000 or 1001 as cause to reconnect.
const closeCodePingTimeout = 4000

// isReadDeadline reports whether a read ended because the deadline armed in
// keepalive.go expired, rather than because the connection broke or the peer
// closed it.
func isReadDeadline(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, os.ErrDeadlineExceeded) {
		return true
	}

	// Belt and braces for anything that reports a timeout without wrapping the
	// sentinel. Cheap, and the alternative is this case quietly reporting
	// itself as "sfu error".
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

// closeStatusFor picks the close code and reason for a connection that is
// ending, from whatever the handler returned.
//
// The reason strings are fixed rather than the error text. They travel to
// whoever is connected, error strings here carry token and validation detail,
// and the code is what answers the question worth answering: was this the SFU
// or was this the network? 1006 — which is what a bare Close() produces, and
// what this exists to stop — means neither end said anything, so nothing here
// ever returns it.
func closeStatusFor(err error) (code int, reason string) {
	switch {
	case err == nil:
		return websocket.CloseNormalClosure, ""
	case errors.Is(err, ErrServerFull):
		return websocket.CloseTryAgainLater, "server full"
	case errors.Is(err, ErrPeerUnresponsive):
		return closeCodePingTimeout, "ping timeout"
	default:
		return websocket.CloseInternalServerErr, "sfu error"
	}
}

// Coordinator interface to avoid circular imports
type Coordinator interface {
	SignalPeerConnectionsInRoom(roomID string)
	OnTrackAddedToRoom(roomID string)
	OnTrackRemovedFromRoom(roomID string)
}

// Handler manages WebSocket connections and integrates with other components
type Handler struct {
	config        *config.Config
	webrtcAPI     *webrtc.API
	trackManager  *track.Manager
	webrtcManager *peerManager.Manager
	roomManager   *room.Manager
	coordinator   Coordinator
}

// NewHandler creates a new WebSocket handler
func NewHandler(cfg *config.Config, webrtcAPI *webrtc.API, trackManager *track.Manager, webrtcManager *peerManager.Manager, roomManager *room.Manager, coordinator Coordinator) *Handler {
	return &Handler{
		config:        cfg,
		webrtcAPI:     webrtcAPI,
		trackManager:  trackManager,
		webrtcManager: webrtcManager,
		roomManager:   roomManager,
		coordinator:   coordinator,
	}
}

// debugLog logs debug messages if debug mode is enabled
func (h *Handler) debugLog(format string, args ...interface{}) {
	if h.config.Debug {
		log.Printf("[WEBSOCKET] "+format, args...)
	}
}

// generateClientID generates a unique client ID
func generateClientID() string {
	bytes := make([]byte, 16)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

// HandleWebSocket handles incoming WebSocket connections
func (h *Handler) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	recovery.SafeExecuteWithContext("WEBSOCKET", "HANDLE_CONNECTION", "", "", r.RemoteAddr, func() error {
		unsafeConn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			if h.config.Debug {
				h.debugLog("❌ WebSocket upgrade error: %v", err)
			}
			return err
		}

		metrics.WebSocketConnections.Inc()
		safeConn := NewThreadSafeWriter(unsafeConn)

		// Started here rather than inside either handler, so it covers the
		// join handshake too. A socket that connects and then never sends
		// client_join reaches no room and no peer, and before this it sat in
		// a blocking read with nothing to end it.
		keepAlive := StartKeepAlive(safeConn, h.config.PingInterval, h.config.PongTimeout)
		defer keepAlive.Stop()

		// Why this connection ended, so the close frame below can say. Set by
		// the handler that owns the connection, read by the deferred close
		// after that handler has returned.
		var handlerErr error

		defer func() {
			metrics.WebSocketConnections.Dec()
			recovery.SafeExecute("WEBSOCKET", "CLOSE_CONNECTION", func() error {
				code, reason := closeStatusFor(handlerErr)
				safeConn.CloseWithReason(code, reason)
				return nil
			})
		}()

		clientID := generateClientID()
		parsedURL, _ := url.Parse(r.RequestURI)

		h.debugLog("🔌 New WebSocket connection: %s (Path: %s, RemoteAddr: %s)", clientID, parsedURL.Path, r.RemoteAddr)

		switch parsedURL.Path {
		case "/server":
			h.debugLog("🖥️  Handling server connection: %s", clientID)
			handlerErr = h.handleServerConnection(safeConn, clientID)
		case "/client":
			h.debugLog("👤 Handling client connection: %s", clientID)
			handlerErr = h.handleClientConnection(safeConn, clientID, r)
		default:
			h.debugLog("👤 Handling default client connection: %s", clientID)
			handlerErr = h.handleClientConnection(safeConn, clientID, r)
		}

		// Translated in one place rather than in each of the three read loops.
		// A read that ended on the deadline is a timeout error from the
		// underlying socket, and every one of those would otherwise reach
		// closeStatusFor as "anything else" and be reported as an SFU bug.
		if isReadDeadline(handlerErr) {
			metrics.PingTimeouts.Inc()
			h.debugLog("💀 No word from %s in %s — hanging up", clientID, h.config.PongTimeout)
			handlerErr = fmt.Errorf("%w after %s", ErrPeerUnresponsive, h.config.PongTimeout)
		}

		return handlerErr
	})
}

// handleServerConnection handles server registration connections
func (h *Handler) handleServerConnection(conn *ThreadSafeWriter, clientID string) error {
	return recovery.SafeExecuteWithContext("WEBSOCKET", "HANDLE_SERVER", clientID, "", "Server connection handling", func() error {
		h.debugLog("🖥️  Server connection established: %s", clientID)

		for {
			var raw []byte
			var err error

			err = recovery.SafeExecuteWithContext("WEBSOCKET", "READ_SERVER_MESSAGE", clientID, "", "Reading server message", func() error {
				_, raw, err = conn.ReadMessage()
				return err
			})

			if err != nil {
				h.debugLog("❌ Error reading server message from %s: %v", clientID, err)
				return err
			}

			message := &types.WebSocketMessage{}
			if err := recovery.SafeJSONUnmarshal(raw, &message); err != nil {
				h.debugLog("❌ Error unmarshalling server message from %s: %v", clientID, err)
				continue
			}

			h.debugLog("📨 Server message from %s: event=%s", clientID, message.Event)

			err = recovery.SafeExecuteWithContext("WEBSOCKET", "PROCESS_SERVER_MESSAGE", clientID, "", message.Event, func() error {
				switch message.Event {
				case types.EventServerRegister:
					return h.handleServerRegistration(conn, clientID, message.Data)
				case types.EventDisconnectUser:
					return h.handleDisconnectUser(message.Data)
				case types.EventUserAudioControl:
					return h.handleUserAudioControl(message.Data)
				case types.EventSyncRequest:
					return h.handleSyncRequest(conn, message.Data)
				case types.EventKeepAlive:
					if h.config.Debug {
						h.debugLog("💓 Keep-alive received from server %s", clientID)
					}
					return nil
				default:
					h.debugLog("❓ Unknown server event from %s: %s", clientID, message.Event)
					return nil
				}
			})

			if err != nil {
				h.debugLog("❌ Error processing server message from %s: %v", clientID, err)
			}
		}
	})
}

// handleServerRegistration processes server registration
func (h *Handler) handleServerRegistration(conn *ThreadSafeWriter, clientID, data string) error {
	var regData types.ServerRegistrationData
	if err := recovery.SafeJSONUnmarshal([]byte(data), &regData); err != nil {
		h.debugLog("❌ Error unmarshalling server registration data from %s: %v", clientID, err)
		h.sendErrorToConnection(conn, "Invalid registration data")
		return err
	}

	h.debugLog("🖥️  Server registration attempt: ServerID=%s, RoomID=%s", regData.ServerID, regData.RoomID)

	if err := h.roomManager.RegisterServer(regData.ServerID, regData.ServerPassword, regData.RoomID); err != nil {
		h.debugLog("❌ Server registration failed for %s: %v", regData.ServerID, err)
		h.sendErrorToConnection(conn, "Registration failed: "+err.Error())
		return err
	}

	// Store the server connection so peer join/leave notifications can be sent back.
	h.roomManager.SetServerConnection(regData.ServerID, conn)

	h.debugLog("✅ Server %s registered room %s successfully", regData.ServerID, regData.RoomID)
	h.sendSuccessToConnection(conn, "Server registered successfully")
	return nil
}

// handleDisconnectUser processes a server request to force-disconnect a user.
func (h *Handler) handleDisconnectUser(data string) error {
	var req types.DisconnectUserData
	if err := recovery.SafeJSONUnmarshal([]byte(data), &req); err != nil {
		h.debugLog("❌ Error unmarshalling disconnect_user data: %v", err)
		return err
	}

	if !h.roomManager.ValidateServerCredentials(req.ServerID, req.ServerPassword) {
		h.debugLog("❌ disconnect_user: invalid credentials for server '%s'", req.ServerID)
		return nil
	}

	h.debugLog("🔌 disconnect_user: server=%s room=%s user=%s", req.ServerID, req.RoomID, req.UserID)
	if err := h.roomManager.DisconnectUser(req.RoomID, req.UserID); err != nil {
		h.debugLog("❌ disconnect_user failed: %v", err)
	}
	return nil
}

// handleUserAudioControl processes a server request to update a user's mute/deafen state.
func (h *Handler) handleUserAudioControl(data string) error {
	var req types.AudioControlData
	if err := recovery.SafeJSONUnmarshal([]byte(data), &req); err != nil {
		h.debugLog("❌ Error unmarshalling user_audio_control data: %v", err)
		return err
	}

	if !h.roomManager.ValidateServerCredentials(req.ServerID, req.ServerPassword) {
		h.debugLog("❌ user_audio_control: invalid credentials for server '%s'", req.ServerID)
		return nil
	}

	h.debugLog("🔇 user_audio_control: server=%s room=%s user=%s muted=%v deafened=%v",
		req.ServerID, req.RoomID, req.UserID, req.IsMuted, req.IsDeafened)

	if err := h.roomManager.SetUserDeafened(req.RoomID, req.UserID, req.IsDeafened); err != nil {
		h.debugLog("❌ user_audio_control: failed to set deafen state: %v", err)
		return nil
	}

	go h.coordinator.SignalPeerConnectionsInRoom(req.RoomID)
	return nil
}

// handleSyncRequest responds with all connected peers for the requesting server.
func (h *Handler) handleSyncRequest(conn *ThreadSafeWriter, data string) error {
	var req types.SyncRequestData
	if err := recovery.SafeJSONUnmarshal([]byte(data), &req); err != nil {
		h.debugLog("❌ Error unmarshalling sync_request data: %v", err)
		return err
	}

	if !h.roomManager.ValidateServerCredentials(req.ServerID, req.ServerPassword) {
		h.debugLog("❌ sync_request: invalid credentials for server '%s'", req.ServerID)
		return nil
	}

	rooms := h.roomManager.GetRoomPeersForServer(req.ServerID)
	payload := types.SyncResponseData{Rooms: rooms}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	h.debugLog("📡 sync_response: server=%s rooms=%d", req.ServerID, len(rooms))
	return conn.WriteJSON(&types.WebSocketMessage{
		Event: types.EventSyncResponse,
		Data:  string(payloadBytes),
	})
}

// handleClientMessages processes incoming WebSocket messages from clients
func (h *Handler) handleClientMessages(conn *ThreadSafeWriter, peerConnection *webrtc.PeerConnection, roomID, clientID string) error {
	return recovery.SafeExecuteWithContext("WEBSOCKET", "HANDLE_CLIENT_MESSAGES", clientID, roomID, "Processing client messages", func() error {
		h.debugLog("📨 Starting message handling for client %s in room '%s'", clientID, roomID)

		message := &types.WebSocketMessage{}
		messageCount := 0
		pendingRenegotiate := false

		// When this connection last said it was still here. Per-connection, so
		// it costs a comparison rather than another entry in a map under the
		// room manager's lock — which is the thing being protected. still_here
		// is the one client message that takes that lock, and a client sending
		// it in a loop would serialise it against every room operation in the
		// process. Nothing is gained by sending it faster than this: the clock
		// it restarts is two minutes long.
		var lastStillHere time.Time

		for {
			var raw []byte
			var err error

			err = recovery.SafeExecuteWithContext("WEBSOCKET", "READ_CLIENT_MESSAGE", clientID, roomID, "Reading client message", func() error {
				_, raw, err = conn.ReadMessage()
				return err
			})

			if err != nil {
				if websocket.IsCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
					h.debugLog("🔌 WebSocket closed normally for %s: %v", clientID, err)
					break
				}

				h.debugLog("❌ Error reading WebSocket message from %s: %v", clientID, err)
				return err
			}

			messageCount++

			if err := recovery.SafeJSONUnmarshal(raw, &message); err != nil {
				h.debugLog("❌ Error unmarshalling WebSocket message from %s: %v", clientID, err)
				continue
			}

			h.debugLog("📨 Message #%d from %s in room '%s': event=%s", messageCount, clientID, roomID, message.Event)

			err = recovery.SafeExecuteWithContext("WEBSOCKET", "PROCESS_CLIENT_MESSAGE", clientID, roomID, message.Event, func() error {
				switch message.Event {
				case types.EventCandidate:
					return h.handleICECandidate(peerConnection, message.Data, clientID)
				case types.EventAnswer:
					if answerErr := h.handleAnswer(peerConnection, message.Data, clientID); answerErr != nil {
						return answerErr
					}
					// The peer is stable again, so anything it asked for while
					// it was not gets its turn now — before the room re-signal
					// below, which is what used to take the peer back out of
					// stable underneath the deferred renegotiation.
					if pendingRenegotiate {
						h.debugLog("🔄 Executing deferred renegotiation for %s", clientID)
						pendingRenegotiate = h.renegotiateOrDefer(peerConnection, conn, clientID, roomID)
					}
					// Distribute any tracks that arrived while this peer was in
					// have-local-offer.
					go h.coordinator.SignalPeerConnectionsInRoom(roomID)
					return nil
				case types.EventRenegotiate:
					pendingRenegotiate = h.renegotiateOrDefer(peerConnection, conn, clientID, roomID)
					return nil
				case types.EventSetLayer:
					return h.handleSetLayer(message.Data, clientID, roomID)
				case types.EventStillHere:
					now := time.Now()
					if now.Sub(lastStillHere) < stillHereMinInterval {
						h.debugLog("🙋 Ignoring still_here from %s: %s since the last one", clientID, now.Sub(lastStillHere).Round(time.Millisecond))
						return nil
					}
					lastStillHere = now
					if h.roomManager.StillHere(roomID) {
						h.debugLog("🙋 %s says they are still in call '%s'", clientID, roomID)
					}
					return nil
				case types.EventKeepAlive:
					if h.config.Debug {
						h.debugLog("💓 Keep-alive received from %s", clientID)
					}
					return nil
				default:
					h.debugLog("❓ Unknown message event from %s: %s", clientID, message.Event)
					return nil
				}
			})

			if err != nil {
				h.debugLog("❌ Error processing message from %s: %v", clientID, err)
			}
		}

		h.debugLog("📨 Message handling ended for client %s (Total messages: %d)", clientID, messageCount)
		return nil
	})
}

// handleICECandidate processes ICE candidate messages
func (h *Handler) handleICECandidate(peerConnection *webrtc.PeerConnection, data, clientID string) error {
	candidate := webrtc.ICECandidateInit{}
	if err := recovery.SafeJSONUnmarshal([]byte(data), &candidate); err != nil {
		h.debugLog("❌ Error unmarshalling ICE candidate from %s: %v", clientID, err)
		return err
	}

	h.debugLog("🔧 Adding ICE candidate from %s", clientID)
	if err := peerConnection.AddICECandidate(candidate); err != nil {
		h.debugLog("❌ Error adding ICE candidate from %s: %v", clientID, err)
		return err
	}
	return nil
}

// handleAnswer applies an answer, leaving the peer's signaling state stable.
// The caller re-signals the room afterwards, once anything the peer is owed
// has had its turn.
func (h *Handler) handleAnswer(peerConnection *webrtc.PeerConnection, data, clientID string) error {
	answer := webrtc.SessionDescription{}
	if err := recovery.SafeJSONUnmarshal([]byte(data), &answer); err != nil {
		h.debugLog("❌ Error unmarshalling answer from %s: %v", clientID, err)
		return err
	}

	h.debugLog("🔄 Setting remote description (answer) from %s", clientID)
	if err := peerConnection.SetRemoteDescription(answer); err != nil {
		h.debugLog("❌ Error setting remote description from %s: %v", clientID, err)
		return err
	}

	return nil
}

// renegotiateOrDefer renegotiates now if the peer can, and otherwise says the
// peer is still owed one. **The request is only served once an offer has
// actually gone out**, which is what the return value carries.
//
// A client adding a camera track cannot publish it until the SFU offers again,
// and it only asks once — so dropping the request left the track in the
// publisher's local preview and nowhere else, with nothing retrying and nothing
// erroring (GRYT-32).
//
// Deferring is safe: the peer is only non-stable because the SFU has an offer
// outstanding to it, so an answer is already on its way.
func (h *Handler) renegotiateOrDefer(peerConnection *webrtc.PeerConnection, conn *ThreadSafeWriter, clientID, roomID string) (stillPending bool) {
	if peerConnection.SignalingState() != webrtc.SignalingStateStable {
		h.debugLog("⏳ Deferring renegotiate for %s: signaling state=%s (retried after next answer)", clientID, peerConnection.SignalingState().String())
		return true
	}

	if err := h.handleRenegotiate(peerConnection, conn, clientID, roomID); err != nil {
		h.debugLog("❌ Renegotiation for %s failed, keeping it pending: %v", clientID, err)
		return true
	}

	return false
}

// handleRenegotiate creates a fresh offer so the client can include newly
// added tracks (camera, screen share) in its answer. Callers go through
// renegotiateOrDefer so that a refused or failed attempt is not forgotten.
func (h *Handler) handleRenegotiate(peerConnection *webrtc.PeerConnection, conn *ThreadSafeWriter, clientID, roomID string) error {
	if peerConnection.SignalingState() != webrtc.SignalingStateStable {
		return fmt.Errorf("peer %s not stable (%s)", clientID, peerConnection.SignalingState().String())
	}

	h.debugLog("🔄 Renegotiation requested by client %s in room '%s'", clientID, roomID)

	offer, err := peerConnection.CreateOffer(nil)
	if err != nil {
		h.debugLog("❌ Error creating renegotiation offer for %s: %v", clientID, err)
		return err
	}

	if err := peerConnection.SetLocalDescription(offer); err != nil {
		h.debugLog("❌ Error setting local description for renegotiation (%s): %v", clientID, err)
		return err
	}

	offerJSON, marshalErr := recovery.SafeJSONMarshal(offer)
	if marshalErr != nil {
		h.debugLog("❌ Error marshalling renegotiation offer for %s: %v", clientID, marshalErr)
		return marshalErr
	}

	h.debugLog("📤 Sending renegotiation offer to %s (%d bytes SDP)", clientID, len(offer.SDP))
	return conn.WriteJSON(&types.WebSocketMessage{
		Event: types.EventOffer,
		Data:  string(offerJSON),
	})
}

// handleSetLayer processes a set_layer message to manually set the max temporal
// layer a receiver subscribes to for a given track.
func (h *Handler) handleSetLayer(data, clientID, roomID string) error {
	var req types.SetLayerData
	if err := recovery.SafeJSONUnmarshal([]byte(data), &req); err != nil {
		h.debugLog("❌ Error unmarshalling set_layer data from %s: %v", clientID, err)
		return nil
	}

	lf, ok := h.trackManager.GetForwarder(roomID, req.TrackID)
	if !ok {
		h.debugLog("⚠️ set_layer: no forwarder for track %s in room %s", req.TrackID, roomID)
		return nil
	}

	lf.SetMaxTemporalLayer(clientID, req.MaxTemporalLayer)
	h.debugLog("📊 set_layer: client %s track %s → maxTemporal=%d", clientID, req.TrackID, req.MaxTemporalLayer)
	return nil
}

// sendErrorToConnection sends an error message to a WebSocket connection
func (h *Handler) sendErrorToConnection(conn *ThreadSafeWriter, errorMsg string) {
	recovery.SafeExecute("WEBSOCKET", "SEND_ERROR", func() error {
		h.debugLog("❌ Sending error: %s", errorMsg)
		return conn.WriteJSON(&types.WebSocketMessage{
			Event: types.EventRoomError,
			Data:  errorMsg,
		})
	})
}

// stillHereMinInterval is how often one connection may say it is still here.
//
// A second is far below anything a person can press and far above what a loop
// costs to ignore.
const stillHereMinInterval = time.Second

// sendRoomJoined tells a client it is in, and what this SFU's call timeout is.
// Separate from sendSuccessToConnection, which sends the same event to the
// *server* connection and is read by nothing.
//
// Neither side needs the other to move first: an older client reads the JSON as
// an opaque string, and a newer one gets a string that will not parse and keeps
// its own default.
func (h *Handler) sendRoomJoined(conn *ThreadSafeWriter, message string) {
	recovery.SafeExecute("WEBSOCKET", "SEND_ROOM_JOINED", func() error {
		payload, err := json.Marshal(types.RoomJoinedData{
			Message:                 message,
			CallAloneTimeoutSeconds: int(h.config.CallAloneTimeout / time.Second),
		})
		if err != nil {
			return err
		}

		h.debugLog("✅ Sending room_joined: %s", string(payload))
		return conn.WriteJSON(&types.WebSocketMessage{
			Event: types.EventRoomJoined,
			Data:  string(payload),
		})
	})
}

// sendSuccessToConnection sends a success message to a WebSocket connection
func (h *Handler) sendSuccessToConnection(conn *ThreadSafeWriter, successMsg string) {
	recovery.SafeExecute("WEBSOCKET", "SEND_SUCCESS", func() error {
		h.debugLog("✅ Sending success: %s", successMsg)
		return conn.WriteJSON(&types.WebSocketMessage{
			Event: types.EventRoomJoined,
			Data:  successMsg,
		})
	})
}
