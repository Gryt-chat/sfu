package websocket

import (
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// How long a close frame gets to reach the socket.
//
// Short on purpose. Most of the time the peer is already gone and this write
// fails immediately; the rest of the time the frame is a handful of bytes. All
// this deadline does is stop a dead TCP connection holding the handler open.
const closeWriteWait = 2 * time.Second

// The most a close frame can say. The payload is capped at 125 bytes and the
// first two are the status code.
const maxCloseReason = 123

// ThreadSafeWriter wraps a WebSocket connection with a mutex to ensure safe concurrent access
type ThreadSafeWriter struct {
	*websocket.Conn
	sync.Mutex

	// How long the peer may say nothing before its read deadline fires. Zero
	// means no deadline, which is what it is until StartKeepAlive sets it, and
	// what it stays if liveness checking is switched off.
	//
	// Not guarded by the mutex above, and it does not need to be: it is
	// written once, before the connection's read loop starts, and everything
	// that reads it afterwards runs on that same reading goroutine.
	readTimeout time.Duration
}

// ReadMessage shadows the embedded gorilla method so every message that
// arrives pushes the read deadline back out.
//
// A shadow rather than a helper with its own name, because three separate read
// loops already call conn.ReadMessage() — the client loop, the server loop,
// and the join handshake that runs before a connection is anything more than
// an open socket. Threading a new name through all three leaves one of them to
// be missed, and the one that got missed would be the one holding a goroutine
// open with no deadline on it.
//
// Extending on any message and not only on pong is the more forgiving of the
// two rules. A peer that is sending is alive whether or not its WebSocket
// stack answers control frames, and the cost of being wrong in this direction
// is hanging up on somebody who was fine.
func (t *ThreadSafeWriter) ReadMessage() (int, []byte, error) {
	messageType, payload, err := t.Conn.ReadMessage()
	if err == nil {
		t.extendReadDeadline()
	}
	return messageType, payload, err
}

// armReadDeadline sets the timeout and starts the clock.
func (t *ThreadSafeWriter) armReadDeadline(timeout time.Duration) {
	t.readTimeout = timeout
	t.extendReadDeadline()
}

// extendReadDeadline gives the peer another full timeout to say something.
func (t *ThreadSafeWriter) extendReadDeadline() {
	if t.readTimeout <= 0 {
		return
	}

	_ = t.Conn.SetReadDeadline(time.Now().Add(t.readTimeout))
}

// WriteJSON writes a JSON message to the WebSocket connection in a thread-safe manner
func (t *ThreadSafeWriter) WriteJSON(v interface{}) error {
	t.Lock()
	defer t.Unlock()
	return t.Conn.WriteJSON(v)
}

// CloseWithReason says why before hanging up.
//
// `websocket.Conn.Close()` closes the TCP connection and nothing else — no
// close frame, no status code. Every peer on the other end of one of those sees
// exactly `code=1006 reason="(none)" wasClean=false`, which is also what a
// snapped network cable looks like. So an SFU that only ever calls Close()
// cannot be told apart from a broken connection by anybody, including us: a
// client that has been rejected for capacity, dropped because its room went
// away, or cut off by a deploy reports the same three fields as one whose Wi-Fi
// died. That is not a small loss. It is the whole difference between "the SFU
// dropped me" and "my network dropped", and without it the client's log cannot
// answer which happened.
//
// The write is best-effort by design. If the peer is already gone the frame
// goes nowhere and the connection still closes, which is the same outcome as
// before this existed.
func (t *ThreadSafeWriter) CloseWithReason(code int, reason string) error {
	t.Lock()
	defer t.Unlock()

	if len(reason) > maxCloseReason {
		reason = reason[:maxCloseReason]
	}

	_ = t.Conn.WriteControl(
		websocket.CloseMessage,
		websocket.FormatCloseMessage(code, reason),
		time.Now().Add(closeWriteWait),
	)

	return t.Conn.Close()
}

// NewThreadSafeWriter creates a new thread-safe WebSocket writer
func NewThreadSafeWriter(conn *websocket.Conn) *ThreadSafeWriter {
	return &ThreadSafeWriter{
		Conn:  conn,
		Mutex: sync.Mutex{},
	}
}
