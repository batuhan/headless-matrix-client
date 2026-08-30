package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"

	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"

	"github.com/batuhan/easymatrix/internal/compat"
	errs "github.com/batuhan/easymatrix/internal/errors"
)

// muteChat toggles the mute state of a chat via the com.beeper.mute room account data.
func (s *Server) muteChat(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		ChatID string `json:"chatID,omitempty"`
		Muted  *bool  `json:"muted,omitempty"`
	}
	if err := decodeJSON(r, &req); err != nil {
		return err
	}
	chatID := readChatID(r, req.ChatID)
	if chatID == "" {
		return errs.Validation(map[string]any{"chatID": "chatID is required"})
	}
	muted := true
	if req.Muted != nil {
		muted = *req.Muted
	}

	var content event.BeeperMuteEventContent
	if muted {
		content.MutedUntil = -1 // -1 means muted forever
	} else {
		content.MutedUntil = 0 // 0 means not muted
	}
	if err := s.rt.Client().Client.SetRoomAccountData(r.Context(), id.RoomID(chatID), event.AccountDataBeeperMute.Type, content); err != nil {
		return errs.Internal(fmt.Errorf("failed to set mute state: %w", err))
	}
	// The homeserver echoes room account data through sync, but a message can
	// arrive before that echo. Update the local gomuks view immediately so APNs
	// filtering and chat hydration observe the state once this request returns.
	if cli := s.rt.Client(); cli != nil && cli.Account != nil {
		raw, marshalErr := json.Marshal(content)
		if marshalErr != nil {
			log.Printf("failed to encode local mute state for %s: %v", chatID, marshalErr)
		} else if _, cacheErr := cli.DB.AccountData.PutRoom(
			r.Context(),
			cli.Account.UserID,
			id.RoomID(chatID),
			event.AccountDataBeeperMute,
			raw,
		); cacheErr != nil {
			log.Printf("failed to cache local mute state for %s: %v", chatID, cacheErr)
		}
	}
	return writeJSON(w, compat.ActionSuccessOutput{Success: true})
}

// markChatRead sends a read receipt for the latest message in the room.
func (s *Server) markChatRead(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		ChatID string `json:"chatID,omitempty"`
	}
	if err := decodeOptionalJSON(r, &req); err != nil {
		return err
	}
	chatID := readChatID(r, req.ChatID)
	if chatID == "" {
		return errs.Validation(map[string]any{"chatID": "chatID is required"})
	}

	cli := s.rt.Client()
	roomID := id.RoomID(chatID)

	// Find the latest event in the timeline.
	latestEventID, err := s.findLatestEventID(r.Context(), roomID)
	if err != nil {
		return err
	}
	if latestEventID == "" {
		return errs.NotFound("No messages in this chat")
	}

	// Send m.read receipt.
	if err := cli.Client.MarkRead(r.Context(), roomID, id.EventID(latestEventID)); err != nil {
		return errs.Internal(fmt.Errorf("failed to send read receipt: %w", err))
	}

	// Also clear the m.fully_read marker (read marker line).
	if err := cli.Client.SetRoomAccountData(r.Context(), roomID, "m.fully_read", map[string]any{
		"event_id": latestEventID,
	}); err != nil {
		// Non-fatal: the read receipt is the important part.
		_ = err
	}

	return writeJSON(w, compat.ActionSuccessOutput{Success: true})
}

const latestTimelineEventQuery = `
	SELECT event.event_id
	FROM timeline
	JOIN event ON event.rowid = timeline.event_rowid
	WHERE timeline.room_id = ?
	ORDER BY timeline.rowid DESC
	LIMIT 1
`

func (s *Server) findLatestEventID(ctx context.Context, roomID id.RoomID) (string, error) {
	cli := s.rt.Client()
	var eventID string
	err := cli.DB.QueryRow(ctx, latestTimelineEventQuery, roomID).Scan(&eventID)
	if err != nil {
		return "", nil // No events found is not an error.
	}
	return eventID, nil
}

// loadOtherReadReceipts loads read receipts from real (non-bot) other participants.
// Returns a map from userID → timeline rowid of the event they've read up to.
// Bridge bots (e.g., @whatsappbot:beeper.local) are excluded because their
// receipts represent message delivery, not actual reading by a person.
func (s *Server) loadOtherReadReceipts(ctx context.Context, roomID id.RoomID) (map[string]int64, error) {
	cli := s.rt.Client()
	if cli == nil || cli.Account == nil {
		return nil, nil
	}
	const query = `
		SELECT r.user_id, t.rowid
		FROM receipt r
		JOIN event e ON e.event_id = r.event_id AND e.room_id = r.room_id
		JOIN timeline t ON t.event_rowid = e.rowid AND t.room_id = r.room_id
		WHERE r.room_id = ? AND r.receipt_type = 'm.read' AND r.user_id != ?
	`
	rows, err := cli.DB.Query(ctx, query, roomID, cli.Account.UserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string]int64)
	for rows.Next() {
		var userID string
		var timelineRowID int64
		if scanErr := rows.Scan(&userID, &timelineRowID); scanErr != nil {
			continue
		}
		// Skip bridge bots — their receipts indicate delivery, not human reading.
		if isBridgeBot(userID) {
			continue
		}
		if timelineRowID > 0 || timelineRowID < 0 {
			if existing, ok := result[userID]; !ok || timelineRowID > existing {
				result[userID] = timelineRowID
			}
		}
	}
	return result, rows.Err()
}

// computeReadBy determines the furthest-read position among other participants.
// For single chats, this is the other person's read receipt.
// For group chats, this is the max of all other participants' receipts.
// Returns the timeline rowid up to which someone else has read.
func computeMaxOtherRead(receipts map[string]int64) int64 {
	var maxRead int64
	for _, tlRowID := range receipts {
		if tlRowID > maxRead {
			maxRead = tlRowID
		}
	}
	return maxRead
}

// isMessageReadByOther checks if a message (by its sortKey) has been read by
// at least one other participant based on their read receipt positions.
func isMessageReadByOther(sortKey string, maxOtherRead int64) bool {
	if maxOtherRead == 0 {
		return false
	}
	sk, err := strconv.ParseInt(sortKey, 10, 64)
	if err != nil {
		return false
	}
	return sk <= maxOtherRead
}
