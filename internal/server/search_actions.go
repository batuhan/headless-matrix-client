package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"go.mau.fi/gomuks/pkg/hicli/database"
	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/id"

	"github.com/batuhan/gomuks-beeper-api/internal/compat"
	"github.com/batuhan/gomuks-beeper-api/internal/cursor"
	errs "github.com/batuhan/gomuks-beeper-api/internal/errors"
)

const (
	searchChatsDefaultLimit    = 50
	searchChatsMaxLimit        = 200
	searchMessagesDefaultLimit = 20
	searchMessagesMaxLimit     = 200
	unifiedChatSectionLimit    = 30
	unifiedMessageSectionLimit = 20
)

const timelineSearchGlobalBase = `
	SELECT event.rowid, timeline.rowid,
	       event.room_id, event_id, sender, type, state_key, timestamp, content, decrypted, decrypted_type,
	       unsigned, local_content, transaction_id, redacted_by, relates_to, relation_type,
	       megolm_session_id, decryption_error, send_error, reactions, last_edit_rowid, unread_type
	FROM timeline
	JOIN event ON event.rowid = timeline.event_rowid
`

const timelineSearchGlobalBefore = timelineSearchGlobalBase + `WHERE (? = 0 OR timeline.rowid < ?) ORDER BY timeline.rowid DESC LIMIT ?`
const timelineSearchGlobalAfter = timelineSearchGlobalBase + `WHERE (? = 0 OR timeline.rowid > ?) ORDER BY timeline.rowid DESC LIMIT ?`

type searchChatsParams struct {
	Query              string
	Scope              string
	Inbox              string
	Type               string
	Direction          string
	Cursor             *cursor.ChatCursor
	Limit              int
	UnreadOnly         bool
	IncludeMuted       bool
	LastActivityBefore *time.Time
	LastActivityAfter  *time.Time
	AccountIDs         []string
}

type searchMessagesParams struct {
	Query              string
	Direction          string
	Cursor             int64
	Limit              int
	ChatIDs            []string
	AccountIDs         []string
	ChatType           string
	Sender             string
	MediaTypes         []string
	DateAfter          *time.Time
	DateBefore         *time.Time
	ExcludeLowPriority bool
	IncludeMuted       bool
}

type reminderInput struct {
	Reminder struct {
		RemindAtMS               int64 `json:"remindAtMs"`
		DismissOnIncomingMessage *bool `json:"dismissOnIncomingMessage,omitempty"`
	} `json:"reminder"`
}

func (s *Server) searchChats(w http.ResponseWriter, r *http.Request) error {
	params, err := parseSearchChatsParams(r)
	if err != nil {
		return err
	}
	out, err := s.searchChatsCore(r.Context(), params)
	if err != nil {
		return err
	}
	return writeJSON(w, out)
}

func (s *Server) searchMessages(w http.ResponseWriter, r *http.Request) error {
	params, err := parseSearchMessagesParams(r)
	if err != nil {
		return err
	}
	out, err := s.searchMessagesCore(r.Context(), params)
	if err != nil {
		return err
	}
	return writeJSON(w, out)
}

