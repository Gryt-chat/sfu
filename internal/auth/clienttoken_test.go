package auth

import (
	"errors"
	"strings"
	"testing"
	"time"
)

const (
	secret = "shared-server-secret"
	user   = "user-abc"
	room   = "room-xyz"
)

func validToken(t *testing.T) string {
	t.Helper()
	return Sign(secret, user, room, "nonce-1", time.Now().Add(time.Minute))
}

func TestARoundTripVerifies(t *testing.T) {
	if err := Verify(secret, validToken(t), room, user, time.Now()); err != nil {
		t.Fatalf("verify = %v, want nil", err)
	}
}

// The whole point: the token is only worth anything to somebody holding the
// shared secret, which the browser no longer does.
func TestAnotherSecretDoesNotVerify(t *testing.T) {
	if err := Verify("some-other-secret", validToken(t), room, user, time.Now()); !errors.Is(err, ErrSignature) {
		t.Fatalf("verify = %v, want ErrSignature", err)
	}
}

// A token is a statement about one user in one room. Without this check the
// signature alone would let anybody with a valid token of their own walk into
// any room, which is the hole this replaces rather than a new one.
func TestATokenForAnotherRoomIsRefused(t *testing.T) {
	if err := Verify(secret, validToken(t), "some-other-room", user, time.Now()); !errors.Is(err, ErrMismatch) {
		t.Fatalf("verify = %v, want ErrMismatch", err)
	}
}

func TestATokenForAnotherUserIsRefused(t *testing.T) {
	if err := Verify(secret, validToken(t), room, "someone-else", time.Now()); !errors.Is(err, ErrMismatch) {
		t.Fatalf("verify = %v, want ErrMismatch", err)
	}
}

func TestAnExpiredTokenIsRefused(t *testing.T) {
	tok := Sign(secret, user, room, "nonce-1", time.Now().Add(-time.Second))
	if err := Verify(secret, tok, room, user, time.Now()); !errors.Is(err, ErrExpired) {
		t.Fatalf("verify = %v, want ErrExpired", err)
	}
}

// Editing the payload invalidates the signature, so the claims inside it cannot
// be rewritten by whoever is holding the token.
func TestATamperedPayloadIsRefused(t *testing.T) {
	tok := validToken(t)
	parts := strings.Split(tok, ".")
	forged := Sign(secret, "attacker", room, "nonce-1", time.Now().Add(time.Minute))
	tampered := parts[0] + "." + strings.Split(forged, ".")[1] + "." + parts[2]

	if err := Verify(secret, tampered, room, "attacker", time.Now()); !errors.Is(err, ErrSignature) {
		t.Fatalf("verify = %v, want ErrSignature", err)
	}
}

func TestRubbishIsRefusedRatherThanPanicking(t *testing.T) {
	for _, tok := range []string{
		"", ".", "..", "v1", "v1.", "v1.a", "v1.a.b.c",
		"v2." + strings.Split(validToken(t), ".")[1] + ".sig",
		"v1.!!!not-base64!!!.also-not",
	} {
		if err := Verify(secret, tok, room, user, time.Now()); err == nil {
			t.Fatalf("verify(%q) = nil, want an error", tok)
		}
	}
}

// An empty secret must not become a universal key: a token signed with one is
// still only valid against that same empty secret, never against a real one.
func TestAnEmptySecretIsNotAMasterKey(t *testing.T) {
	tok := Sign("", user, room, "nonce-1", time.Now().Add(time.Minute))
	if err := Verify(secret, tok, room, user, time.Now()); !errors.Is(err, ErrSignature) {
		t.Fatalf("verify = %v, want ErrSignature", err)
	}
}
