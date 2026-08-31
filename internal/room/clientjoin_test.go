package room

import (
	"strings"
	"testing"
	"time"

	"sfu-v2/internal/auth"
)

const (
	testServer   = "server-1"
	testPassword = "server-1-shared-secret"
	testRoom     = "room-1"
	testUser     = "user-1"
)

func managerWithServer(t *testing.T) *Manager {
	t.Helper()
	m := NewManager(false)
	if err := m.RegisterServer(testServer, testPassword, testRoom); err != nil {
		t.Fatalf("RegisterServer: %v", err)
	}
	return m
}

func signed(userID, roomID string) string {
	return auth.Sign(testPassword, userID, roomID, "n", time.Now().Add(time.Minute))
}

func TestJoinWithASignedTokenIsAccepted(t *testing.T) {
	m := managerWithServer(t)
	if err := m.ValidateClientJoin(testRoom, testServer, "", signed(testUser, testRoom), testUser); err != nil {
		t.Fatalf("ValidateClientJoin = %v, want nil", err)
	}
}

// The point of the change. Holding the shared password used to be enough to
// enter any room, and every browser was handed it.
func TestJoinWithAStolenPasswordAndABadTokenIsRefused(t *testing.T) {
	m := managerWithServer(t)
	forged := auth.Sign("not-the-secret", testUser, testRoom, "n", time.Now().Add(time.Minute))

	err := m.ValidateClientJoin(testRoom, testServer, testPassword, forged, testUser)
	if err == nil {
		t.Fatal("ValidateClientJoin = nil, want a refusal: a presented token must be verified, not fallen back on")
	}
	if !strings.Contains(err.Error(), "client token rejected") {
		t.Fatalf("ValidateClientJoin = %v, want the token rejection", err)
	}
}

// A token for one room must not open another, even signed by the right server.
func TestATokenForAnotherRoomDoesNotOpenThisOne(t *testing.T) {
	m := managerWithServer(t)
	if err := m.ValidateClientJoin(testRoom, testServer, "", signed(testUser, "some-other-room"), testUser); err == nil {
		t.Fatal("ValidateClientJoin = nil, want a refusal for a token minted for a different room")
	}
}

// Deployable before the servers that mint tokens; removable once none rely on it.
func TestJoinWithoutATokenStillFallsBackToThePassword(t *testing.T) {
	m := managerWithServer(t)
	if err := m.ValidateClientJoin(testRoom, testServer, testPassword, "", testUser); err != nil {
		t.Fatalf("ValidateClientJoin = %v, want nil for the legacy path", err)
	}
}

func TestJoinWithNeitherIsRefused(t *testing.T) {
	m := managerWithServer(t)
	if err := m.ValidateClientJoin(testRoom, testServer, "wrong-password", "", testUser); err == nil {
		t.Fatal("ValidateClientJoin = nil, want a refusal when neither a token nor the password is valid")
	}
}

func TestJoinAgainstAnUnregisteredServerIsRefused(t *testing.T) {
	m := managerWithServer(t)
	if err := m.ValidateClientJoin(testRoom, "server-nobody-registered", "", signed(testUser, testRoom), testUser); err == nil {
		t.Fatal("ValidateClientJoin = nil, want a refusal for an unregistered server")
	}
}

// With the flag on, knowing the old shared password buys nothing. Until it is
// on, it still does — which is why the flag exists rather than a comment saying
// to remove the fallback later.
func TestRequireClientTokenClosesTheLegacyPath(t *testing.T) {
	m := managerWithServer(t)
	m.SetRequireClientToken(true)

	if err := m.ValidateClientJoin(testRoom, testServer, testPassword, "", testUser); err == nil {
		t.Fatal("ValidateClientJoin = nil, want a refusal once client tokens are required")
	}
	if err := m.ValidateClientJoin(testRoom, testServer, "", signed(testUser, testRoom), testUser); err != nil {
		t.Fatalf("ValidateClientJoin = %v, want nil: a signed token must still work", err)
	}
}