func (s *Server) searchContacts(w http.ResponseWriter, r *http.Request) error {
	accountID := strings.TrimSpace(r.PathValue("accountID"))
	if accountID == "" {
		return errs.Validation(map[string]any{"accountID": "accountID is required"})
	}
	query := strings.TrimSpace(r.URL.Query().Get("query"))
	if query == "" {
		return errs.Validation(map[string]any{"query": "query is required"})
	}
	lookup, err := s.buildAccountLookup(r.Context())
	if err != nil {
		return err
	}
	if _, ok := lookup.ByID[accountID]; !ok {
		return errs.NotFound("Account not found")
	}

	resp, err := s.rt.Client().Client.SearchUserDirectory(r.Context(), query, 50)
	if err != nil {
		return errs.Internal(fmt.Errorf("failed to search user directory: %w", err))
	}
	items := make([]compat.User, 0, len(resp.Results))
	for _, user := range resp.Results {
		if user == nil {
			continue
		}
		cannotMessage := false
		isSelf := user.UserID == s.rt.Client().Account.UserID
		fullName := strings.TrimSpace(user.DisplayName)
		if fullName == "" {
			fullName = user.UserID.String()
		}
		username := strings.TrimPrefix(strings.SplitN(user.UserID.String(), ":", 2)[0], "@")
		items = append(items, compat.User{
			ID:            user.UserID.String(),
			Username:      username,
			FullName:      fullName,
			ImgURL:        user.AvatarURL.String(),
			CannotMessage: cannotMessage,
			IsSelf:        isSelf,
		})
	}
	return writeJSON(w, compat.SearchContactsOutput{Items: items})
}

func (s *Server) search(w http.ResponseWriter, r *http.Request) error {
	query := strings.TrimSpace(r.URL.Query().Get("query"))
	if query == "" {
		return errs.Validation(map[string]any{"query": "query is required"})
	}

	chatsResult, err := s.searchChatsCore(r.Context(), searchChatsParams{
		Query:        query,
		Scope:        "titles",
		Type:         "any",
		Direction:    "before",
		Limit:        unifiedChatSectionLimit,
		IncludeMuted: true,
	})
	if err != nil {
		return err
	}
	inGroupsResult, err := s.searchChatsCore(r.Context(), searchChatsParams{
		Query:        query,
		Scope:        "participants",
		Type:         "any",
		Direction:    "before",
		Limit:        unifiedChatSectionLimit,
		IncludeMuted: true,
	})
	if err != nil {
		return err
	}
	messagesResult, err := s.searchMessagesCore(r.Context(), searchMessagesParams{
		Query:              query,
		Direction:          "before",
		Limit:              unifiedMessageSectionLimit,
		IncludeMuted:       true,
		ExcludeLowPriority: true,
	})
	if err != nil {
		return err
	}

	return writeJSON(w, compat.UnifiedSearchOutput{
		Results: compat.UnifiedSearchResults{
			Chats:    chatsResult.Items,
			InGroups: inGroupsResult.Items,
			Messages: messagesResult,
		},
	})
}

func (s *Server) focusApp(w http.ResponseWriter, r *http.Request) error {
	var req compat.FocusAppInput
	if err := decodeJSONIfPresent(r, &req); err != nil {
		return errs.Validation(map[string]any{"error": err.Error()})
	}
	if req.ChatID != "" {
		req.ChatID = readChatID(r, req.ChatID)
	}
	if strings.TrimSpace(req.ChatID) == "" {
		req.ChatID = readChatID(r, "")
	}
	if strings.TrimSpace(req.ChatID) == "" && strings.TrimSpace(req.DraftText) != "" {
		return errs.Validation(map[string]any{"draftText": "chatID is required when draftText is set"})
	}
	return writeJSON(w, compat.FocusAppOutput{Success: true})
}

