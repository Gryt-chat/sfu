package websocket

import (
	"encoding/json"
	"testing"

	"sfu-v2/internal/audiotransport"
	"sfu-v2/internal/config"
	"sfu-v2/internal/room"
	"sfu-v2/pkg/types"
)

// Only the room's own server can add a member it speaks for, and they go when its connection does.
func TestVirtualVoiceAnswersOnlyToTheRoomsServer(t *testing.T) {
	rooms := room.NewManager(false)
	if err := rooms.RegisterServer("srv-a", "secret-a", "srv-a_room"); err != nil {
		t.Fatalf("register: %v", err)
	}
	audio := audiotransport.New(0, nil)
	h := &Handler{config: &config.Config{}, roomManager: rooms, audio: audio}
	set := &virtualSet{}
	join := func(password string) {
		data, _ := json.Marshal(types.VirtualVoiceData{RoomID: "srv-a_room", UserID: "discord:7", ServerID: "srv-a", ServerPassword: password})
		if err := h.handleVirtualJoin(nil, set, string(data)); err != nil {
			t.Fatalf("handleVirtualJoin: %v", err)
		}
	}

	join("wrong")
	if len(audio.Senders("srv-a_room")) != 0 {
		t.Fatal("a server without the room's credentials added a member")
	}

	join("secret-a")
	if _, ok := audio.Senders("srv-a_room")["virtual:discord:7"]; !ok {
		t.Fatal("the room's server couldn't add a member")
	}
	if user, ok := h.live.userOf("virtual:discord:7"); !ok || user != "discord:7" {
		t.Fatalf("the sender map won't name the member: %q %v", user, ok)
	}

	h.handleVirtualFrame(set, frame(frameToRoom, "virtual:discord:7", []byte{1, 0, 0, 0, 0, 0, 0, 0, 9}))
	h.handleVirtualFrame(set, []byte{frameToRoom, 200, 1})

	h.leaveAllVirtual(set)
	if len(audio.Senders("srv-a_room")) != 0 {
		t.Fatal("the member stayed after the server's connection went")
	}
	if _, ok := h.live.userOf("virtual:discord:7"); ok {
		t.Fatal("the member stayed in the sender map")
	}
}
