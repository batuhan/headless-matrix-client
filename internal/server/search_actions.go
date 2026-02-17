package server

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"go.mau.fi/gomuks/pkg/hicli/database"
	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/bridgev2/provisionutil"
	"maunium.net/go/mautrix/id"

	"github.com/batuhan/gomuks-beeper-api/internal/compat"
	"github.com/batuhan/gomuks-beeper-api/internal/cursor"
	errs "github.com/batuhan/gomuks-beeper-api/internal/errors"
	beeperdesktopapi "github.com/beeper/desktop-api-go"
)

const (
	searchChatsDefaultLimit    = 50
	searchChatsMaxLimit        = 200
	searchMessagesDefaultLimit = 20
	searchMessagesMaxLimit     = 20
	searchContactsDefaultLimit = 50
	searchContactsMaxLimit     = 200
	unifiedChatSectionLimit    = 30
	unifiedMessageSectionLimit = 20

	searchMessagesScanBatchSize  = 500
	searchMessagesScanMaxEvents  = 5000
	searchMessagesScanMaxBatches = 20

	contactSourceScoreCloudList    = 100
	contactSourceScoreParticipants = 120
	contactSourceScoreDirectory    = 200
	contactSourceScoreLookup       = 300
	contactLookupMaxCandidates     = 4
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
	ChatID   string `json:"chatID,omitempty"`
	Reminder struct {
		RemindAtMS               int64 `json:"remindAtMs"`
		DismissOnIncomingMessage *bool `json:"dismissOnIncomingMessage,omitempty"`
	} `json:"reminder"`
}

type contactCursor struct {
	Index int `json:"index"`
}

type contactCandidate struct {
	User  compat.User
	Key   string
	Score int
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
	items, err := s.loadAccountContacts(r.Context(), lookup, accountID, query)
	if err != nil {
		return err
	}
	if len(items) > searchContactsMaxLimit {
		items = items[:searchContactsMaxLimit]
	}
	return writeJSON(w, compat.SearchContactsOutput{Items: items})
}

func (s *Server) searchUsersV0(w http.ResponseWriter, r *http.Request) error {
	accountID := strings.TrimSpace(r.URL.Query().Get("accountID"))
	if accountID == "" {
		accountID = strings.TrimSpace(r.URL.Query().Get("desktopAccountID"))
	}
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
			CannotMessage: false,
			IsSelf:        user.UserID == s.rt.Client().Account.UserID,
		})
	}
	return writeJSON(w, compat.SearchContactsOutput{Items: items})
}

