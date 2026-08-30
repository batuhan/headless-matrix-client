package compat

import (
	"encoding/json"
	"testing"
)

func TestMessageDeletionStateSerialization(t *testing.T) {
	deletedJSON, err := json.Marshal(Message{ID: "message-1", IsDeleted: true, Text: "Retained text"})
	if err != nil {
		t.Fatalf("marshal deleted message: %v", err)
	}

	var deleted map[string]any
	if err = json.Unmarshal(deletedJSON, &deleted); err != nil {
		t.Fatalf("unmarshal deleted message: %v", err)
	}
	if deleted["isDeleted"] != true {
		t.Fatalf("expected isDeleted to be true, got %#v", deleted["isDeleted"])
	}
	if deleted["text"] != "Retained text" {
		t.Fatalf("expected retained text, got %#v", deleted["text"])
	}

	activeJSON, err := json.Marshal(Message{ID: "message-2"})
	if err != nil {
		t.Fatalf("marshal active message: %v", err)
	}
	var active map[string]any
	if err = json.Unmarshal(activeJSON, &active); err != nil {
		t.Fatalf("unmarshal active message: %v", err)
	}
	if _, exists := active["isDeleted"]; exists {
		t.Fatal("expected isDeleted to be omitted for active messages")
	}
}
