package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"go.mau.fi/gomuks/pkg/hicli/database"
	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"

	"github.com/batuhan/gomuks-beeper-api/internal/compat"
	errs "github.com/batuhan/gomuks-beeper-api/internal/errors"
)

const (
	messagePageSize = 20
)

const timelineSelectBase = `
	SELECT event.rowid, timeline.rowid,
	       event.room_id, event_id, sender, type, state_key, timestamp, content, decrypted, decrypted_type,
	       unsigned, local_content, transaction_id, redacted_by, relates_to, relation_type,
	       megolm_session_id, decryption_error, send_error, reactions, last_edit_rowid, unread_type
	FROM timeline
	JOIN event ON event.rowid = timeline.event_rowid
	WHERE timeline.room_id = ?
`

const timelineSelectBefore = timelineSelectBase + ` AND (? = 0 OR timeline.rowid < ?) ORDER BY timeline.rowid DESC LIMIT ?`
const timelineSelectAfter = timelineSelectBase + ` AND (? = 0 OR timeline.rowid > ?) ORDER BY timeline.rowid ASC LIMIT ?`

var errSkipEvent = errors.New("skip event")

func (s *Server) listMessages(w http.ResponseWriter, r *http.Request) error {
	chatID := readChatID(r, "")
	if chatID == "" {
		return errs.Validation(map[string]any{"chatID": "chatID is required"})
	}
	direction, err := parseDirection(r.URL.Query().Get("direction"))
	if err != nil {
		return err
	}
	cursorValue, err := parseMessageCursor(r.URL.Query().Get("cursor"))
	if err != nil {
		return err
	}

	lookup, err := s.buildAccountLookup(r.Context())
	if err != nil {
		return err
	}
	room, err := s.rt.Client().DB.Room.Get(r.Context(), id.RoomID(chatID))
	if err != nil {
		return errs.Internal(fmt.Errorf("failed to get room: %w", err))
	}
	if room == nil {
		return errs.NotFound("Chat not found")
	}

	events, hasMore, err := s.loadTimelineEvents(r.Context(), room.ID, cursorValue, direction, messagePageSize+1)
	if err != nil {
		return err
	}
	if len(events) > messagePageSize {
		events = events[:messagePageSize]
	}

	memberNames := s.loadMemberNameMap(r.Context(), room.ID)
	reactions, err := s.loadReactionMap(r.Context(), room.ID, events)
	if err != nil {
		return err
	}

	messages := make([]compat.Message, 0, len(events))
	for _, evt := range events {
		mapped, mapErr := s.mapEventToMessage(r.Context(), evt, room, lookup, reactionBundle{Names: memberNames, Reactions: reactions})
		if errors.Is(mapErr, errSkipEvent) {
			continue
		}
		if mapErr != nil {
			continue
		}
		messages = append(messages, mapped)
	}

	return writeJSON(w, compat.ListMessagesOutput{Items: messages, HasMore: hasMore})
}

func (s *Server) sendMessage(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		ChatID string `json:"chatID"`
		compat.SendMessageInput
	}
	if err := decodeJSON(r, &req); err != nil {
		return err
	}
	chatID := readChatID(r, req.ChatID)
	if chatID == "" {
		return errs.Validation(map[string]any{"chatID": "chatID is required"})
	}
	if strings.TrimSpace(req.Text) == "" && req.Attachment == nil {
		return errs.Validation(map[string]any{"text": "text or attachment is required"})
	}

	cli := s.rt.Client()
	roomID := id.RoomID(chatID)
	if room, err := cli.DB.Room.Get(r.Context(), roomID); err != nil {
		return errs.Internal(fmt.Errorf("failed to get room: %w", err))
	} else if room == nil {
		return errs.NotFound("Chat not found")
	}

	var base *event.MessageEventContent
	var err error
	if req.Attachment != nil {
		base, err = s.buildAttachmentMessageContent(r.Context(), req.Attachment)
		if err != nil {
			return err
		}
	}

	var relatesTo *event.RelatesTo
	if req.ReplyToMessageID != "" {
		relatesTo = &event.RelatesTo{InReplyTo: &event.InReplyTo{EventID: id.EventID(req.ReplyToMessageID)}}
	}

	dbEvent, err := cli.SendMessage(r.Context(), roomID, base, nil, req.Text, relatesTo, nil, nil)
	if err != nil {
		return errs.Internal(fmt.Errorf("failed to send message: %w", err))
	}
	pendingMessageID := dbEvent.TransactionID
	if pendingMessageID == "" {
		pendingMessageID = string(dbEvent.ID)
	}

	return writeJSON(w, compat.SendMessageOutput{ChatID: chatID, PendingMessageID: pendingMessageID})
}

