package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

func value(t *testing.T, c prometheus.Counter) float64 {
	t.Helper()
	var m dto.Metric
	if err := c.Write(&m); err != nil {
		t.Fatal(err)
	}
	return m.GetCounter().GetValue()
}

func TestBytesLandOnTheRoomsServer(t *testing.T) {
	SetServerResolver(func(roomID string) (string, bool) {
		if roomID == "room-a" {
			return "thomas_5000_thomas", true
		}
		return "", false
	})

	ForRoom("room-a").Out.Add(1000)
	ForRoom("room-a").In.Add(10)
	ForRoom("room-b").Out.Add(7)

	if got := value(t, egressBytes.WithLabelValues("thomas_5000_thomas")); got != 1000 {
		t.Fatalf("egress for thomas = %v, want 1000", got)
	}
	if got := value(t, ingressBytes.WithLabelValues("thomas_5000_thomas")); got != 10 {
		t.Fatalf("ingress for thomas = %v, want 10", got)
	}
	// A room nobody owns yet isn't cached, so it can still be counted right once it is.
	if got := value(t, egressBytes.WithLabelValues("unknown")); got != 7 {
		t.Fatalf("egress for an unknown room = %v, want 7", got)
	}
	if _, cached := byRoom.Load("room-b"); cached {
		t.Fatal("an unresolved room was cached")
	}
}
