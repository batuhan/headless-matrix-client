package server

import (
	"net/http/httptest"
	"testing"
)

func TestCapabilitiesForNetwork(t *testing.T) {
	for _, networkID := range []string{"telegram", "local-telegram", "telegramgo"} {
		if !capabilitiesForNetwork(networkID).UnlimitedMessageEdits {
			t.Fatalf("expected %q to allow unlimited message edits", networkID)
		}
	}
	if capabilitiesForNetwork("whatsapp").UnlimitedMessageEdits {
		t.Fatal("did not expect WhatsApp to allow unlimited message edits")
	}
}

func TestParseSearchChatsParamsAcceptsUnreadAndAccountIDs(t *testing.T) {
	req := httptest.NewRequest("GET", "/v1/chats/search?inbox=unread&accountIDs=telegram&accountIDs=whatsapp", nil)
	params, err := parseSearchChatsParams(req)
	if err != nil {
		t.Fatalf("parse search params: %v", err)
	}
	if params.Inbox != "unread" {
		t.Fatalf("expected unread inbox, got %q", params.Inbox)
	}
	if len(params.AccountIDs) != 2 || params.AccountIDs[0] != "telegram" || params.AccountIDs[1] != "whatsapp" {
		t.Fatalf("unexpected account IDs: %#v", params.AccountIDs)
	}
}