func (s *Server) listContacts(w http.ResponseWriter, r *http.Request) error {
	accountID := strings.TrimSpace(r.PathValue("accountID"))
	if accountID == "" {
		return errs.Validation(map[string]any{"accountID": "accountID is required"})
	}
	lookup, err := s.buildAccountLookup(r.Context())
	if err != nil {
		return err
	}
	if _, ok := lookup.ByID[accountID]; !ok {
		return errs.NotFound("Account not found")
	}

	direction, err := parseDirection(r.URL.Query().Get("direction"))
	if err != nil {
		return err
	}
	limit, err := parseOptionalLimit(r.URL.Query().Get("limit"), searchContactsDefaultLimit, 1, searchContactsMaxLimit, "limit")
	if err != nil {
		return err
	}
	cursorValue, err := parseContactCursor(r.URL.Query().Get("cursor"))
	if err != nil {
		return err
	}

	contacts, err := s.loadAccountContacts(r.Context(), lookup, accountID, strings.TrimSpace(r.URL.Query().Get("query")))
	if err != nil {
		return err
	}

	start := 0
	hasMore := false
	switch direction {
	case "after":
		if cursorValue != nil {
			end := cursorValue.Index
			if end < 0 {
				end = 0
			}
			if end > len(contacts) {
				end = len(contacts)
			}
			start = end - limit
			if start < 0 {
				start = 0
			}
			contacts = contacts[start:end]
			hasMore = start > 0
		} else if len(contacts) > limit {
			contacts = contacts[:limit]
			hasMore = true
		}
	default:
		if cursorValue != nil {
			start = cursorValue.Index + 1
		}
		if start > len(contacts) {
			start = len(contacts)
		}
		end := start + limit
		if end > len(contacts) {
			end = len(contacts)
		}
		hasMore = end < len(contacts)
		contacts = contacts[start:end]
	}

	var newestCursor *string
	var oldestCursor *string
	if len(contacts) > 0 {
		newestEncoded, newErr := cursor.Encode(contactCursor{Index: start})
		oldestEncoded, oldErr := cursor.Encode(contactCursor{Index: start + len(contacts) - 1})
		if firstErr(newErr, oldErr) == nil {
			newestCursor = &newestEncoded
			oldestCursor = &oldestEncoded
		}
	}

	return writeJSON(w, compat.ListContactsOutput{
		Items:        contacts,
		HasMore:      hasMore,
		OldestCursor: oldestCursor,
		NewestCursor: newestCursor,
	})
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
	chatID := ""
	if req.ChatID.Valid() {
		chatID = readChatID(r, req.ChatID.Value)
	}
	if strings.TrimSpace(chatID) == "" {
		chatID = readChatID(r, "")
	}
	if strings.TrimSpace(chatID) == "" && strings.TrimSpace(req.DraftText.Or("")) != "" {
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
	req.Mode = strings.TrimSpace(req.Mode)
	if req.Mode == "" {
		req.Mode = "create"
	}
	if req.AccountID == "" {
		return errs.Validation(map[string]any{"accountID": "accountID is required"})
	}
	if req.Mode != "create" && req.Mode != "start" {
		return errs.Validation(map[string]any{"mode": "must be one of: create, start"})
	}

	lookup, err := s.buildAccountLookup(r.Context())
	if err != nil {
		return err
	}
	if _, ok := lookup.ByID[req.AccountID]; !ok {
		return errs.NotFound("Account not found")
	}

	if req.Mode == "start" {
		return s.startChat(w, r, req, lookup)
	}

	chatType := strings.TrimSpace(string(req.Type))
	if chatType != "single" && chatType != "group" {
		return errs.Validation(map[string]any{"type": "must be one of: single, group"})
	}
	if len(req.ParticipantIDs) == 0 {
		return errs.Validation(map[string]any{"participantIDs": "at least one participantID is required"})
	}
	if chatType == "single" && len(req.ParticipantIDs) != 1 {
		return errs.Validation(map[string]any{"participantIDs": "single chats require exactly one participantID"})
	}

	chatID, err := s.createChatRoom(r.Context(), chatType, req.ParticipantIDs, req.Title, req.MessageText)
	if err != nil {
		return err
	}
	return writeJSON(w, newCreateChatOutput(chatID, ""))
}

func (s *Server) startChat(w http.ResponseWriter, r *http.Request, req compat.CreateChatInput, lookup *accountLookup) error {
	if req.User == nil {
		return errs.Validation(map[string]any{"user": "user is required for mode=start"})
	}
	if req.User.CannotMessage {
		return errs.Forbidden("Cannot message this user on the selected account")
	}

	userID, err := s.resolveStartChatUserID(r.Context(), req.User)
	if err != nil {
		return err
	}
	existingChatID, err := s.findExistingSingleChat(r.Context(), lookup, req.AccountID, userID)
	if err != nil {
		return err
	}
	if existingChatID != "" {
		return writeJSON(w, newCreateChatOutput(existingChatID, "existing"))
	}

	chatID, err := s.createChatRoom(r.Context(), "single", []string{userID}, "", req.MessageText)
	if err != nil {
		return err
	}
	return writeJSON(w, newCreateChatOutput(chatID, "created"))
}

func newCreateChatOutput(chatID, status string) compat.CreateChatOutput {
	output := compat.CreateChatOutput{ChatID: chatID}
	switch status {
	case "existing":
		output.Status = beeperdesktopapi.ChatNewResponseStatusExisting
	case "created":
		output.Status = beeperdesktopapi.ChatNewResponseStatusCreated
	}
	return output
}

func (s *Server) createChatRoom(ctx context.Context, chatType string, participantIDs []string, title string, messageText string) (string, error) {
	invitees := make([]id.UserID, 0, len(participantIDs))
	for _, participantID := range participantIDs {
		participantID = strings.TrimSpace(participantID)
		if participantID == "" {
			continue
		}
		invitees = append(invitees, id.UserID(participantID))
	}
	if len(invitees) == 0 {
		return "", errs.Validation(map[string]any{"participantIDs": "at least one non-empty participantID is required"})
	}
	createReq := &mautrix.ReqCreateRoom{
		Visibility: "private",
		Invite:     invitees,
		IsDirect:   chatType == "single",
	}
	if chatType == "group" {
		createReq.Name = strings.TrimSpace(title)
	}

	createResp, err := s.rt.Client().Client.CreateRoom(ctx, createReq)
	if err != nil {
		return "", errs.Internal(fmt.Errorf("failed to create chat: %w", err))
	}

	if strings.TrimSpace(messageText) != "" {
		if _, err = s.rt.Client().SendMessage(
			ctx,
			createResp.RoomID,
			nil,
			nil,
			messageText,
			nil,
			nil,
			nil,
		); err != nil {
			return "", errs.Internal(fmt.Errorf("chat was created but sending first message failed: %w", err))
		}
	}
	return createResp.RoomID.String(), nil
}

func (s *Server) resolveStartChatUserID(ctx context.Context, user *compat.CreateChatStartUserInput) (string, error) {
	if user == nil {
		return "", errs.Validation(map[string]any{"user": "user is required"})
	}
	if directID := strings.TrimSpace(user.ID); directID != "" {
		return directID, nil
	}

	queries := make([]string, 0, 4)
	for _, candidate := range []string{user.Username, user.PhoneNumber, user.Email, user.FullName} {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		duplicate := false
		for _, existing := range queries {
			if strings.EqualFold(existing, candidate) {
				duplicate = true
				break
			}
		}
		if !duplicate {
			queries = append(queries, candidate)
		}
	}
	if len(queries) == 0 {
		return "", errs.Validation(map[string]any{"user": "one of user.id, user.username, user.phoneNumber, user.email, or user.fullName is required"})
	}

	targetUsername := strings.TrimSpace(user.Username)
	targetFullName := strings.TrimSpace(user.FullName)
	var fallbackUserID string
	for _, query := range queries {
		resp, err := s.rt.Client().Client.SearchUserDirectory(ctx, query, 20)
		if err != nil {
			continue
		}
		for _, match := range resp.Results {
			if match == nil || match.UserID == s.rt.Client().Account.UserID {
				continue
			}
			foundUserID := strings.TrimSpace(match.UserID.String())
			if foundUserID == "" {
				continue
			}
			if targetUsername != "" && strings.EqualFold(userIDLocalpart(foundUserID), targetUsername) {
				return foundUserID, nil
			}
			if targetFullName != "" && strings.EqualFold(strings.TrimSpace(match.DisplayName), targetFullName) {
				return foundUserID, nil
			}
			if fallbackUserID == "" {
				fallbackUserID = foundUserID
			}
		}
	}
	if fallbackUserID != "" {
		return fallbackUserID, nil
	}
	return "", errs.NotFound("User not found")
}

func (s *Server) findExistingSingleChat(ctx context.Context, lookup *accountLookup, accountID, userID string) (string, error) {
	rooms, err := s.loadRoomsSorted(ctx)
	if err != nil {
		return "", err
	}
	for _, room := range rooms {
		mappedAccountID, _ := inferAccountForRoom(room.ID, lookup)
		if mappedAccountID != accountID {
			continue
		}
		if room.DMUserID == nil || *room.DMUserID == "" {
			continue
		}
		if userIDMatches(string(*room.DMUserID), userID) {
			return string(room.ID), nil
		}
	}
	return "", nil
}

func userIDMatches(left, right string) bool {
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	if left == "" || right == "" {
		return false
	}
	if strings.EqualFold(left, right) {
		return true
	}
	return strings.EqualFold(userIDLocalpart(left), userIDLocalpart(right))
}

func (s *Server) archiveChat(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Archived *bool  `json:"archived,omitempty"`
		ChatID   string `json:"chatID,omitempty"`
	}
	if err := decodeJSONIfPresent(r, &req); err != nil {
		return errs.Validation(map[string]any{"error": err.Error()})
	}
	chatID := readChatID(r, req.ChatID)
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
	chatID := readChatID(r, req.ChatID)
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
	var req struct {
		ChatID string `json:"chatID,omitempty"`
	}
	if err := decodeJSONIfPresent(r, &req); err != nil {
		return errs.Validation(map[string]any{"error": err.Error()})
	}
	chatID := readChatID(r, req.ChatID)
	if chatID == "" {
		return errs.Validation(map[string]any{"chatID": "chatID is required"})
	}
	if err := s.rt.Client().Client.SetRoomAccountData(r.Context(), id.RoomID(chatID), "com.beeper.chats.reminder", map[string]any{}); err != nil {
		return errs.Internal(fmt.Errorf("failed to clear chat reminder: %w", err))
	}
	return writeJSON(w, compat.ActionSuccessOutput{Success: true})
}

