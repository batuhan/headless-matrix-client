package server

import (
	"encoding/json"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.mau.fi/gomuks/pkg/hicli/database"
	"maunium.net/go/mautrix/event"

	"github.com/batuhan/easymatrix/internal/compat"
)

func TestPushRegistrationsPersist(t *testing.T) {
	stateDir := t.TempDir()
	service := &pushService{
		devices:   make(map[string]pushDevice),
		storePath: filepath.Join(stateDir, "push", "devices.json"),
	}
	device := pushDevice{
		Token:       "aabbcc",
		Platform:    "apple",
		ServerURL:   "https://relay.example.test",
		AccessToken: "server-access-key",
		UpdatedAt:   time.Now().UTC(),
	}
	if err := service.register(device); err != nil {
		t.Fatalf("register: %v", err)
	}

	reloaded := &pushService{
		devices:   make(map[string]pushDevice),
		storePath: service.storePath,
	}
	if err := reloaded.load(); err != nil {
		t.Fatalf("load: %v", err)
	}
	devices := reloaded.registeredDevices()
	if len(devices) != 1 || devices[0].Token != device.Token {
		t.Fatalf("devices = %v, want token %s", devices, device.Token)
	}
	if devices[0].ServerURL != device.ServerURL || devices[0].AccessToken != device.AccessToken {
		t.Fatalf("device credentials were not persisted")
	}

	if err := reloaded.delete(device.Token); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if len(reloaded.registeredDevices()) != 0 {
		t.Fatalf("tokens were not deleted")
	}
}

func TestGroupPushPayloadUsesThreeTextLevels(t *testing.T) {
	payload, ok := makePushPayload(compatRecord{
		"id":          "message-1",
		"chatID":      "chat-1",
		"senderID":    "@alice:example.test",
		"senderName":  "Alice",
		"chatTitle":   "Weekend Plans",
		"isGroupChat": true,
		"text":        "Dinner at seven?",
		"pushParticipants": []pushParticipant{
			{ID: "@bob:example.test", Name: "Bob"},
		},
	})
	if !ok {
		t.Fatal("makePushPayload() skipped an incoming group message")
	}

	aps := payload["aps"].(map[string]any)
	alert := aps["alert"].(map[string]string)
	if alert["title"] != "Weekend Plans" {
		t.Fatalf("title = %q, want group title", alert["title"])
	}
	if alert["subtitle"] != "Alice" {
		t.Fatalf("subtitle = %q, want sender name", alert["subtitle"])
	}
	if alert["body"] != "Dinner at seven?" {
		t.Fatalf("body = %q, want message body", alert["body"])
	}
	if aps["mutable-content"] != 1 {
		t.Fatalf("mutable-content = %v, want 1", aps["mutable-content"])
	}
	if payload["senderID"] != "@alice:example.test" {
		t.Fatalf("senderID = %v, want @alice:example.test", payload["senderID"])
	}
	participants, ok := payload["groupParticipants"].([]pushParticipant)
	if !ok || len(participants) != 1 || participants[0].ID != "@bob:example.test" {
		t.Fatalf("groupParticipants = %#v, want Bob", payload["groupParticipants"])
	}
}

func TestPushGroupParticipantsExcludesSelfAndSenderAndCapsPayload(t *testing.T) {
	participants := []compat.User{
		{ID: "@self:example.test", FullName: "Me", IsSelf: true},
		{ID: "@sender:example.test", FullName: "Sender"},
	}
	for _, participantID := range []string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "10"} {
		participants = append(participants, compat.User{
			ID:       participantID,
			FullName: "Member " + participantID,
		})
	}

	output := pushGroupParticipants(
		participants,
		compatRecord{"senderID": "@sender:example.test"},
		true,
	)

	if len(output) != maxPushParticipants {
		t.Fatalf("participant count = %d, want %d", len(output), maxPushParticipants)
	}
	if output[0].ID != "1" || output[len(output)-1].ID != "8" {
		t.Fatalf("participants = %#v, want first eight non-self recipients", output)
	}
	if got := pushGroupParticipants(participants, nil, false); got != nil {
		t.Fatalf("direct-chat participants = %#v, want nil", got)
	}
}

func TestPushAvatarURLUsesAssetSpecificSignature(t *testing.T) {
	entry := compatRecord{"pushAvatarURL": "mxc://example.test/avatar"}
	payload := map[string]any{}
	device := pushDevice{
		ServerURL:   "https://relay.example.test",
		AccessToken: "server-access-key",
	}

	addPushAvatarURL(payload, entry, device)

	rawURL, ok := payload["avatarURL"].(string)
	if !ok {
		t.Fatal("avatarURL was not added")
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse avatarURL: %v", err)
	}
	if parsed.Scheme != "https" || parsed.Host != "relay.example.test" || parsed.Path != "/v1/assets/serve" {
		t.Fatalf("avatarURL target = %s, want https://relay.example.test/v1/assets/serve", parsed.String())
	}
	if parsed.Query().Get("url") != "mxc://example.test/avatar" {
		t.Fatalf("avatar source = %q", parsed.Query().Get("url"))
	}
	wantSignature := assetAccessSignature(device.AccessToken, "mxc://example.test/avatar")
	if parsed.Query().Get("assetAccessSignature") != wantSignature {
		t.Fatal("avatarURL signature does not match the device access token")
	}
	if strings.Contains(rawURL, device.AccessToken) {
		t.Fatal("avatarURL exposed the bearer token")
	}
}