func (s *Server) createChat(w http.ResponseWriter, r *http.Request) error {
	var req compat.CreateChatInput
	if err := decodeJSON(r, &req); err != nil {
		return err
	}
	req.AccountID = strings.TrimSpace(req.AccountID)
	req.Type = strings.TrimSpace(req.Type)
	if req.AccountID == "" {
		return errs.Validation(map[string]any{"accountID": "accountID is required"})
	}
	if req.Type != "single" && req.Type != "group" {
		return errs.Validation(map[string]any{"type": "must be one of: single, group"})
	}
	if len(req.ParticipantIDs) == 0 {
		return errs.Validation(map[string]any{"participantIDs": "at least one participantID is required"})
	}
	if req.Type == "single" && len(req.ParticipantIDs) != 1 {
		return errs.Validation(map[string]any{"participantIDs": "single chats require exactly one participantID"})
	}

	lookup, err := s.buildAccountLookup(r.Context())
	if err != nil {
		return err
	}
	if _, ok := lookup.ByID[req.AccountID]; !ok {
		return errs.NotFound("Account not found")
	}

	invitees := make([]id.UserID, 0, len(req.ParticipantIDs))
	for _, participantID := range req.ParticipantIDs {
		participantID = strings.TrimSpace(participantID)
		if participantID == "" {
			continue
		}
		invitees = append(invitees, id.UserID(participantID))
	}
	if len(invitees) == 0 {
		return errs.Validation(map[string]any{"participantIDs": "at least one non-empty participantID is required"})
	}

	createReq := &mautrix.ReqCreateRoom{
		Visibility: "private",
		Invite:     invitees,
		IsDirect:   req.Type == "single",
	}
	if req.Type == "group" {
		createReq.Name = strings.TrimSpace(req.Title)
	}

	createResp, err := s.rt.Client().Client.CreateRoom(r.Context(), createReq)
	if err != nil {
		return errs.Internal(fmt.Errorf("failed to create chat: %w", err))
	}

	if strings.TrimSpace(req.MessageText) != "" {
		if _, err = s.rt.Client().SendMessage(
			r.Context(),
			createResp.RoomID,
			nil,
			nil,
			req.MessageText,
			nil,
			nil,
			nil,
		); err != nil {
			return errs.Internal(fmt.Errorf("chat was created but sending first message failed: %w", err))
		}
	}

	return writeJSON(w, compat.CreateChatOutput{ChatID: createResp.RoomID.String()})
}

func (s *Server) archiveChat(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Archived *bool `json:"archived,omitempty"`
	}
	if err := decodeJSONIfPresent(r, &req); err != nil {
		return errs.Validation(map[string]any{"error": err.Error()})
	}
	chatID := readChatID(r, "")
	if chatID == "" {
		return errs.Validation(map[string]any{"chatID": "chatID is required"})
	}
	archived := true
	if req.Archived != nil {
		archived = *req.Archived
	}

	var content any = map[string]any{}
	if archived {
		content = map[string]any{"updated_ts": time.Now().UnixMilli()}
	}
	if err := s.rt.Client().Client.SetRoomAccountData(r.Context(), id.RoomID(chatID), "com.beeper.inbox.done", content); err != nil {
		return errs.Internal(fmt.Errorf("failed to set archive state: %w", err))
	}
	return writeJSON(w, compat.ActionSuccessOutput{Success: true})
}

func (s *Server) setChatReminder(w http.ResponseWriter, r *http.Request) error {
	var req reminderInput
	if err := decodeJSON(r, &req); err != nil {
		return err
	}
	chatID := readChatID(r, "")
	if chatID == "" {
		return errs.Validation(map[string]any{"chatID": "chatID is required"})
	}
	if req.Reminder.RemindAtMS <= 0 {
		return errs.Validation(map[string]any{"reminder.remindAtMs": "must be a positive unix timestamp in milliseconds"})
	}
	dismissOnIncoming := true
	if req.Reminder.DismissOnIncomingMessage != nil {
		dismissOnIncoming = *req.Reminder.DismissOnIncomingMessage
	}
	payload := map[string]any{
		"reminder": map[string]any{
			"remindAtMs":               req.Reminder.RemindAtMS,
			"dismissOnIncomingMessage": dismissOnIncoming,
		},
		"remind_at_ms":                req.Reminder.RemindAtMS,
		"dismiss_on_incoming_message": dismissOnIncoming,
		"remind_at_client":            "desktop",
	}
	if err := s.rt.Client().Client.SetRoomAccountData(r.Context(), id.RoomID(chatID), "com.beeper.chats.reminder", payload); err != nil {
		return errs.Internal(fmt.Errorf("failed to set chat reminder: %w", err))
	}
	return writeJSON(w, compat.ActionSuccessOutput{Success: true})
}