func parseContactCursor(raw string) (*contactCursor, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	if parsed, err := strconv.Atoi(raw); err == nil {
		if parsed < 0 {
			return nil, errs.Validation(map[string]any{"cursor": "must be a non-negative integer"})
		}
		return &contactCursor{Index: parsed}, nil
	}
	var decoded contactCursor
	if err := cursor.Decode(raw, &decoded); err != nil {
		return nil, errs.Validation(map[string]any{"cursor": err.Error()})
	}
	if decoded.Index < 0 {
		return nil, errs.Validation(map[string]any{"cursor": "index must be a non-negative integer"})
	}
	return &decoded, nil
}

func (s *Server) loadAccountContacts(ctx context.Context, lookup *accountLookup, accountID, query string) ([]compat.User, error) {
	query = strings.TrimSpace(query)
	rooms, err := s.loadRoomsSorted(ctx)
	if err != nil {
		return nil, err
	}
	candidates := make([]contactCandidate, 0, 256)
	addCandidate := func(user compat.User, baseScore int) {
		normalized, ok := normalizeContactUser(user)
		if !ok || normalized.IsSelf {
			return
		}
		score := scoreContactForQuery(normalized, query, baseScore)
		if score < 0 {
			return
		}
		candidates = append(candidates, contactCandidate{
			User:  normalized,
			Key:   contactCandidateKey(normalized),
			Score: score,
		})
	}

	for _, room := range rooms {
		mappedAccountID, _ := inferAccountForRoom(room.ID, lookup)
		if mappedAccountID != accountID {
			continue
		}
		participants, _ := s.loadRoomParticipants(ctx, room)
		for _, participant := range participants {
			participant.ID = strings.TrimSpace(participant.ID)
			if participant.ID == "" || participant.IsSelf {
				continue
			}
			if participant.Username == "" {
				participant.Username = userIDLocalpart(participant.ID)
			}
			addCandidate(participant, contactSourceScoreParticipants)
		}
	}

	cloudContacts, _ := s.fetchCloudBridgeContacts(ctx, accountID)
	for _, resolved := range cloudContacts {
		if resolved == nil {
			continue
		}
		addCandidate(s.mapResolvedIdentifierToUser(resolved), contactSourceScoreCloudList)
	}

	if query != "" {
		for _, identifier := range buildIdentifierLookupCandidates(query) {
			resolved, _ := s.resolveCloudBridgeIdentifier(ctx, accountID, identifier)
			if resolved == nil {
				continue
			}
			addCandidate(s.mapResolvedIdentifierToUser(resolved), contactSourceScoreLookup)
		}
		resp, searchErr := s.rt.Client().Client.SearchUserDirectory(ctx, query, searchContactsMaxLimit)
		if searchErr == nil {
			for _, user := range resp.Results {
				addCandidate(s.mapDirectoryUserToContact(user), contactSourceScoreDirectory)
			}
		}
	}

	return mergeContactCandidates(candidates), nil
}

