package svc

import "time"

// A video track sent well over its server's ceiling is a client that skipped the clamp (GRYT-2171).
const (
	capTolerance = 1.5
	capOverFor   = 5 * time.Second
	capPauseFor  = 30 * time.Second
)

// capMeter measures one track's incoming bitrate a second at a time and says when to pause it.
type capMeter struct {
	windowStart time.Time
	windowBytes int
	overSince   time.Time
	pausedUntil time.Time
}

// add counts n bytes arriving at now. It reports whether the track is paused, and whether that
// pause starts with this packet.
func (m *capMeter) add(now time.Time, n int, maxBps int64) (paused, started bool) {
	if now.Before(m.pausedUntil) {
		return true, false
	}
	if m.windowStart.IsZero() {
		m.windowStart = now
	}
	m.windowBytes += n
	elapsed := now.Sub(m.windowStart)
	if elapsed < time.Second {
		return false, false
	}
	rate := float64(m.windowBytes*8) / elapsed.Seconds()
	m.windowStart, m.windowBytes = now, 0
	if maxBps <= 0 || rate <= float64(maxBps)*capTolerance {
		m.overSince = time.Time{}
		return false, false
	}
	if m.overSince.IsZero() {
		m.overSince = now
	}
	if now.Sub(m.overSince) < capOverFor {
		return false, false
	}
	m.overSince = time.Time{}
	m.pausedUntil = now.Add(capPauseFor)
	return true, true
}
