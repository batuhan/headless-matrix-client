package cursor

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
)

type ChatCursor struct {
	TS     int64  `json:"ts"`
	RoomID string `json:"room_id,omitempty"`
}

type MessageCursor struct {
	TimelineRowID int64 `json:"timeline_row_id"`
}

func Encode(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func Decode(raw string, out any) error {
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return fmt.Errorf("invalid cursor: %w", err)
	}
	if err = json.Unmarshal(decoded, out); err != nil {
		return fmt.Errorf("invalid cursor payload: %w", err)
	}
	return nil
}
