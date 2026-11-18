package room

import (
	"bufio"
	"crypto/subtle"
	"fmt"
	"os"
	"strings"
)

// SetServerAllowlist limits registration to the server IDs and secrets in path, one
// "id secret" per line. Read on every registration, so adding a server needs no restart.
func (m *Manager) SetServerAllowlist(path string) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.allowlistPath = path
}

// allowed is nil when path holds serverID with exactly this secret.
func allowed(path, serverID, secret string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("the server allowlist can't be read: %w", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 || fields[0] != serverID {
			continue
		}
		if subtle.ConstantTimeCompare([]byte(fields[1]), []byte(secret)) == 1 {
			return nil
		}
		return fmt.Errorf("server %s is listed with a different secret", serverID)
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("the server allowlist can't be read: %w", err)
	}
	return fmt.Errorf("server %s is not on this SFU's allowlist", serverID)
}
