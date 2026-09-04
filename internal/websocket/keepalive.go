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

// KeepAlive pings a connection on a timer and arms a read deadline on it. Two
// problems, one mechanism:
//
//   - A peer that vanished without a FIN leaves a socket that reads forever.
//     Nothing arrives and nothing errors, so the SFU keeps that peer in its
//     room and in the count the capacity guardrail reads, and never runs the
//     cleanup that would tell everybody else it left.
//
//   - Nothing travels server to client while a call is quiet, so every NAT
//     between the two eventually reaps the mapping. The peer finds out on its
//     next write, as a reset rather than a close.
//
// **A server-side ping works where the client's own keep-alive cannot.** The
// client's `keep_alive` is a JSON message on a timer, and a browser throttles a
// backgrounded tab's timers to roughly once a minute — the case where the
// connection most needs traffic is the one where the client can least produce
// it. A ping frame is answered below the JS layer, and the same is true of the
// `ws` library and React Native's socket.
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