func mergeContactUsers(existing, incoming compat.User) compat.User {
	if existing.Username == "" {
		existing.Username = incoming.Username
	}
	if existing.PhoneNumber == "" {
		existing.PhoneNumber = incoming.PhoneNumber
	}
	if existing.Email == "" {
		existing.Email = incoming.Email
	}
	if existing.FullName == "" || existing.FullName == existing.ID {
		existing.FullName = incoming.FullName
	}
	if existing.ImgURL == "" {
		existing.ImgURL = incoming.ImgURL
	}
	existing.IsSelf = existing.IsSelf || incoming.IsSelf
	existing.CannotMessage = existing.CannotMessage || incoming.CannotMessage
	return existing
}

func normalizeContactUser(user compat.User) (compat.User, bool) {
	user.ID = strings.TrimSpace(user.ID)
	user.Username = normalizeUsername(user.Username)
	user.PhoneNumber = normalizePhoneNumber(user.PhoneNumber)
	user.Email = normalizeEmail(user.Email)
	user.FullName = strings.TrimSpace(user.FullName)
	user.ImgURL = strings.TrimSpace(user.ImgURL)
	if user.ID == "" {
		switch {
		case user.PhoneNumber != "":
			user.ID = user.PhoneNumber
		case user.Email != "":
			user.ID = user.Email
		case user.Username != "":
			user.ID = user.Username
		}
	}
	if user.ID == "" {
		return compat.User{}, false
	}
	if user.Username == "" && strings.HasPrefix(user.ID, "@") {
		user.Username = userIDLocalpart(user.ID)
	}
	if user.FullName == "" {
		user.FullName = user.ID
	}
	return user, true
}