func (s *Server) clearChatReminder(w http.ResponseWriter, r *http.Request) error {
	chatID := readChatID(r, "")
	if chatID == "" {
		return errs.Validation(map[string]any{"chatID": "chatID is required"})
	}
	if err := s.rt.Client().Client.SetRoomAccountData(r.Context(), id.RoomID(chatID), "com.beeper.chats.reminder", map[string]any{}); err != nil {
		return errs.Internal(fmt.Errorf("failed to clear chat reminder: %w", err))
	}
	return writeJSON(w, compat.ActionSuccessOutput{Success: true})
}

func (s *Server) searchChatsCore(ctx context.Context, params searchChatsParams) (compat.SearchChatsOutput, error) {
	lookup, err := s.buildAccountLookup(ctx)
	if err != nil {
		return compat.SearchChatsOutput{}, err
	}
	rooms, err := s.loadRoomsSorted(ctx)
	if err != nil {
		return compat.SearchChatsOutput{}, err
	}
	roomStates, err := s.loadRoomAccountDataStates(ctx)
	if err != nil {
		return compat.SearchChatsOutput{}, err
	}

	items := make([]compat.Chat, 0, params.Limit+1)
	for _, room := range rooms {
		if params.Cursor != nil {
			if params.Direction == "before" && !roomIsOlderThanCursor(room, params.Cursor) {
				continue
			}
			if params.Direction == "after" && !roomIsNewerThanCursor(room, params.Cursor) {
				continue
			}
		}
		state := roomStates[room.ID]
		if !params.IncludeMuted && state.IsMuted {
			continue
		}
		if params.Inbox != "" {
			switch params.Inbox {
			case "primary":
				if state.IsArchived || state.IsLowPriority {
					continue
				}
			case "low-priority":
				if !state.IsLowPriority {
					continue
				}
			case "archive":
				if !state.IsArchived {
					continue
				}
			}
		}

		chat, mapErr := s.mapRoomToChat(ctx, room, lookup, chatPreviewParticipants, false, state)
		if mapErr != nil {
			continue
		}
		if len(params.AccountIDs) > 0 && !equalsAny(chat.AccountID, params.AccountIDs) {
			continue
		}
		if params.Type != "any" && params.Type != "" && string(chat.Type) != params.Type {
			continue
		}
		if params.UnreadOnly && chat.UnreadCount <= 0 {
			continue
		}
		if params.LastActivityBefore != nil && mustParseRFC3339(chat.LastActivity) >= params.LastActivityBefore.UnixMilli() {
			continue
		}
		if params.LastActivityAfter != nil && mustParseRFC3339(chat.LastActivity) <= params.LastActivityAfter.UnixMilli() {
			continue
		}
		if !matchesChatQuery(chat, params.Query, params.Scope) {
			continue
		}

		items = append(items, chat)
		if len(items) > params.Limit {
			break
		}
	}

	hasMore := len(items) > params.Limit
	if hasMore {
		items = items[:params.Limit]
	}
	var oldestCursor *string
	var newestCursor *string
	if len(items) > 0 {
		firstTS := mustParseRFC3339(items[0].LastActivity)
		lastTS := mustParseRFC3339(items[len(items)-1].LastActivity)
		newestEncoded, newErr := cursor.Encode(cursor.ChatCursor{TS: firstTS, RoomID: items[0].ID})
		oldestEncoded, oldErr := cursor.Encode(cursor.ChatCursor{TS: lastTS, RoomID: items[len(items)-1].ID})
		if firstErr(newErr, oldErr) == nil {
			newestCursor = &newestEncoded
			oldestCursor = &oldestEncoded
		}
	}
	return compat.SearchChatsOutput{
		Items:        items,
		HasMore:      hasMore,
		OldestCursor: oldestCursor,
		NewestCursor: newestCursor,
	}, nil
}

