package server

import (
	"net/http"
	"sort"
	"strings"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

type wsSetSubscriptionsInput struct {
	Type      string   `json:"type"`
	RequestID string   `json:"requestID,omitempty"`
	ChatIDs   []string `json:"chatIDs"`
}

type wsReadyMessage struct {
	Type    string   `json:"type"`
	Version int      `json:"version"`
	ChatIDs []string `json:"chatIDs"`
}

type wsSubscriptionsUpdatedMessage struct {
	Type      string   `json:"type"`
	RequestID string   `json:"requestID,omitempty"`
	ChatIDs   []string `json:"chatIDs"`
}

type wsErrorMessage struct {
	Type      string `json:"type"`
	RequestID string `json:"requestID,omitempty"`
	Code      string `json:"code"`
	Message   string `json:"message"`
}

func (s *Server) wsEvents(w http.ResponseWriter, r *http.Request) error {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		CompressionMode: websocket.CompressionDisabled,
		OriginPatterns:  []string{"*"},
	})
	if err != nil {
		return nil
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	if err = wsjson.Write(r.Context(), conn, wsReadyMessage{
		Type:    "ready",
		Version: 1,
		ChatIDs: []string{},
	}); err != nil {
		return nil
	}

	for {
		var payload map[string]any
		if err = wsjson.Read(r.Context(), conn, &payload); err != nil {
			return nil
		}

		msgType, _ := payload["type"].(string)
		requestID, _ := payload["requestID"].(string)
		if msgType != "subscriptions.set" {
			_ = wsjson.Write(r.Context(), conn, wsErrorMessage{
				Type:      "error",
				RequestID: requestID,
				Code:      "INVALID_COMMAND",
				Message:   "Unsupported command type",
			})
			continue
		}

		chatIDs, ok := decodeWSChatIDs(payload["chatIDs"])
		if !ok {
			_ = wsjson.Write(r.Context(), conn, wsErrorMessage{
				Type:      "error",
				RequestID: requestID,
				Code:      "INVALID_PAYLOAD",
				Message:   "chatIDs must be an array of strings",
			})
			continue
		}
		normalized, valid := normalizeWSChatIDs(chatIDs)
		if !valid {
			_ = wsjson.Write(r.Context(), conn, wsErrorMessage{
				Type:      "error",
				RequestID: requestID,
				Code:      "INVALID_PAYLOAD",
				Message:   "chatIDs cannot combine '*' with specific IDs",
			})
			continue
		}

		if err = wsjson.Write(r.Context(), conn, wsSubscriptionsUpdatedMessage{
			Type:      "subscriptions.updated",
			RequestID: requestID,
			ChatIDs:   normalized,
		}); err != nil {
			return nil
		}
	}
}

func decodeWSChatIDs(raw any) ([]string, bool) {
	valueList, ok := raw.([]any)
	if !ok {
		return nil, false
	}
	output := make([]string, 0, len(valueList))
	for _, value := range valueList {
		asString, ok := value.(string)
		if !ok {
			return nil, false
		}
		asString = strings.TrimSpace(asString)
		if asString != "" {
			output = append(output, asString)
		}
	}
	return output, true
}

func normalizeWSChatIDs(chatIDs []string) ([]string, bool) {
	if len(chatIDs) == 0 {
		return []string{}, true
	}

	seen := make(map[string]struct{}, len(chatIDs))
	normalized := make([]string, 0, len(chatIDs))
	hasWildcard := false
	for _, chatID := range chatIDs {
		chatID = strings.TrimSpace(chatID)
		if chatID == "" {
			continue
		}
		if chatID == "*" {
			hasWildcard = true
		}
		if _, ok := seen[chatID]; ok {
			continue
		}
		seen[chatID] = struct{}{}
		normalized = append(normalized, chatID)
	}

	if hasWildcard {
		if len(normalized) != 1 || normalized[0] != "*" {
			return nil, false
		}
		return []string{"*"}, true
	}

	sort.Strings(normalized)
	return normalized, true
}