func scoreContactForQuery(user compat.User, query string, baseScore int) int {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return baseScore
	}
	candidates := []string{
		strings.ToLower(strings.TrimSpace(user.ID)),
		strings.ToLower(strings.TrimSpace(user.FullName)),
		strings.ToLower(strings.TrimSpace(user.Username)),
		strings.ToLower(strings.TrimSpace(user.Email)),
		strings.ToLower(strings.TrimSpace(user.PhoneNumber)),
	}
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if candidate == query {
			return baseScore + 300
		}
	}
	for _, candidate := range candidates {
		if candidate != "" && strings.HasPrefix(candidate, query) {
			return baseScore + 200
		}
	}
	for _, candidate := range candidates {
		if candidate != "" && strings.Contains(candidate, query) {
			return baseScore + 100
		}
	}
	return -1
}

func contactCandidateKey(user compat.User) string {
	switch {
	case strings.TrimSpace(user.ID) != "":
		return "id:" + strings.ToLower(strings.TrimSpace(user.ID))
	case normalizePhoneNumber(user.PhoneNumber) != "":
		return "phone:" + normalizePhoneNumber(user.PhoneNumber)
	case normalizeEmail(user.Email) != "":
		return "email:" + normalizeEmail(user.Email)
	case normalizeUsername(user.Username) != "":
		return "username:" + normalizeUsername(user.Username)
	default:
		return ""
	}
}

func mergeContactCandidates(candidates []contactCandidate) []compat.User {
	merged := make(map[string]contactCandidate, len(candidates))
	for _, candidate := range candidates {
		key := candidate.Key
		if key == "" {
			continue
		}
		existing, ok := merged[key]
		if !ok {
			merged[key] = candidate
			continue
		}
		preferred := candidate.Score > existing.Score
		winner := existing.User
		loser := candidate.User
		if preferred {
			winner = candidate.User
			loser = existing.User
		}
		merged[key] = contactCandidate{
			User:  mergeContactUsers(winner, loser),
			Key:   key,
			Score: max(existing.Score, candidate.Score),
		}
	}

	contacts := make([]contactCandidate, 0, len(merged))
	for _, contact := range merged {
		contacts = append(contacts, contact)
	}
	sort.Slice(contacts, func(i, j int) bool {
		if contacts[i].Score != contacts[j].Score {
			return contacts[i].Score > contacts[j].Score
		}
		leftName := strings.ToLower(strings.TrimSpace(contacts[i].User.FullName))
		rightName := strings.ToLower(strings.TrimSpace(contacts[j].User.FullName))
		if leftName != rightName {
			return leftName < rightName
		}
		if contacts[i].User.Username != contacts[j].User.Username {
			return contacts[i].User.Username < contacts[j].User.Username
		}
		return contacts[i].User.ID < contacts[j].User.ID
	})

	items := make([]compat.User, 0, len(contacts))
	for _, candidate := range contacts {
		items = append(items, candidate.User)
	}
	return items
}

