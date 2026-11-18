package auth

import (
	"strconv"
	"strings"
)

// CapVideoMaxKbpsPrefix carries a Gryt Cloud plan's video ceiling as a bitrate, "video_max_kbps:12184".
const CapVideoMaxKbpsPrefix = "video_max_kbps:"

// VideoMaxKbps is the token's video ceiling, or 0 for none.
func (c Claims) VideoMaxKbps() int {
	for _, capability := range c.Capabilities {
		if raw, ok := strings.CutPrefix(capability, CapVideoMaxKbpsPrefix); ok {
			if n, err := strconv.Atoi(raw); err == nil && n > 0 {
				return n
			}
		}
	}
	return 0
}