func (s *Server) editMessage(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		ChatID    string `json:"chatID"`
		MessageID string `json:"messageID"`
		compat.EditMessageInput
	}
	if err := decodeJSON(r, &req); err != nil {
		return err
	}
	chatID := readChatID(r, req.ChatID)
	if chatID == "" {
		return errs.Validation(map[string]any{"chatID": "chatID is required"})
	}
	messageID := r.PathValue("messageID")
	if messageID == "" {
		messageID = req.MessageID
	}
	if messageID == "" {
		return errs.Validation(map[string]any{"messageID": "messageID is required"})
	}
	if strings.TrimSpace(req.Text) == "" {
		return errs.Validation(map[string]any{"text": "text is required"})
	}

	cli := s.rt.Client()
	targetEvent, err := cli.DB.Event.GetByID(r.Context(), id.EventID(messageID))
	if err != nil {
		return errs.Internal(fmt.Errorf("failed to get target message: %w", err))
	}
	if targetEvent == nil {
		return errs.NotFound("Message not found")
	}
	if eventHasAttachment(targetEvent) {
		return errs.Validation(map[string]any{"messageID": "cannot edit messages with attachments"})
	}

	relatesTo := &event.RelatesTo{Type: event.RelReplace, EventID: id.EventID(messageID)}
	if _, err = cli.SendMessage(r.Context(), id.RoomID(chatID), nil, nil, req.Text, relatesTo, nil, nil); err != nil {
		return errs.Internal(fmt.Errorf("failed to edit message: %w", err))
	}

	return writeJSON(w, compat.EditMessageOutput{ChatID: chatID, MessageID: messageID, Success: true})
}

func (s *Server) addReaction(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		ChatID    string `json:"chatID"`
		MessageID string `json:"messageID"`
		compat.AddReactionInput
	}
	if err := decodeJSON(r, &req); err != nil {
		return err
	}
	chatID := readChatID(r, req.ChatID)
	if chatID == "" {
		return errs.Validation(map[string]any{"chatID": "chatID is required"})
	}
	messageID := r.PathValue("messageID")
	if messageID == "" {
		messageID = req.MessageID
	}
	if messageID == "" {
		return errs.Validation(map[string]any{"messageID": "messageID is required"})
	}
	if strings.TrimSpace(req.ReactionKey) == "" {
		return errs.Validation(map[string]any{"reactionKey": "reactionKey is required"})
	}

	content := &event.ReactionEventContent{
		RelatesTo: event.RelatesTo{
			Type:    event.RelAnnotation,
			EventID: id.EventID(messageID),
			Key:     req.ReactionKey,
		},
	}
	dbEvt, err := s.rt.Client().Send(r.Context(), id.RoomID(chatID), event.EventReaction, content, false, false)
	if err != nil {
		return errs.Internal(fmt.Errorf("failed to add reaction: %w", err))
	}
	transactionID := req.TransactionID
	if transactionID == "" && dbEvt != nil {
		transactionID = dbEvt.TransactionID
	}
	if transactionID == "" {
		transactionID = randomID()
	}

	return writeJSON(w, compat.AddReactionOutput{
		Success:       true,
		ChatID:        chatID,
		MessageID:     messageID,
		ReactionKey:   req.ReactionKey,
		TransactionID: transactionID,
	})
}

