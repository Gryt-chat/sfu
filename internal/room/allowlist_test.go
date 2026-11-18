package room

import (
	"os"
	"path/filepath"
	"testing"
)

// GRYT-2165. Several tenants share one SFU in Gryt Cloud, and registration is first come
// per server ID, so a broken-into tenant could otherwise claim a neighbour's ID.
func writeAllowlist(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "servers")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestAllowlistLetsOnlyListedServersIn(t *testing.T) {
	m := NewManager(false)
	m.SetServerAllowlist(writeAllowlist(t, "# tenants\nthomas_5000_thomas secret-thomas\nanna_5000_anna secret-anna\n"))

	if err := m.RegisterServer("thomas_5000_thomas", "secret-thomas", "room-1"); err != nil {
		t.Fatalf("a listed server was refused: %v", err)
	}
	if err := m.RegisterServer("mallory_5000_x", "anything", "room-2"); err == nil {
		t.Fatal("a server missing from the list registered")
	}
	// The attack: claim a neighbour's ID before it starts, with a secret of your own.
	if err := m.RegisterServer("anna_5000_anna", "secret-of-the-attacker", "room-3"); err == nil {
		t.Fatal("a listed ID registered with the wrong secret")
	}
	if _, exists := m.registeredServers["anna_5000_anna"]; exists {
		t.Fatal("the refused registration was remembered")
	}
	if err := m.RegisterServer("anna_5000_anna", "secret-anna", "room-3"); err != nil {
		t.Fatalf("the real owner of the ID was locked out: %v", err)
	}
}

func TestAllowlistIsReadAtEachRegistration(t *testing.T) {
	m := NewManager(false)
	path := writeAllowlist(t, "")
	m.SetServerAllowlist(path)

	if err := m.RegisterServer("new_5000_new", "s", "room-1"); err == nil {
		t.Fatal("registered before it was listed")
	}
	if err := os.WriteFile(path, []byte("new_5000_new s\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := m.RegisterServer("new_5000_new", "s", "room-1"); err != nil {
		t.Fatalf("a server added to the file needed a restart: %v", err)
	}
}

func TestAMissingAllowlistRefusesEveryone(t *testing.T) {
	m := NewManager(false)
	m.SetServerAllowlist(filepath.Join(t.TempDir(), "gone"))
	if err := m.RegisterServer("srv-1", "s", "room-1"); err == nil {
		t.Fatal("an unreadable allowlist let a server in")
	}
}

func TestNoAllowlistKeepsTodaysBehaviour(t *testing.T) {
	m := NewManager(false)
	if err := m.RegisterServer("anyone", "a-secret", "room-1"); err != nil {
		t.Fatalf("self-hosting without a list broke: %v", err)
	}
}