func (s *Server) searchMessagesCore(ctx context.Context, params searchMessagesParams) (compat.SearchMessagesOutput, error) {
	lookup, err := s.buildAccountLookup(ctx)
	if err != nil {
		return compat.SearchMessagesOutput{}, err
	}
	rooms, err := s.loadRoomsSorted(ctx)
	if err != nil {
		return compat.SearchMessagesOutput{}, err
	}
	roomStates, err := s.loadRoomAccountDataStates(ctx)
	if err != nil {
		return compat.SearchMessagesOutput{}, err
	}

	roomByID := make(map[id.RoomID]*database.Room, len(rooms))
	for _, room := range rooms {
		roomByID[room.ID] = room
	}

	fetchLimit := params.Limit * 8
	if fetchLimit < params.Limit+1 {
		fetchLimit = params.Limit + 1
	}
	if fetchLimit > 1000 {
		fetchLimit = 1000
	}
	events, dbHasMore, err := s.loadTimelineEventsGlobal(ctx, params.Cursor, params.Direction, fetchLimit)
	if err != nil {
		return compat.SearchMessagesOutput{}, err
	}

	chatIDFilter := make(map[string]struct{}, len(params.ChatIDs))
	for _, chatID := range params.ChatIDs {
		chatIDFilter[chatID] = struct{}{}
	}
	namesByRoom := make(map[id.RoomID]map[string]string)
	reactionsByRoom := make(map[id.RoomID]map[id.EventID][]compat.Reaction)
	items := make([]compat.Message, 0, params.Limit+1)
	itemRowIDs := make([]int64, 0, params.Limit+1)
	usedRooms := make(map[id.RoomID]struct{})

	for _, evt := range events {
		room, ok := roomByID[evt.RoomID]
		if !ok || room == nil {
			continue
		}
		if len(chatIDFilter) > 0 {
			if _, ok = chatIDFilter[string(room.ID)]; !ok {
				continue
			}
		}
		state := roomStates[room.ID]
		if params.ExcludeLowPriority && state.IsLowPriority {
			continue
		}
		if !params.IncludeMuted && state.IsMuted {
			continue
		}

		accountID, _ := inferAccountForRoom(room.ID, lookup)
		if len(params.AccountIDs) > 0 && !equalsAny(accountID, params.AccountIDs) {
			continue
		}
		if params.ChatType == "single" && (room.DMUserID == nil || *room.DMUserID == "") {
			continue
		}
		if params.ChatType == "group" && room.DMUserID != nil && *room.DMUserID != "" {
			continue
		}

		names := namesByRoom[room.ID]
		if names == nil {
			names = s.loadMemberNameMap(ctx, room.ID)
			namesByRoom[room.ID] = names
		}
		reactions := reactionsByRoom[room.ID]
		if reactions == nil {
			reactions = make(map[id.EventID][]compat.Reaction)
			reactionsByRoom[room.ID] = reactions
		}
		if _, ok = reactions[evt.ID]; !ok {
			roomReactionMap, reactionErr := s.loadReactionMap(ctx, room.ID, []*database.Event{evt})
			if reactionErr == nil {
				for eventID, roomReactions := range roomReactionMap {
					reactions[eventID] = roomReactions
				}
			}
		}

		msg, mapErr := s.mapEventToMessage(ctx, evt, room, lookup, reactionBundle{
			Names:     names,
			Reactions: reactions,
		})
		if errors.Is(mapErr, errSkipEvent) {
			continue
		}
		if mapErr != nil {
			continue
		}
		if !matchesAllTokens(params.Query, msg.Text) {
			continue
		}
		if !matchesSender(msg, params.Sender) {
			continue
		}
		if !matchesMedia(msg, params.MediaTypes) {
			continue
		}
		if !matchesMessageDate(msg.Timestamp, params.DateAfter, params.DateBefore) {
			continue
		}

		items = append(items, msg)
		itemRowIDs = append(itemRowIDs, int64(evt.TimelineRowID))
		usedRooms[room.ID] = struct{}{}
		if len(items) > params.Limit {
			break
		}
	}

	hasMore := dbHasMore || len(items) > params.Limit
	if len(items) > params.Limit {
		items = items[:params.Limit]
		itemRowIDs = itemRowIDs[:params.Limit]
	}

	chats := make(map[string]compat.Chat, len(usedRooms))
	for roomID := range usedRooms {
		room := roomByID[roomID]
		if room == nil {
			continue
		}
		chat, mapErr := s.mapRoomToChat(ctx, room, lookup, chatPreviewParticipants, false, roomStates[roomID])
		if mapErr != nil {
			continue
		}
		chats[chat.ID] = chat
	}

	var oldestCursor *string
	var newestCursor *string
	if len(itemRowIDs) > 0 {
		newestRaw := strconv.FormatInt(itemRowIDs[0], 10)
		oldestRaw := strconv.FormatInt(itemRowIDs[len(itemRowIDs)-1], 10)
		newestCursor = &newestRaw
		oldestCursor = &oldestRaw
	}

	return compat.SearchMessagesOutput{
		Items:        items,
		Chats:        chats,
		HasMore:      hasMore,
		OldestCursor: oldestCursor,
		NewestCursor: newestCursor,
	}, nil
}