func (s *Server) removeReaction(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		ChatID    string `json:"chatID"`
		MessageID string `json:"messageID"`
		compat.RemoveReactionInput
	}
	_ = decodeJSONIfPresent(r, &req)
	chatID := readChatID(r, req.ChatID)
	if chatID == "" {
		return errs.Validation(map[string]any{"chatID": "chatID is required"})
	}
	messageID := r.PathValue("messageID")
	if messageID == "" {
		messageID = req.MessageID
	}
	if messageID == "" {
		return errs.Validation(map[string]any{"messageID": "messageID is required"})
	}
	reactionKey := strings.TrimSpace(req.ReactionKey)
	if reactionKey == "" {
		reactionKey = strings.TrimSpace(r.URL.Query().Get("reactionKey"))
	}
	if reactionKey == "" {
		return errs.Validation(map[string]any{"reactionKey": "reactionKey is required"})
	}

	cli := s.rt.Client()
	roomID := id.RoomID(chatID)
	related, err := cli.DB.Event.GetRelatedEvents(r.Context(), roomID, id.EventID(messageID), event.RelAnnotation)
	if err != nil {
		return errs.Internal(fmt.Errorf("failed to query related events: %w", err))
	}
	toRedact := make([]id.EventID, 0)
	for _, evt := range related {
		if evt.Sender != cli.Account.UserID || evt.RedactedBy != "" {
			continue
		}
		var reaction event.ReactionEventContent
		if err = json.Unmarshal(evt.GetContent(), &reaction); err != nil {
			continue
		}
		if reaction.RelatesTo.Key == reactionKey {
			toRedact = append(toRedact, evt.ID)
		}
	}

	for _, reactionEventID := range toRedact {
		if _, err = cli.Client.RedactEvent(r.Context(), roomID, reactionEventID, mautrix.ReqRedact{}); err != nil {
			return errs.Internal(fmt.Errorf("failed to remove reaction: %w", err))
		}
	}

	return writeJSON(w, compat.RemoveReactionOutput{
		Success:     true,
		ChatID:      chatID,
		MessageID:   messageID,
		ReactionKey: reactionKey,
	})
}

func (s *Server) loadTimelineEvents(ctx context.Context, roomID id.RoomID, cursorValue int64, direction string, limit int) ([]*database.Event, bool, error) {
	cli := s.rt.Client()
	query := timelineSelectBefore
	if direction == "after" {
		query = timelineSelectAfter
	}
	rows, err := cli.DB.Query(ctx, query, roomID, cursorValue, cursorValue, limit)
	if err != nil {
		return nil, false, errs.Internal(fmt.Errorf("failed to query timeline: %w", err))
	}
	defer rows.Close()

	events := make([]*database.Event, 0, limit)
	for rows.Next() {
		evt := &database.Event{}
		if _, scanErr := evt.Scan(rows); scanErr != nil {
			return nil, false, errs.Internal(fmt.Errorf("failed to scan timeline event: %w", scanErr))
		}
		events = append(events, evt)
	}
	if err = rows.Err(); err != nil {
		return nil, false, errs.Internal(fmt.Errorf("timeline query failed: %w", err))
	}

	hasMore := len(events) == limit
	if direction == "after" {
		sort.Slice(events, func(i, j int) bool {
			return events[i].TimelineRowID > events[j].TimelineRowID
		})
	}
	return events, hasMore, nil
}

type reactionBundle struct {
	Names     map[string]string
	Reactions map[id.EventID][]compat.Reaction
}

func (s *Server) loadMemberNameMap(ctx context.Context, roomID id.RoomID) map[string]string {
	memberEvents, err := s.rt.Client().DB.CurrentState.GetMembers(ctx, roomID)
	if err != nil {
		return map[string]string{}
	}
	output := make(map[string]string, len(memberEvents))
	for _, memberEvt := range memberEvents {
		if memberEvt.StateKey == nil || *memberEvt.StateKey == "" {
			continue
		}
		var member event.MemberEventContent
		if err = json.Unmarshal(memberEvt.GetContent(), &member); err != nil {
			continue
		}
		name := strings.TrimSpace(member.Displayname)
		if name == "" {
			name = *memberEvt.StateKey
		}
		output[*memberEvt.StateKey] = name
	}
	return output
}

