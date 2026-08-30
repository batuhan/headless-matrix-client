package server

import (
	"testing"

	"go.mau.fi/gomuks/pkg/hicli/database"
	"maunium.net/go/mautrix/event"

	"github.com/batuhan/easymatrix/internal/compat"
)

func TestApplyStoredMessageContentRetainsRedactedText(t *testing.T) {
	evt := &database.Event{
		Type:       event.EventMessage.Type,
		Content:    []byte(`{"msgtype":"m.text","body":"Retained text"}`),
		RedactedBy: "$redaction",
	}
	message := compat.Message{IsDeleted: evt.RedactedBy != ""}

	if err := applyStoredMessageContent(&message, evt, event.EventMessage.Type); err != nil {
		t.Fatalf("apply stored message content: %v", err)
	}
	if !message.IsDeleted {
		t.Fatal("expected message to remain marked deleted")
	}
	if message.Text != "Retained text" {
		t.Fatalf("expected retained text, got %q", message.Text)
	}
}

func TestApplyStoredMessageContentMapsMentions(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		mentions []string
	}{
		{
			name:     "user and room mentions",
			content:  `{"msgtype":"m.text","body":"Hi @Alice and @room","m.mentions":{"user_ids":["@alice:example.com"],"room":true}}`,
			mentions: []string{"@alice:example.com", "@room"},
		},
		{
			name:     "modern message without mentions",
			content:  `{"msgtype":"m.text","body":"Hello","m.mentions":{}}`,
			mentions: []string{},
		},
		{
			name:     "legacy message",
			content:  `{"msgtype":"m.text","body":"Hello"}`,
			mentions: nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			evt := &database.Event{
				Type:    event.EventMessage.Type,
				Content: []byte(test.content),
			}
			message := compat.Message{}

			if err := applyStoredMessageContent(&message, evt, event.EventMessage.Type); err != nil {
				t.Fatalf("apply stored message content: %v", err)
			}
			if len(message.Mentions) != len(test.mentions) {
				t.Fatalf("expected mentions %v, got %v", test.mentions, message.Mentions)
			}
			for index, mention := range test.mentions {
				if message.Mentions[index] != mention {
					t.Fatalf("expected mentions %v, got %v", test.mentions, message.Mentions)
				}
			}
			if (message.Mentions == nil) != (test.mentions == nil) {
				t.Fatalf("expected nil state %v, got %v", test.mentions == nil, message.Mentions == nil)
			}
		})
	}
}

func TestApplyStoredMessageContentUsesOnlyMediaCaptionAsText(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		expected string
	}{
		{
			name:     "filename fallback",
			content:  `{"msgtype":"m.image","body":"image.jpg","filename":"image.jpg","url":"mxc://example.com/image"}`,
			expected: "",
		},
		{
			name:     "authored caption",
			content:  `{"msgtype":"m.image","body":"Look at this https://example.com/a/very/long/path","filename":"image.jpg","url":"mxc://example.com/image"}`,
			expected: "Look at this https://example.com/a/very/long/path",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			evt := &database.Event{
				Type:    event.EventMessage.Type,
				Content: []byte(test.content),
			}
			message := compat.Message{}

			if err := applyStoredMessageContent(&message, evt, event.EventMessage.Type); err != nil {
				t.Fatalf("apply stored message content: %v", err)
			}
			if message.Text != test.expected {
				t.Fatalf("expected text %q, got %q", test.expected, message.Text)
			}
			if len(message.Attachments) != 1 {
				t.Fatalf("expected one attachment, got %d", len(message.Attachments))
			}
		})
	}
}

func TestApplyStoredMessageContentRemovesReplyFallback(t *testing.T) {
	evt := &database.Event{
		Type: event.EventMessage.Type,
		Content: []byte(`{
			"msgtype":"m.text",
			"body":"> <@alice:example.com> https://example.com/original\n> quoted text\n\nMy reply https://example.com/reply",
			"format":"org.matrix.custom.html",
			"formatted_body":"<mx-reply><blockquote><a href=\"https://matrix.to/#/!room:example.com/$original\">In reply to</a> <a href=\"https://matrix.to/#/@alice:example.com\">@alice</a><br>quoted text</blockquote></mx-reply>My reply https://example.com/reply",
			"m.relates_to":{"m.in_reply_to":{"event_id":"$original"}}
		}`),
	}
	message := compat.Message{}

	if err := applyStoredMessageContent(&message, evt, event.EventMessage.Type); err != nil {
		t.Fatalf("apply stored message content: %v", err)
	}
	if message.Text != "My reply https://example.com/reply" {
		t.Fatalf("expected reply fallback to be removed, got %q", message.Text)
	}
}