func (s *Server) mapDirectoryUserToContact(user *mautrix.UserDirectoryEntry) compat.User {
	if user == nil {
		return compat.User{}
	}
	fullName := strings.TrimSpace(user.DisplayName)
	if fullName == "" {
		fullName = user.UserID.String()
	}
	return compat.User{
		ID:            strings.TrimSpace(user.UserID.String()),
		Username:      userIDLocalpart(user.UserID.String()),
		FullName:      fullName,
		ImgURL:        user.AvatarURL.String(),
		CannotMessage: false,
		IsSelf:        user.UserID == s.rt.Client().Account.UserID,
	}
}

func (s *Server) mapResolvedIdentifierToUser(resolved *provisionutil.RespResolveIdentifier) compat.User {
	if resolved == nil {
		return compat.User{}
	}
	phoneNumber, email, username := parseRemoteContactIdentifiers(resolved.Identifiers)
	userID := strings.TrimSpace(string(resolved.MXID))
	if userID == "" {
		userID = strings.TrimSpace(string(resolved.ID))
	}
	if userID == "" {
		switch {
		case phoneNumber != "":
			userID = phoneNumber
		case email != "":
			userID = email
		default:
			userID = username
		}
	}

	selfUserID := ""
	if s.rt.Client() != nil && s.rt.Client().Account != nil {
		selfUserID = string(s.rt.Client().Account.UserID)
	}
	return compat.User{
		ID:            userID,
		Username:      username,
		PhoneNumber:   phoneNumber,
		Email:         email,
		FullName:      strings.TrimSpace(resolved.Name),
		ImgURL:        strings.TrimSpace(string(resolved.AvatarURL)),
		CannotMessage: false,
		IsSelf:        userIDMatches(userID, selfUserID),
	}
}