func (s *Server) loadTimelineEventsGlobal(ctx context.Context, cursorValue int64, direction string, limit int) ([]*database.Event, bool, error) {
	cli := s.rt.Client()
	query := timelineSearchGlobalBefore
	if direction == "after" {
		query = timelineSearchGlobalAfter
	}
	rows, err := cli.DB.Query(ctx, query, cursorValue, cursorValue, limit)
	if err != nil {
		return nil, false, errs.Internal(fmt.Errorf("failed to query global timeline: %w", err))
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
		return nil, false, errs.Internal(fmt.Errorf("global timeline query failed: %w", err))
	}
	return events, len(events) == limit, nil
}

func parseSearchChatsParams(r *http.Request) (searchChatsParams, error) {
	direction, err := parseDirection(r.URL.Query().Get("direction"))
	if err != nil {
		return searchChatsParams{}, err
	}
	cursorValue, err := parseChatCursor(r.URL.Query().Get("cursor"))
	if err != nil {
		return searchChatsParams{}, err
	}
	limit, err := parseOptionalLimit(r.URL.Query().Get("limit"), searchChatsDefaultLimit, 1, searchChatsMaxLimit, "limit")
	if err != nil {
		return searchChatsParams{}, err
	}
	unreadOnly, err := parseOptionalBool(r.URL.Query().Get("unreadOnly"), false, "unreadOnly")
	if err != nil {
		return searchChatsParams{}, err
	}
	includeMuted, err := parseOptionalBool(r.URL.Query().Get("includeMuted"), true, "includeMuted")
	if err != nil {
		return searchChatsParams{}, err
	}
	scope := strings.TrimSpace(r.URL.Query().Get("scope"))
	if scope == "" {
		scope = "titles"
	}
	if scope != "titles" && scope != "participants" {
		return searchChatsParams{}, errs.Validation(map[string]any{"scope": "must be one of: titles, participants"})
	}
	inbox := strings.TrimSpace(r.URL.Query().Get("inbox"))
	if inbox != "" && inbox != "primary" && inbox != "low-priority" && inbox != "archive" {
		return searchChatsParams{}, errs.Validation(map[string]any{"inbox": "must be one of: primary, low-priority, archive"})
	}
	chatType := strings.TrimSpace(r.URL.Query().Get("type"))
	if chatType == "" {
		chatType = "any"
	}
	if chatType != "any" && chatType != "single" && chatType != "group" {
		return searchChatsParams{}, errs.Validation(map[string]any{"type": "must be one of: any, single, group"})
	}
	lastActivityBefore, err := parseOptionalRFC3339(r.URL.Query().Get("lastActivityBefore"), "lastActivityBefore")
	if err != nil {
		return searchChatsParams{}, err
	}
	lastActivityAfter, err := parseOptionalRFC3339(r.URL.Query().Get("lastActivityAfter"), "lastActivityAfter")
	if err != nil {
		return searchChatsParams{}, err
	}
	return searchChatsParams{
		Query:              strings.TrimSpace(r.URL.Query().Get("query")),
		Scope:              scope,
		Inbox:              inbox,
		Type:               chatType,
		Direction:          direction,
		Cursor:             cursorValue,
		Limit:              limit,
		UnreadOnly:         unreadOnly,
		IncludeMuted:       includeMuted,
		LastActivityBefore: lastActivityBefore,
		LastActivityAfter:  lastActivityAfter,
		AccountIDs:         parseAccountIDs(r),
	}, nil
}