func (s *Server) loadReactionMap(ctx context.Context, roomID id.RoomID, events []*database.Event) (map[id.EventID][]compat.Reaction, error) {
	if len(events) == 0 {
		return map[id.EventID][]compat.Reaction{}, nil
	}
	eventIDs := make([]id.EventID, 0, len(events))
	for _, evt := range events {
		eventIDs = append(eventIDs, evt.ID)
	}
	result, err := s.rt.Client().DB.Event.GetReactions(ctx, roomID, eventIDs...)
	if err != nil {
		return nil, errs.Internal(fmt.Errorf("failed to read reactions: %w", err))
	}
	output := make(map[id.EventID][]compat.Reaction, len(result))
	for evtID, reactionResult := range result {
		if reactionResult == nil || len(reactionResult.Events) == 0 {
			continue
		}
		seen := make(map[string]struct{})
		reactions := make([]compat.Reaction, 0, len(reactionResult.Events))
		for _, reactionEvt := range reactionResult.Events {
			if reactionEvt.RedactedBy != "" {
				continue
			}
			var reaction event.ReactionEventContent
			if err = json.Unmarshal(reactionEvt.GetContent(), &reaction); err != nil {
				continue
			}
			key := strings.TrimSpace(reaction.RelatesTo.Key)
			if key == "" {
				continue
			}
			reactionID := string(reactionEvt.Sender) + ":" + key
			if _, ok := seen[reactionID]; ok {
				continue
			}
			seen[reactionID] = struct{}{}
			reactions = append(reactions, compat.Reaction{
				ID:            reactionID,
				ReactionKey:   key,
				ParticipantID: string(reactionEvt.Sender),
				Emoji:         utf8.RuneCountInString(key) <= 2,
			})
		}
		if len(reactions) > 0 {
			output[evtID] = reactions
		}
	}
	return output, nil
}

func (s *Server) mapEventToMessage(ctx context.Context, evt *database.Event, room *database.Room, lookup *accountLookup, reactions reactionBundle) (compat.Message, error) {
	if evt == nil || evt.RedactedBy != "" {
		return compat.Message{}, errSkipEvent
	}
	evtType := evt.GetType().Type
	if evt.RelationType == event.RelReplace {
		return compat.Message{}, errSkipEvent
	}
	if evtType != event.EventMessage.Type && evtType != event.EventSticker.Type && evtType != event.EventReaction.Type {
		return compat.Message{}, errSkipEvent
	}

	accountID, _ := inferAccountForRoom(room.ID, lookup)
	message := compat.Message{
		ID:        string(evt.ID),
		ChatID:    string(evt.RoomID),
		AccountID: accountID,
		SenderID:  string(evt.Sender),
		Timestamp: evt.Timestamp.Time.UTC().Format(time.RFC3339),
		SortKey:   messageSortKey(evt),
		IsSender:  evt.Sender == s.rt.Client().Account.UserID,
		Reactions: reactions.Reactions[evt.ID],
	}
	if name, ok := reactions.Names[string(evt.Sender)]; ok {
		message.SenderName = name
	} else {
		message.SenderName = string(evt.Sender)
	}
	if replyTo := evt.GetReplyTo(); replyTo != "" {
		message.LinkedMessageID = string(replyTo)
	}

	switch evtType {
	case event.EventReaction.Type:
		var reaction event.ReactionEventContent
		if err := json.Unmarshal(evt.GetContent(), &reaction); err == nil {
			message.Type = "REACTION"
			message.Text = reaction.RelatesTo.Key
			if message.LinkedMessageID == "" {
				message.LinkedMessageID = string(reaction.RelatesTo.EventID)
			}
		}
		return message, nil
	case event.EventSticker.Type, event.EventMessage.Type:
		var content event.MessageEventContent
		if err := json.Unmarshal(evt.GetContent(), &content); err != nil {
			return compat.Message{}, errSkipEvent
		}
		message.Type = mapMessageType(evtType, content.MsgType)
		message.Text = content.Body
		if message.Text == "" && evt.LocalContent != nil {
			message.Text = evt.LocalContent.SanitizedHTML
		}
		if att, ok := messageAttachment(content, evtType); ok {
			message.Attachments = []compat.Attachment{att}
		}
		return message, nil
	default:
		return compat.Message{}, errSkipEvent
	}
}

func mapMessageType(evtType string, msgType event.MessageType) string {
	if evtType == event.EventSticker.Type {
		return "STICKER"
	}
	switch msgType {
	case event.MsgNotice:
		return "NOTICE"
	case event.MsgImage:
		return "IMAGE"
	case event.MsgVideo:
		return "VIDEO"
	case event.MsgAudio:
		return "AUDIO"
	case event.MsgFile:
		return "FILE"
	case event.MsgLocation:
		return "LOCATION"
	default:
		return "TEXT"
	}
}

