package websocket

import (
	"encoding/json"
	"testing"
	"time"

	"sfu-v2/internal/config"
	"sfu-v2/pkg/types"
)

/*
What a client is told when it joins (GRYT-715).

The client draws a countdown before a call it is alone in ends, and until this
message carried a number it had its own copy of the SFU's default. That copy
was right until an operator changed theirs. These check the number crosses the
wire and survives the units.
*/

func TestRoomJoinedCarriesTheCallAloneTimeout(t *testing.T) {
	h := &Handler{config: &config.Config{CallAloneTimeout: 5 * time.Minute}}
	serverConn, clientConn := newTestSocketPair(t)

	h.sendRoomJoined(serverConn, "Successfully joined room")

	message, ok := readNextMessage(t, clientConn, time.Second)
	if !ok {
		t.Fatal("no room_joined arrived")
	}
	if message.Event != types.EventRoomJoined {
		t.Fatalf("expected %s, got %s", types.EventRoomJoined, message.Event)
	}

	var joined types.RoomJoinedData
	if err := json.Unmarshal([]byte(message.Data), &joined); err != nil {
		t.Fatalf("room_joined data is not the JSON a client parses: %v (%q)", err, message.Data)
	}
	if joined.CallAloneTimeoutSeconds != 300 {
		t.Fatalf("five minutes should reach the client as 300 seconds, got %d", joined.CallAloneTimeoutSeconds)
	}
	if joined.Message != "Successfully joined room" {
		t.Fatalf("the message a client used to read the whole payload as is gone: %q", joined.Message)
	}
}

func TestRoomJoinedSaysZeroWhenTheSweepIsOff(t *testing.T) {
	h := &Handler{config: &config.Config{CallAloneTimeout: 0}}
	serverConn, clientConn := newTestSocketPair(t)

	h.sendRoomJoined(serverConn, "Successfully joined room")

	message, ok := readNextMessage(t, clientConn, time.Second)
	if !ok {
		t.Fatal("no room_joined arrived")
	}

	var joined types.RoomJoinedData
	if err := json.Unmarshal([]byte(message.Data), &joined); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// The disagreement that matters. An operator who turns the sweep off and
	// gets a client still hanging up after two minutes has a call ending for
	// no reason anybody can see.
	if joined.CallAloneTimeoutSeconds != 0 {
		t.Fatalf("SFU_CALL_ALONE_TIMEOUT=0 has to reach the client as 0, got %d", joined.CallAloneTimeoutSeconds)
	}
}