func parseSearchMessagesParams(r *http.Request) (searchMessagesParams, error) {
	direction, err := parseDirection(r.URL.Query().Get("direction"))
	if err != nil {
		return searchMessagesParams{}, err
	}
	cursorValue, err := parseMessageCursor(r.URL.Query().Get("cursor"))
	if err != nil {
		return searchMessagesParams{}, err
	}
	limit, err := parseOptionalLimit(r.URL.Query().Get("limit"), searchMessagesDefaultLimit, 1, searchMessagesMaxLimit, "limit")
	if err != nil {
		return searchMessagesParams{}, err
	}
	includeMuted, err := parseOptionalBool(r.URL.Query().Get("includeMuted"), true, "includeMuted")
	if err != nil {
		return searchMessagesParams{}, err
	}
	excludeLowPriority, err := parseOptionalBool(r.URL.Query().Get("excludeLowPriority"), true, "excludeLowPriority")
	if err != nil {
		return searchMessagesParams{}, err
	}
	chatType := strings.TrimSpace(r.URL.Query().Get("chatType"))
	if chatType != "" && chatType != "single" && chatType != "group" {
		return searchMessagesParams{}, errs.Validation(map[string]any{"chatType": "must be one of: single, group"})
	}
	sender := strings.TrimSpace(r.URL.Query().Get("sender"))
	dateAfter, err := parseOptionalRFC3339(r.URL.Query().Get("dateAfter"), "dateAfter")
	if err != nil {
		return searchMessagesParams{}, err
	}
	dateBefore, err := parseOptionalRFC3339(r.URL.Query().Get("dateBefore"), "dateBefore")
	if err != nil {
		return searchMessagesParams{}, err
	}
	if dateAfter != nil && dateBefore != nil && !dateAfter.Before(*dateBefore) {
		return searchMessagesParams{}, errs.Validation(map[string]any{"dateAfter": "must be earlier than dateBefore"})
	}
	mediaTypes, err := parseEnumList(r, "mediaTypes", []string{"any", "video", "image", "link", "file"})
	if err != nil {
		return searchMessagesParams{}, err
	}
	return searchMessagesParams{
		Query:              strings.TrimSpace(r.URL.Query().Get("query")),
		Direction:          direction,
		Cursor:             cursorValue,
		Limit:              limit,
		ChatIDs:            parseStringListParam(r, "chatIDs"),
		AccountIDs:         parseAccountIDs(r),
		ChatType:           chatType,
		Sender:             sender,
		MediaTypes:         mediaTypes,
		DateAfter:          dateAfter,
		DateBefore:         dateBefore,
		ExcludeLowPriority: excludeLowPriority,
		IncludeMuted:       includeMuted,
	}, nil
}

func parseStringListParam(r *http.Request, key string) []string {
	values := make([]string, 0)
	for _, raw := range r.URL.Query()[key] {
		for _, part := range strings.Split(raw, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				values = append(values, part)
			}
		}
	}
	return values
}