func messageAttachment(content event.MessageEventContent, evtType string) (compat.Attachment, bool) {
	msgType := content.MsgType
	if evtType == event.EventSticker.Type {
		msgType = "m.sticker"
	}
	isMedia := msgType == event.MsgImage || msgType == event.MsgVideo || msgType == event.MsgAudio || msgType == event.MsgFile || msgType == "m.sticker"
	if !isMedia {
		return compat.Attachment{}, false
	}
	uri := string(content.URL)
	if uri == "" && content.File != nil {
		uri = string(content.File.URL)
	}
	att := compat.Attachment{
		ID:       uri,
		SrcURL:   uri,
		FileName: content.GetFileName(),
		MimeType: "",
		Type:     "unknown",
	}
	if content.Info != nil {
		att.MimeType = content.Info.MimeType
		att.FileSize = int64(content.Info.Size)
		if content.Info.Width > 0 || content.Info.Height > 0 {
			att.Size = &compat.AttachmentSize{Width: content.Info.Width, Height: content.Info.Height}
		}
		if content.Info.Duration > 0 {
			att.Duration = float64(content.Info.Duration) / 1000.0
		}
		if content.Info.ThumbnailURL != "" {
			att.PosterImg = string(content.Info.ThumbnailURL)
		}
	}
	switch msgType {
	case event.MsgImage:
		att.Type = "img"
		att.IsGif = strings.EqualFold(att.MimeType, "image/gif")
	case event.MsgVideo:
		att.Type = "video"
	case event.MsgAudio:
		att.Type = "audio"
	case "m.sticker":
		att.Type = "img"
		att.IsSticker = true
	}
	return att, true
}

func messageSortKey(evt *database.Event) string {
	if evt.TimelineRowID != 0 {
		return strconv.FormatInt(int64(evt.TimelineRowID), 10)
	}
	if evt.RowID != 0 {
		return strconv.FormatInt(int64(evt.RowID), 10)
	}
	return strconv.FormatInt(evt.Timestamp.UnixMilli(), 10)
}

func eventHasAttachment(evt *database.Event) bool {
	var content event.MessageEventContent
	if err := json.Unmarshal(evt.GetContent(), &content); err != nil {
		return false
	}
	if content.URL != "" {
		return true
	}
	return content.File != nil && content.File.URL != ""
}

func (s *Server) buildAttachmentMessageContent(ctx context.Context, attachment *compat.MessageAttachmentInput) (*event.MessageEventContent, error) {
	if attachment == nil {
		return nil, nil
	}
	meta, err := s.loadUploadMetadataByID(attachment.UploadID)
	if err != nil {
		return nil, err
	}
	fileName := attachment.FileName
	if fileName == "" {
		fileName = meta.FileName
	}
	mimeType := attachment.MimeType
	if mimeType == "" {
		mimeType = meta.MimeType
	}
	file, err := os.Open(meta.FilePath)
	if err != nil {
		return nil, errs.Internal(fmt.Errorf("failed to open uploaded asset: %w", err))
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil {
		return nil, errs.Internal(fmt.Errorf("failed to stat uploaded asset: %w", err))
	}

	uploadResp, err := s.rt.Client().Client.UploadMedia(ctx, mautrix.ReqUploadMedia{
		Content:       file,
		ContentLength: stat.Size(),
		ContentType:   mimeType,
		FileName:      fileName,
	})
	if err != nil {
		return nil, errs.Internal(fmt.Errorf("failed to upload media to Matrix: %w", err))
	}

	msgType := messageTypeFromAttachment(mimeType, attachment.Type)
	content := &event.MessageEventContent{
		MsgType:  msgType,
		Body:     fileName,
		URL:      uploadResp.ContentURI.CUString(),
		FileName: fileName,
		Info: &event.FileInfo{
			MimeType: mimeType,
			Size:     int(stat.Size()),
		},
	}
	if attachment.Size != nil {
		content.Info.Width = attachment.Size.Width
		content.Info.Height = attachment.Size.Height
	}
	duration := attachment.Duration
	if duration <= 0 {
		duration = meta.Duration
	}
	if duration > 0 {
		content.Info.Duration = int(duration * 1000)
	}
	return content, nil
}

func messageTypeFromAttachment(mimeType, hint string) event.MessageType {
	switch hint {
	case "sticker":
		return "m.sticker"
	case "voiceNote":
		return event.MsgAudio
	}
	if strings.HasPrefix(mimeType, "image/") {
		return event.MsgImage
	}
	if strings.HasPrefix(mimeType, "video/") {
		return event.MsgVideo
	}
	if strings.HasPrefix(mimeType, "audio/") {
		return event.MsgAudio
	}
	return event.MsgFile
}

func decodeJSONIfPresent(r *http.Request, out any) error {
	if r.Body == nil {
		return nil
	}
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(out); err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return err
	}
	return nil
}
