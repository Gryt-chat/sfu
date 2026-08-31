// Package auth verifies that a client connecting to the SFU was actually sent
// here by the server that owns the room.
//
// Before this existed the SFU checked the server's shared password, which the
// server handed to every browser so the browser could present it. Anyone who
// joined a voice channel once therefore held the credential, and could open a
// socket to the SFU directly, claim any user id, and enter any room. The
// server's own access checks were bypassed by not asking the server.
//
// A client now carries a token the server signed and the client cannot forge.
// The key is the same shared secret the server registers with, so there is no
// new key to distribute; what changes is that the secret stays between the two
// services instead of travelling through the browser.
package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// TokenVersion prefixes every token so the format can change later without
// having to guess what an unprefixed string was meant to be.
const TokenVersion = "v1"

var (
	ErrMalformed = errors.New("client token is malformed")
	ErrSignature = errors.New("client token signature does not verify")
	ErrExpired   = errors.New("client token has expired")
	ErrMismatch  = errors.New("client token is for a different user or room")
)

var enc = base64.RawURLEncoding

// Sign produces a token binding a user to a room until expiresAt.
//
// The room and user are inside the signed payload rather than alongside it, so
// a token minted for one room cannot be replayed into another: the SFU compares
// what the payload says against what the client asked for.
func Sign(secret, userID, roomID, nonce string, expiresAt time.Time) string {
	payload := fmt.Sprintf("%s|%s|%d|%s", userID, roomID, expiresAt.UnixMilli(), nonce)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	return TokenVersion + "." + enc.EncodeToString([]byte(payload)) + "." + enc.EncodeToString(mac.Sum(nil))
}

// Verify checks the signature, the expiry, and that the token was issued for
// this exact user and room.
//
// Order matters: the signature is checked before anything is believed, so the
// payload's own claims are never acted on until they are known to be ours.
func Verify(secret, token, roomID, userID string, now time.Time) error {
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[0] != TokenVersion {
		return ErrMalformed
	}
	payload, err := enc.DecodeString(parts[1])
	if err != nil {
		return ErrMalformed
	}
	sig, err := enc.DecodeString(parts[2])
	if err != nil {
		return ErrMalformed
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return ErrSignature
	}

	fields := strings.Split(string(payload), "|")
	if len(fields) != 4 {
		return ErrMalformed
	}
	gotUser, gotRoom, expiryRaw := fields[0], fields[1], fields[2]

	expiryMs, err := strconv.ParseInt(expiryRaw, 10, 64)
	if err != nil {
		return ErrMalformed
	}
	if now.After(time.UnixMilli(expiryMs)) {
		return ErrExpired
	}

	// Compared in constant time out of habit rather than necessity: these are
	// not secrets, but they are attacker-supplied and it costs nothing.
	if !hmac.Equal([]byte(gotUser), []byte(userID)) || !hmac.Equal([]byte(gotRoom), []byte(roomID)) {
		return ErrMismatch
	}
	return nil
}