func parseEnumList(r *http.Request, key string, allowed []string) ([]string, error) {
	values := parseStringListParam(r, key)
	if len(values) == 0 {
		return nil, nil
	}
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, candidate := range allowed {
		allowedSet[candidate] = struct{}{}
	}
	for _, value := range values {
		if _, ok := allowedSet[value]; !ok {
			return nil, errs.Validation(map[string]any{key: "contains unsupported value"})
		}
	}
	return values, nil
}

func parseOptionalLimit(raw string, defaultValue, minValue, maxValue int, field string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return defaultValue, nil
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil {
		return 0, errs.Validation(map[string]any{field: "must be an integer"})
	}
	if parsed < minValue || parsed > maxValue {
		return 0, errs.Validation(map[string]any{field: fmt.Sprintf("must be between %d and %d", minValue, maxValue)})
	}
	return parsed, nil
}

func parseOptionalBool(raw string, defaultValue bool, field string) (bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return defaultValue, nil
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return false, errs.Validation(map[string]any{field: "must be true or false"})
	}
	return value, nil
}

func parseOptionalRFC3339(raw, field string) (*time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return nil, errs.Validation(map[string]any{field: "must be an RFC3339 datetime"})
	}
	return &parsed, nil
}

func matchesChatQuery(chat compat.Chat, query, scope string) bool {
	query = strings.TrimSpace(query)
	if query == "" {
		return true
	}
	tokens := strings.Fields(strings.ToLower(query))
	if len(tokens) == 0 {
		return true
	}
	var haystack string
	if scope == "participants" {
		var parts []string
		for _, participant := range chat.Participants.Items {
			parts = append(parts, participant.FullName, participant.Username, participant.ID)
		}
		haystack = strings.ToLower(strings.Join(parts, " "))
	} else {
		haystack = strings.ToLower(chat.Title + " " + chat.Network)
	}
	for _, token := range tokens {
		if !strings.Contains(haystack, token) {
			return false
		}
	}
	return true
}

func matchesAllTokens(query, text string) bool {
	query = strings.TrimSpace(query)
	if query == "" {
		return true
	}
	tokens := strings.Fields(strings.ToLower(query))
	haystack := strings.ToLower(text)
	for _, token := range tokens {
		if !strings.Contains(haystack, token) {
			return false
		}
	}
	return true
}

func matchesSender(msg compat.Message, sender string) bool {
	sender = strings.TrimSpace(sender)
	switch sender {
	case "":
		return true
	case "me":
		return msg.IsSender
	case "others":
		return !msg.IsSender
	default:
		return msg.SenderID == sender
	}
}

func matchesMedia(msg compat.Message, mediaTypes []string) bool {
	if len(mediaTypes) == 0 {
		return true
	}
	hasLink := strings.Contains(strings.ToLower(msg.Text), "http://") || strings.Contains(strings.ToLower(msg.Text), "https://")
	for _, mediaType := range mediaTypes {
		switch mediaType {
		case "any":
			if len(msg.Attachments) > 0 || hasLink {
				return true
			}
		case "video":
			if string(msg.Type) == "VIDEO" {
				return true
			}
		case "image":
			if string(msg.Type) == "IMAGE" || string(msg.Type) == "STICKER" {
				return true
			}
		case "file":
			if string(msg.Type) == "FILE" {
				return true
			}
		case "link":
			if hasLink {
				return true
			}
		}
	}
	return false
}

func matchesMessageDate(timestamp time.Time, dateAfter, dateBefore *time.Time) bool {
	if dateAfter == nil && dateBefore == nil {
		return true
	}
	if timestamp.IsZero() {
		return false
	}
	if dateAfter != nil && !timestamp.After(*dateAfter) {
		return false
	}
	if dateBefore != nil && !timestamp.Before(*dateBefore) {
		return false
	}
	return true
}
