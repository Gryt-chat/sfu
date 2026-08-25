package websocket

import (
	"time"

	"github.com/gorilla/websocket"
)

// How long a ping frame gets to reach the socket.
//
// Short for the same reason closeWriteWait is: the frame is two bytes, so
// either it goes now or the socket is already in trouble. Without a deadline a
// ping to a peer whose TCP window has stopped draining blocks this goroutine
// indefinitely.
const pingWriteWait = 10 * time.Second

// KeepAlive pings a connection on a timer and arms a read deadline on it, so
// that a peer which went away without saying so is noticed instead of held
// open forever.
//
// Two problems, one mechanism:
//
//   - A peer that vanished without a FIN — a closed laptop lid, LTE that
//     dropped, a process killed behind a NAT that keeps the mapping alive —
//     leaves a socket that reads forever. Nothing arrives and nothing errors.
//     The SFU keeps that peer in its room, keeps it in the peer count that the
//     capacity guardrail reads, and never runs the cleanup that would tell
//     everybody else it left.
//
//   - Nothing at all travels server to client while a call is quiet.
//     Signalling goes silent once the tracks are up, so every NAT and stateful
//     firewall between the two sees an idle TCP connection and eventually
//     reaps the mapping. The peer discovers this on its next write, which may
//     be minutes later, and it arrives as a reset rather than as a close.
//
// A server-side ping answers both, and it works in a place the client's own
// keep-alive does not. The `keep_alive` message the client sends is a JSON
// message on a timer, and a browser throttles the timers of a backgrounded tab
// to roughly once a minute — so the one case where the connection most needs
// traffic is the case where the client is least able to produce it. A ping
// frame is answered by the browser's WebSocket implementation below the JS
// layer, on a tab that is doing nothing at all. Same for the `ws` library the
// Gryt server uses on its own connection here, and for React Native's socket
// on mobile. None of them needed a change for this.
type KeepAlive struct {
	stop chan struct{}
}

// StartKeepAlive arms conn's read deadline and starts pinging every interval.
// Stop belongs in a defer next to it.
//
// An interval of zero turns the whole thing off, read deadline included. That
// is deliberate and it is the escape hatch: if this ever starts hanging up on
// people who were fine, the fix is SFU_PING_INTERVAL=0 and a restart rather
// than a release.
func StartKeepAlive(conn *ThreadSafeWriter, interval, timeout time.Duration) *KeepAlive {
	if interval <= 0 || timeout <= 0 {
		return &KeepAlive{}
	}

	// Arm before the first read rather than at the first tick. The read that
	// happens before any ping has gone out is the join handshake, and a socket
	// that connects and then says nothing is exactly the case that used to
	// hold a goroutine open with no room, no peer and nothing to time it out.
	conn.armReadDeadline(timeout)

	// A pong is the peer saying it is still there. Gorilla calls this handler
	// on the reading goroutine, from inside ReadMessage, so touching the
	// deadline here is no different from touching it after a message arrives.
	conn.SetPongHandler(func(string) error {
		conn.extendReadDeadline()
		return nil
	})

	k := &KeepAlive{stop: make(chan struct{})}

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-k.stop:
				return
			case <-ticker.C:
				// WriteControl is the one write here that does not take the
				// mutex. Gorilla documents it as safe to call concurrently
				// with every other method, which is what lets this sit on its
				// own goroutine while the handler writes JSON on another.
				//
				// A failed ping is dropped on the floor on purpose. The socket
				// is already broken, the read side is a deadline away from
				// finding that out, and closing from here would race the
				// handler that owns the connection and owns saying why.
				_ = conn.WriteControl(
					websocket.PingMessage,
					nil,
					time.Now().Add(pingWriteWait),
				)
			}
		}
	}()

	return k
}

// Stop ends the pinger.
//
// It does not wait for the goroutine to be gone, which means a ping can still
// be in flight while the caller closes the connection. That is fine and is why
// the ping's error is ignored: gorilla allows WriteControl concurrently with
// Close, and a write to a closed connection returns an error nobody reads.
// Waiting instead would hold the connection's handler for up to a full
// interval on a socket that is already dead — the exact stall this file exists
// to remove.
func (k *KeepAlive) Stop() {
	if k == nil || k.stop == nil {
		return
	}
	close(k.stop)
}