func splitDesktopAccountID(accountID string) (bridgeID, loginID string) {
	accountID = strings.TrimSpace(accountID)
	if accountID == "" {
		return "", ""
	}
	parts := strings.SplitN(accountID, "_", 2)
	if len(parts) != 2 {
		return "", ""
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
}

func (s *Server) fetchCloudBridgeContacts(ctx context.Context, accountID string) ([]*provisionutil.RespResolveIdentifier, error) {
	bridgeID, loginID := splitDesktopAccountID(accountID)
	if bridgeID == "" || loginID == "" || bridgeID == "matrix" {
		return nil, nil
	}
	cli := s.rt.Client()
	if cli == nil || cli.Client == nil || cli.Account == nil {
		return nil, nil
	}
	urlPath := cli.Client.BuildURLWithQuery(
		mautrix.ClientURLPath{"unstable", "com.beeper.bridge", bridgeID, "_matrix", "provision", "v3", "contacts"},
		map[string]string{
			"user_id":  string(cli.Account.UserID),
			"login_id": loginID,
		},
	)
	var resp provisionutil.RespGetContactList
	if _, err := cli.Client.MakeRequest(ctx, http.MethodGet, urlPath, nil, &resp); err != nil {
		return nil, nil
	}
	return resp.Contacts, nil
}

func (s *Server) resolveCloudBridgeIdentifier(ctx context.Context, accountID, identifier string) (*provisionutil.RespResolveIdentifier, error) {
	bridgeID, loginID := splitDesktopAccountID(accountID)
	identifier = strings.TrimSpace(identifier)
	if bridgeID == "" || loginID == "" || identifier == "" || bridgeID == "matrix" {
		return nil, nil
	}
	cli := s.rt.Client()
	if cli == nil || cli.Client == nil || cli.Account == nil {
		return nil, nil
	}
	urlPath := cli.Client.BuildURLWithQuery(
		mautrix.ClientURLPath{"unstable", "com.beeper.bridge", bridgeID, "_matrix", "provision", "v3", "resolve_identifier", identifier},
		map[string]string{
			"user_id":  string(cli.Account.UserID),
			"login_id": loginID,
		},
	)
	var resp provisionutil.RespResolveIdentifier
	if _, err := cli.Client.MakeRequest(ctx, http.MethodGet, urlPath, nil, &resp); err != nil {
		return nil, nil
	}
	return &resp, nil
}

func buildIdentifierLookupCandidates(query string) []string {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil
	}
	candidates := make([]string, 0, contactLookupMaxCandidates)
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		for _, existing := range candidates {
			if strings.EqualFold(existing, value) {
				return
			}
		}
		candidates = append(candidates, value)
	}
	add(query)
	add(normalizePhoneNumber(query))
	add(normalizeEmail(query))
	add(normalizeUsername(query))
	if strings.HasPrefix(query, "@") {
		add(strings.TrimPrefix(query, "@"))
	}
	if len(candidates) > contactLookupMaxCandidates {
		candidates = candidates[:contactLookupMaxCandidates]
	}
	return candidates
}

func normalizePhoneNumber(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	var builder strings.Builder
	for i, r := range value {
		if r >= '0' && r <= '9' {
			builder.WriteRune(r)
			continue
		}
		if r == '+' && i == 0 {
			builder.WriteRune(r)
		}
	}
	return builder.String()
}

func normalizeEmail(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	return value
}

func normalizeUsername(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return ""
	}
	if strings.HasPrefix(value, "@") {
		return strings.TrimPrefix(value, "@")
	}
	return value
}

func isLikelyEmail(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || strings.Contains(value, " ") {
		return false
	}
	at := strings.Index(value, "@")
	if at <= 0 || at == len(value)-1 {
		return false
	}
	domain := value[at+1:]
	return strings.Contains(domain, ".")
}

func isLikelyPhone(value string) bool {
	cleaned := normalizePhoneNumber(value)
	digits := strings.TrimPrefix(cleaned, "+")
	return len(digits) >= 7
}

func isLikelyUsername(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	value = strings.TrimPrefix(value, "@")
	if len(value) < 2 {
		return false
	}
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '.', r == '_', r == '-':
		default:
			return false
		}
	}
	return true
}

func parseRemoteContactIdentifiers(identifiers []string) (phoneNumber, email, username string) {
	for _, raw := range identifiers {
		identifier := strings.TrimSpace(raw)
		if identifier == "" {
			continue
		}
		switch {
		case phoneNumber == "" && strings.HasPrefix(identifier, "tel:"):
			phoneNumber = normalizePhoneNumber(strings.TrimPrefix(identifier, "tel:"))
		case email == "" && strings.HasPrefix(identifier, "mailto:"):
			email = normalizeEmail(strings.TrimPrefix(identifier, "mailto:"))
		case phoneNumber == "" && isLikelyPhone(identifier):
			phoneNumber = normalizePhoneNumber(identifier)
		case email == "" && isLikelyEmail(identifier):
			email = normalizeEmail(identifier)
		case username == "" && isLikelyUsername(identifier):
			username = normalizeUsername(identifier)
		}
	}
	return phoneNumber, email, username
}

func userIDLocalpart(userID string) string {
	left := strings.SplitN(strings.TrimSpace(userID), ":", 2)[0]
	return strings.TrimPrefix(left, "@")
}

