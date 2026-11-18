package auth

import "testing"

func TestVideoMaxKbpsReadsTheCapability(t *testing.T) {
	cases := map[string]struct {
		caps []string
		want int
	}{
		"none":     {[]string{CapSpeak, CapShareVideo}, 0},
		"set":      {[]string{CapSpeak, "video_max_kbps:12184"}, 12184},
		"garbage":  {[]string{"video_max_kbps:lots"}, 0},
		"negative": {[]string{"video_max_kbps:-5"}, 0},
	}
	for name, c := range cases {
		if got := (Claims{Capabilities: c.caps}).VideoMaxKbps(); got != c.want {
			t.Errorf("%s: got %d, want %d", name, got, c.want)
		}
	}
}
