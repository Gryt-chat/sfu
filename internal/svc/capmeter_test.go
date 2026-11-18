package svc

import (
	"testing"
	"time"
)

// feed sends bps for d, in 100 ms packets, and reports whether the meter paused at any point.
func feed(m *capMeter, start time.Time, d time.Duration, bps, maxBps int64) (paused bool, at time.Time) {
	step := 100 * time.Millisecond
	per := int(bps / 8 / 10)
	for t := time.Duration(0); t < d; t += step {
		now := start.Add(t)
		if p, _ := m.add(now, per, maxBps); p {
			return true, now
		}
	}
	return false, start.Add(d)
}

func TestUnderTheCeilingIsNeverPaused(t *testing.T) {
	var m capMeter
	if paused, _ := feed(&m, time.Unix(0, 0), 30*time.Second, 1_400_000, 1_000_000); paused {
		t.Fatal("a track at 1.4x its ceiling was paused; the tolerance is 1.5x")
	}
}

func TestNoCeilingIsNeverPaused(t *testing.T) {
	var m capMeter
	if paused, _ := feed(&m, time.Unix(0, 0), 30*time.Second, 50_000_000, 0); paused {
		t.Fatal("a track was paused on a server with no ceiling")
	}
}

func TestWellOverForFiveSecondsPausesThenResumes(t *testing.T) {
	var m capMeter
	start := time.Unix(0, 0)
	paused, at := feed(&m, start, 20*time.Second, 5_000_000, 1_000_000)
	if !paused {
		t.Fatal("a track at 5x its ceiling for 20 s was never paused")
	}
	if at.Sub(start) < capOverFor {
		t.Fatalf("paused after %v, before the %v grace", at.Sub(start), capOverFor)
	}
	if p, _ := m.add(at.Add(capPauseFor-time.Second), 10, 1_000_000); !p {
		t.Fatal("the pause ended early")
	}
	if p, _ := m.add(at.Add(capPauseFor+time.Second), 10, 1_000_000); p {
		t.Fatal("the track stayed paused after the pause ran out")
	}
}

func TestAShortBurstIsForgiven(t *testing.T) {
	var m capMeter
	start := time.Unix(0, 0)
	if paused, _ := feed(&m, start, 3*time.Second, 5_000_000, 1_000_000); paused {
		t.Fatal("three seconds over was enough to pause")
	}
	if paused, _ := feed(&m, start.Add(3*time.Second), 10*time.Second, 500_000, 1_000_000); paused {
		t.Fatal("a track that came back under the ceiling was paused")
	}
}