func contactMatchesQuery(user compat.User, query string) bool {
	query = strings.TrimSpace(query)
	if query == "" {
		return true
	}
	haystack := strings.ToLower(strings.Join([]string{
		user.ID,
		user.Username,
		user.PhoneNumber,
		user.Email,
		user.FullName,
	}, " "))
	for _, token := range strings.Fields(strings.ToLower(query)) {
		if !strings.Contains(haystack, token) {
			return false
		}
	}
	return true
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
	_ = ctx
	_ = params
	return emptySearchMessagesOutput(), nil
}

func emptySearchMessagesOutput() compat.SearchMessagesOutput {
	return compat.SearchMessagesOutput{
		Items:   []compat.Message{},
		Chats:   map[string]compat.Chat{},
		HasMore: false,
	}
}

func (s *Server) loadSearchMessageEvents(ctx context.Context, params searchMessagesParams) ([]*database.Event, bool, error) {
	// Most message searches go backwards in history; iterate over multiple timeline pages so sparse
	// filters still find matches deeper in history.
	if params.Direction != "before" {
		fetchLimit := params.Limit * 8
		if fetchLimit < params.Limit+1 {
			fetchLimit = params.Limit + 1
		}
		if fetchLimit > 1000 {
			fetchLimit = 1000
		}
		return s.loadTimelineEventsGlobal(ctx, params.Cursor, params.Direction, fetchLimit)
	}

	events := make([]*database.Event, 0, min(searchMessagesScanMaxEvents, searchMessagesScanBatchSize*2))
	cursorValue := params.Cursor
	hasMore := false
	for batch := 0; batch < searchMessagesScanMaxBatches && len(events) < searchMessagesScanMaxEvents; batch++ {
		remaining := searchMessagesScanMaxEvents - len(events)
		limit := searchMessagesScanBatchSize
		if remaining < limit {
			limit = remaining
		}
		if limit <= 0 {
			break
		}

		page, pageHasMore, err := s.loadTimelineEventsGlobal(ctx, cursorValue, "before", limit)
		if err != nil {
			return nil, false, err
		}
		if len(page) == 0 {
			hasMore = false
			break
		}
		events = append(events, page...)
		hasMore = pageHasMore
		if !pageHasMore {
			break
		}
		cursorValue = int64(page[len(page)-1].TimelineRowID)
	}
	return events, hasMore, nil
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

func matchesMessageQuery(query string, msg compat.Message) bool {
	query = strings.TrimSpace(query)
	if query == "" {
		return true
	}
	tokens := strings.Fields(strings.ToLower(query))
	if len(tokens) == 0 {
		return true
	}

	baseText := strings.ToLower(msg.Text)
	looseText := normalizeLooseSearch(baseText)
	compactText := strings.ReplaceAll(looseText, " ", "")
	trimmedColons := strings.Trim(msg.Text, ":")
	looseTrimmedColons := normalizeLooseSearch(trimmedColons)

	haystacks := []string{
		baseText,
		looseText,
		compactText,
	}
	if msg.Type == compat.MessageType("REACTION") && strings.TrimSpace(trimmedColons) != "" {
		haystacks = append(haystacks, strings.ToLower(trimmedColons), looseTrimmedColons, strings.ReplaceAll(looseTrimmedColons, " ", ""))
	}

	for _, token := range tokens {
		looseToken := normalizeLooseSearch(token)
		compactToken := strings.ReplaceAll(looseToken, " ", "")
		matched := false
		for _, haystack := range haystacks {
			if haystack == "" {
				continue
			}
			if strings.Contains(haystack, token) {
				matched = true
				break
			}
			if looseToken != "" && strings.Contains(haystack, looseToken) {
				matched = true
				break
			}
			if compactToken != "" && strings.Contains(strings.ReplaceAll(haystack, " ", ""), compactToken) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

func normalizeLooseSearch(input string) string {
	var builder strings.Builder
	builder.Grow(len(input))
	for _, r := range strings.ToLower(input) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			builder.WriteRune(r)
		case r == '_', r == '-', r == ':', r == '/', r == '.', r == ' ':
			builder.WriteByte(' ')
		}
	}
	return strings.Join(strings.Fields(builder.String()), " ")
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