func TestDirectPushPayloadOmitsSubtitle(t *testing.T) {
	payload, ok := makePushPayload(compatRecord{
		"id":          "message-1",
		"chatID":      "chat-1",
		"senderName":  "Alice",
		"chatTitle":   "Alice",
		"isGroupChat": false,
		"text":        "Hello",
	})
	if !ok {
		t.Fatal("makePushPayload() skipped an incoming direct message")
	}

	aps := payload["aps"].(map[string]any)
	alert := aps["alert"].(map[string]string)
	if alert["title"] != "Alice" {
		t.Fatalf("title = %q, want sender name", alert["title"])
	}
	if _, exists := alert["subtitle"]; exists {
		t.Fatal("direct message payload should not include a subtitle")
	}
}

func TestPushPayloadSkipsMessagesExcludedByRelay(t *testing.T) {
	now := time.Date(2026, time.July, 30, 8, 0, 0, 0, time.UTC)
	base := compatRecord{
		"id":         "message-1",
		"chatID":     "chat-1",
		"senderName": "Alice",
		"text":       "Hello",
		"timestamp":  now.Format(time.RFC3339Nano),
	}
	tests := []struct {
		name   string
		mutate func(compatRecord)
	}{
		{
			name: "outgoing",
			mutate: func(entry compatRecord) {
				entry["isSender"] = true
			},
		},
		{
			name: "reaction",
			mutate: func(entry compatRecord) {
				entry["type"] = "REACTION"
			},
		},
		{
			name: "member join type",
			mutate: func(entry compatRecord) {
				entry["type"] = "MEMBER_JOIN"
			},
		},
		{
			name: "member invite type",
			mutate: func(entry compatRecord) {
				entry["type"] = "MEMBER_INVITE"
			},
		},
		{
			name: "member join fallback text",
			mutate: func(entry compatRecord) {
				entry["text"] = "Alice joined the chat"
			},
		},
		{
			name: "member invite fallback text",
			mutate: func(entry compatRecord) {
				entry["text"] = "Alice was invited to the chat"
			},
		},
		{
			name: "stale message",
			mutate: func(entry compatRecord) {
				entry["timestamp"] = now.Add(-time.Minute - time.Millisecond).Format(time.RFC3339Nano)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			entry := make(compatRecord, len(base))
			for key, value := range base {
				entry[key] = value
			}
			test.mutate(entry)
			if _, ok := makePushPayloadAt(entry, now); ok {
				t.Fatal("makePushPayloadAt() allowed an event Relay excludes")
			}
		})
	}
}

func TestPushPayloadAllowsFreshIncomingMessage(t *testing.T) {
	now := time.Date(2026, time.July, 30, 8, 0, 0, 0, time.UTC)
	_, ok := makePushPayloadAt(compatRecord{
		"id":         "message-1",
		"chatID":     "chat-1",
		"senderName": "Alice",
		"text":       "Hello",
		"timestamp":  now.Add(-time.Minute).Format(time.RFC3339Nano),
	}, now)
	if !ok {
		t.Fatal("makePushPayloadAt() skipped a fresh incoming message")
	}
}

func TestRoomAccountDataAllowsPushHonorsMuteState(t *testing.T) {
	tests := []struct {
		name       string
		mutedUntil int64
		want       bool
	}{
		{name: "unmuted", mutedUntil: 0, want: true},
		{name: "muted forever", mutedUntil: -1, want: false},
		{name: "future mute", mutedUntil: time.Now().Add(time.Hour).UnixMilli(), want: false},
		{name: "expired mute", mutedUntil: time.Now().Add(-time.Hour).UnixMilli(), want: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			content, err := json.Marshal(event.BeeperMuteEventContent{
				MutedUntil: test.mutedUntil,
			})
			if err != nil {
				t.Fatalf("marshal mute content: %v", err)
			}
			accountData := []*database.AccountData{{
				Type:    event.AccountDataBeeperMute.Type,
				Content: content,
			}}

			if got := roomAccountDataAllowsPush(accountData); got != test.want {
				t.Fatalf("roomAccountDataAllowsPush() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestRoomAccountDataAllowsPushDefaultsToEnabled(t *testing.T) {
	if !roomAccountDataAllowsPush(nil) {
		t.Fatal("roomAccountDataAllowsPush() disabled push without mute account data")
	}
}

func TestRoomAccountDataAllowsPushOnlyForPrimaryInbox(t *testing.T) {
	tests := []struct {
		name        string
		accountData []*database.AccountData
		want        bool
	}{
		{
			name: "low priority",
			accountData: []*database.AccountData{{
				Type:    event.AccountDataRoomTags.Type,
				Content: []byte(`{"tags":{"m.lowpriority":{}}}`),
			}},
			want: false,
		},
		{
			name: "archived",
			accountData: []*database.AccountData{{
				Type:    "com.beeper.inbox.done",
				Content: []byte(`{"updated_ts":100}`),
			}},
			want: false,
		},
		{
			name: "newer marked unread restores primary inbox",
			accountData: []*database.AccountData{
				{
					Type:    "com.beeper.inbox.done",
					Content: []byte(`{"updated_ts":100}`),
				},
				{
					Type:    "m.marked_unread",
					Content: []byte(`{"unread":true,"ts":200}`),
				},
			},
			want: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := roomAccountDataAllowsPush(test.accountData); got != test.want {
				t.Fatalf("roomAccountDataAllowsPush() = %t, want %t", got, test.want)
			}
		})
	}
}
