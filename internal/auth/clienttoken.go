// Package auth verifies that a client connecting to the SFU was actually sent
// here by the server that owns the room.
//
// The SFU used to check the server's shared password, which the server handed
// to every browser — so anyone who joined a voice channel once could open a
// socket directly, claim any user id and enter any room, bypassing the server's
// access checks by not asking the server.
//
// The token is signed with that same shared secret, so there is no new key to
// distribute; what changes is that it stays between the two services.
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

// Token versions prefix every token so the format can change without guessing
// what an unprefixed string meant. v2 adds a capability list.
//
// **v1 is still accepted and means every capability.** The SFU and the server
// deploy separately, so one is always older — and reading v1 as "may not speak"
// would silently mute an entire server the first time this SFU shipped ahead of
// it. Failing open is the safe direction: the gate is a permission the server
// has not asked for yet.
const (
	TokenVersion  = "v1"
	TokenVersion2 = "v2"
)

// CapSpeak is the capability to publish microphone audio.
//
// Its absence is what `speak` denied on a channel scope comes to mean here. It
// gates the microphone only — screen-share audio is a different capability on
// the server (`share_screen`) and arrives on a different transceiver.
const CapSpeak = "speak"

var (
	ErrMalformed = errors.New("client token is malformed")
	ErrSignature = errors.New("client token signature does not verify")
	ErrExpired   = errors.New("client token has expired")
	ErrMismatch  = errors.New("client token is for a different user or room")
)

var enc = base64.RawURLEncoding

// Claims is what a verified token says the bearer may do.
type Claims struct {
	// Capabilities the server granted. Nil or empty means none were granted,
	// which is only reachable from a v2 token — see Verify.
	Capabilities []string
}

// Can reports whether the token granted a capability.
func (c Claims) Can(capability string) bool {
	for _, got := range c.Capabilities {
		if got == capability {
			return true
		}
	}
	return false
}

// Sign produces a v1 token binding a user to a room until expiresAt.
//
// The room and user are inside the signed payload rather than alongside it, so
// a token minted for one room cannot be replayed into another: the SFU compares
// what the payload says against what the client asked for.
func Sign(secret, userID, roomID, nonce string, expiresAt time.Time) string {
	payload := fmt.Sprintf("%s|%s|%d|%s", userID, roomID, expiresAt.UnixMilli(), nonce)
	return TokenVersion + "." + sealed(secret, payload)
}

// SignV2 produces a token that also says what the bearer may do.
//
// The capability list is inside the signed payload, so a client cannot add
// `speak` to a token that was minted without it. That is the whole point: the
// client is the thing being restricted, so nothing it can edit may be believed.
func SignV2(secret, userID, roomID, nonce string, expiresAt time.Time, capabilities []string) string {
	payload := fmt.Sprintf("%s|%s|%d|%s|%s", userID, roomID, expiresAt.UnixMilli(), nonce, strings.Join(capabilities, ","))
	return TokenVersion2 + "." + sealed(secret, payload)
}

func sealed(secret, payload string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	return enc.EncodeToString([]byte(payload)) + "." + enc.EncodeToString(mac.Sum(nil))
}

// Verify checks the signature, the expiry, and that the token was issued for
// this exact user and room, and reports what the bearer may do.
//
// Order matters: the signature is checked before anything is believed, so the
// payload's own claims are never acted on until they are known to be ours.
//
// **A v1 token grants every capability.** It carries none, and the alternative
// reading — that it grants nothing — would mute every client of any server not
// yet minting v2, with no error and no sound. See the comment on TokenVersion.
func Verify(secret, token, roomID, userID string, now time.Time) (Claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return Claims{}, ErrMalformed
	}
	version := parts[0]
	if version != TokenVersion && version != TokenVersion2 {
		return Claims{}, ErrMalformed
	}
	payload, err := enc.DecodeString(parts[1])
	if err != nil {
		return Claims{}, ErrMalformed
	}
	sig, err := enc.DecodeString(parts[2])
	if err != nil {
		return Claims{}, ErrMalformed
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return Claims{}, ErrSignature
	}

	fields := strings.Split(string(payload), "|")
	// v1 is four fields, v2 adds the capability list. An exact count per
	// version rather than a minimum, so a token with trailing rubbish is
	// rejected instead of being read as far as it parses.
	wantFields := 4
	if version == TokenVersion2 {
		wantFields = 5
	}
	if len(fields) != wantFields {
		return Claims{}, ErrMalformed
	}
	gotUser, gotRoom, expiryRaw := fields[0], fields[1], fields[2]

	expiryMs, err := strconv.ParseInt(expiryRaw, 10, 64)
	if err != nil {
		return Claims{}, ErrMalformed
	}
	if now.After(time.UnixMilli(expiryMs)) {
		return Claims{}, ErrExpired
	}

	// Compared in constant time out of habit rather than necessity: these are
	// not secrets, but they are attacker-supplied and it costs nothing.
	if !hmac.Equal([]byte(gotUser), []byte(userID)) || !hmac.Equal([]byte(gotRoom), []byte(roomID)) {
		return Claims{}, ErrMismatch
	}

	if version == TokenVersion {
		return Claims{Capabilities: []string{CapSpeak}}, nil
	}
	return Claims{Capabilities: splitCaps(fields[4])}, nil
}

// splitCaps turns the capability field into a list, without the single empty
// string strings.Split hands back for an empty input.
func splitCaps(raw string) []string {
	if raw == "" {
		return nil
	}
	return strings.Split(raw, ",")
}
