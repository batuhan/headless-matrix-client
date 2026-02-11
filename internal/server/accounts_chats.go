package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"go.mau.fi/gomuks/pkg/hicli/database"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"

	"github.com/batuhan/gomuks-beeper-api/internal/compat"
	"github.com/batuhan/gomuks-beeper-api/internal/cursor"
	errs "github.com/batuhan/gomuks-beeper-api/internal/errors"
)

const (
	localBridgeStateEventType = "com.beeper.local_bridge_state"
	chatPageSize              = 25
	chatPreviewParticipants   = 5
)

const roomSelectBaseQuery = `
	SELECT room_id, creation_content, tombstone_content, name, name_quality,
	       avatar, explicit_avatar, dm_user_id, topic, canonical_alias,
	       lazy_load_summary, encryption_event, has_member_list, preview_event_rowid, sorting_timestamp,
	       unread_highlights, unread_notifications, unread_messages, marked_unread, prev_batch
	FROM room
`

const roomSelectSortedQuery = roomSelectBaseQuery + `WHERE sorting_timestamp > 0 AND room_type<>'m.space' ORDER BY sorting_timestamp DESC, room_id ASC`

type localBridgeDeviceState struct {
	State string `json:"state"`
}

type localBridgeAccount struct {
	State       string                            `json:"state"`
	ProfileData map[string]any                    `json:"profile_data,omitempty"`
	Devices     map[string]localBridgeDeviceState `json:"devices,omitempty"`
}

type localBridgeStateContent map[string]map[string]localBridgeAccount

type accountLookup struct {
	Accounts []compat.Account
	ByID     map[string]compat.Account
	ByBridge map[string][]compat.Account
}

func (s *Server) getAccounts(w http.ResponseWriter, r *http.Request) error {
	accounts, err := s.loadAccounts(r.Context())
	if err != nil {
		return err
	}
	return writeJSON(w, accounts)
}

func (s *Server) buildAccountLookup(ctx context.Context) (*accountLookup, error) {
	accounts, err := s.loadAccounts(ctx)
	if err != nil {
		return nil, err
	}
	lookup := &accountLookup{
		Accounts: accounts,
		ByID:     make(map[string]compat.Account, len(accounts)),
		ByBridge: make(map[string][]compat.Account),
	}
	for _, account := range accounts {
		lookup.ByID[account.AccountID] = account
		bridgeID := bridgeIDFromAccountID(account.AccountID)
		if bridgeID != "" {
			lookup.ByBridge[bridgeID] = append(lookup.ByBridge[bridgeID], account)
		}
	}
	return lookup, nil
}

func (s *Server) loadAccounts(ctx context.Context) ([]compat.Account, error) {
	cli := s.rt.Client()
	if cli == nil || cli.Account == nil {
		return []compat.Account{}, nil
	}

	accountDataEvents, err := cli.DB.AccountData.GetAllGlobal(ctx, cli.Account.UserID)
	if err != nil {
		return nil, errs.Internal(fmt.Errorf("failed to read global account data: %w", err))
	}

	var state localBridgeStateContent
	for _, ad := range accountDataEvents {
		if ad.Type != localBridgeStateEventType {
			continue
		}
		if len(ad.Content) == 0 {
			continue
		}
		if err = json.Unmarshal(ad.Content, &state); err != nil {
			return nil, errs.Internal(fmt.Errorf("failed to parse %s: %w", localBridgeStateEventType, err))
		}
		break
	}

	accounts := make([]compat.Account, 0)
	currentDeviceID := string(cli.Account.DeviceID)

	bridgeIDs := make([]string, 0, len(state))
	for bridgeID := range state {
		bridgeIDs = append(bridgeIDs, bridgeID)
	}
	sort.Strings(bridgeIDs)

	for _, bridgeID := range bridgeIDs {
		accountsByRemote := state[bridgeID]
		remoteIDs := make([]string, 0, len(accountsByRemote))
		for remoteID := range accountsByRemote {
			remoteIDs = append(remoteIDs, remoteID)
		}
		sort.Strings(remoteIDs)

		for _, remoteID := range remoteIDs {
			bridgeAccount := accountsByRemote[remoteID]
			if !isConfiguredLocalAccount(bridgeAccount, currentDeviceID) {
				continue
			}

			desktopAccountID := bridgeID + "_" + remoteID
			network := networkFromBridgeID(bridgeID)
			fullName := firstString(bridgeAccount.ProfileData, "name", "display_name", "displayName")
			if fullName == "" {
				fullName = remoteID
			}
			self := true
			cannotMessage := false
			accounts = append(accounts, compat.Account{
				AccountID: desktopAccountID,
				Network:   network,
				User: compat.User{
					ID:            remoteID,
					Username:      firstString(bridgeAccount.ProfileData, "username", "handle"),
					PhoneNumber:   firstString(bridgeAccount.ProfileData, "phone", "phone_number"),
					Email:         firstString(bridgeAccount.ProfileData, "email"),
					FullName:      fullName,
					ImgURL:        firstString(bridgeAccount.ProfileData, "avatar", "avatar_url"),
					CannotMessage: &cannotMessage,
					IsSelf:        &self,
				},
			})
		}
	}

	if len(accounts) == 0 {
		self := true
		cannotMessage := false
		accounts = append(accounts, compat.Account{
			AccountID: "matrix_" + string(cli.Account.UserID),
			Network:   "Matrix",
			User: compat.User{
				ID:            string(cli.Account.UserID),
				FullName:      string(cli.Account.UserID),
				CannotMessage: &cannotMessage,
				IsSelf:        &self,
			},
		})
	}

	return accounts, nil
}

func isConfiguredLocalAccount(account localBridgeAccount, deviceID string) bool {
	state := strings.ToUpper(strings.TrimSpace(account.State))
	if state == "" || state == "DELETED" {
		return false
	}
	if deviceID == "" {
		return true
	}
	if len(account.Devices) == 0 {
		return false
	}
	deviceState, ok := account.Devices[deviceID]
	if !ok {
		return false
	}
	status := strings.ToUpper(strings.TrimSpace(deviceState.State))
	return status != "DELETED" && status != "LOGGED_OUT"
}

func bridgeIDFromAccountID(accountID string) string {
	if idx := strings.Index(accountID, "_"); idx > 0 {
		return accountID[:idx]
	}
	return ""
}

func (s *Server) listChats(w http.ResponseWriter, r *http.Request) error {
	lookup, err := s.buildAccountLookup(r.Context())
	if err != nil {
		return err
	}
	direction, err := parseDirection(r.URL.Query().Get("direction"))
	if err != nil {
		return err
	}
	cursorValue, err := parseChatCursor(r.URL.Query().Get("cursor"))
	if err != nil {
		return err
	}
	accountIDs := parseAccountIDs(r)
	rooms, err := s.loadRoomsSorted(r.Context())
	if err != nil {
		return err
	}

	items := make([]compat.Chat, 0, chatPageSize+1)
	for _, room := range rooms {
		if cursorValue != nil {
			if direction == "before" && !roomIsOlderThanCursor(room, cursorValue) {
				continue
			}
			if direction == "after" && !roomIsNewerThanCursor(room, cursorValue) {
				continue
			}
		}
		chat, mapErr := s.mapRoomToChat(r.Context(), room, lookup, chatPreviewParticipants, true)
		if mapErr != nil {
			continue
		}
		if len(accountIDs) > 0 && !equalsAny(chat.AccountID, accountIDs) {
			continue
		}
		items = append(items, chat)
		if len(items) > chatPageSize {
			break
		}
	}

	hasMore := len(items) > chatPageSize
	if hasMore {
		items = items[:chatPageSize]
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

	return writeJSON(w, compat.ListChatsOutput{
		Items:        items,
		HasMore:      hasMore,
		OldestCursor: oldestCursor,
		NewestCursor: newestCursor,
	})
}

func (s *Server) getChat(w http.ResponseWriter, r *http.Request) error {
	chatID := readChatID(r, "")
	if chatID == "" {
		return errs.Validation(map[string]any{"chatID": "chatID is required"})
	}
	maxParticipants, err := parseParticipantLimit(r.URL.Query().Get("maxParticipantCount"))
	if err != nil {
		return err
	}
	lookup, err := s.buildAccountLookup(r.Context())
	if err != nil {
		return err
	}
	cli := s.rt.Client()
	room, err := cli.DB.Room.Get(r.Context(), id.RoomID(chatID))
	if err != nil {
		return errs.Internal(fmt.Errorf("failed to read room metadata: %w", err))
	}
	if room == nil {
		return errs.NotFound("Chat not found")
	}
	chat, err := s.mapRoomToChat(r.Context(), room, lookup, maxParticipants, true)
	if err != nil {
		return err
	}
	return writeJSON(w, chat)
}

func (s *Server) loadRoomsSorted(ctx context.Context) ([]*database.Room, error) {
	cli := s.rt.Client()
	rows, err := cli.DB.Query(ctx, roomSelectSortedQuery)
	if err != nil {
		return nil, errs.Internal(fmt.Errorf("failed to query rooms: %w", err))
	}
	defer rows.Close()

	rooms := make([]*database.Room, 0)
	for rows.Next() {
		room := &database.Room{}
		if _, scanErr := room.Scan(rows); scanErr != nil {
			return nil, errs.Internal(fmt.Errorf("failed to scan room: %w", scanErr))
		}
		rooms = append(rooms, room)
	}
	if err = rows.Err(); err != nil {
		return nil, errs.Internal(fmt.Errorf("room query failed: %w", err))
	}
	return rooms, nil
}

func roomIsOlderThanCursor(room *database.Room, c *cursor.ChatCursor) bool {
	ts := room.SortingTimestamp.UnixMilli()
	if ts < c.TS {
		return true
	}
	if ts > c.TS {
		return false
	}
	if c.RoomID == "" {
		return false
	}
	return string(room.ID) > c.RoomID
}

func roomIsNewerThanCursor(room *database.Room, c *cursor.ChatCursor) bool {
	ts := room.SortingTimestamp.UnixMilli()
	if ts > c.TS {
		return true
	}
	if ts < c.TS {
		return false
	}
	if c.RoomID == "" {
		return false
	}
	return string(room.ID) < c.RoomID
}

func (s *Server) mapRoomToChat(ctx context.Context, room *database.Room, lookup *accountLookup, maxParticipants int, includePreview bool) (compat.Chat, error) {
	accountID, network := inferAccountForRoom(room.ID, lookup)
	participants, total := s.loadRoomParticipants(ctx, room)
	filteredParticipants := participants
	hasMoreParticipants := false
	if maxParticipants >= 0 && len(filteredParticipants) > maxParticipants {
		filteredParticipants = filteredParticipants[:maxParticipants]
		hasMoreParticipants = true
	}

	title := strings.TrimSpace(ptrString(room.Name))
	if title == "" {
		title = string(room.ID)
	}
	chatType := "group"
	if room.DMUserID != nil && *room.DMUserID != "" {
		chatType = "single"
	}

	chat := compat.Chat{
		ID:        string(room.ID),
		AccountID: accountID,
		Network:   network,
		Title:     title,
		Type:      chatType,
		Participants: compat.Participants{
			Items:   filteredParticipants,
			HasMore: hasMoreParticipants,
			Total:   total,
		},
		UnreadCount: room.UnreadMessages,
		IsArchived:  false,
		IsMuted:     false,
		IsPinned:    false,
	}

	if ts := room.SortingTimestamp.UnixMilli(); ts > 0 {
		chat.LastActivity = time.UnixMilli(ts).UTC().Format(time.RFC3339)
	}

	if includePreview && room.PreviewEventRowID > 0 {
		if previewEvt, err := s.rt.Client().DB.Event.GetByRowID(ctx, room.PreviewEventRowID); err == nil && previewEvt != nil {
			if preview, mapErr := s.mapEventToMessage(ctx, previewEvt, room, lookup, reactionBundle{}); mapErr == nil {
				chat.Preview = &preview
			}
		}
	}

	return chat, nil
}

func (s *Server) loadRoomParticipants(ctx context.Context, room *database.Room) ([]compat.User, int) {
	cli := s.rt.Client()
	memberEvents, err := cli.DB.CurrentState.GetMembers(ctx, room.ID)
	if err != nil {
		return []compat.User{}, 0
	}

	users := make([]compat.User, 0, len(memberEvents))
	seen := make(map[string]struct{}, len(memberEvents))

	for _, memberEvt := range memberEvents {
		if memberEvt.StateKey == nil || *memberEvt.StateKey == "" {
			continue
		}
		var content event.MemberEventContent
		if err = json.Unmarshal(memberEvt.GetContent(), &content); err != nil {
			continue
		}
		if content.Membership != event.MembershipJoin && content.Membership != event.MembershipInvite {
			continue
		}
		userID := *memberEvt.StateKey
		if _, ok := seen[userID]; ok {
			continue
		}
		seen[userID] = struct{}{}
		cannotMessage := false
		isSelf := userID == string(cli.Account.UserID)
		fullName := strings.TrimSpace(content.Displayname)
		if fullName == "" {
			fullName = userID
		}
		users = append(users, compat.User{
			ID:            userID,
			FullName:      fullName,
			ImgURL:        string(content.AvatarURL),
			CannotMessage: &cannotMessage,
			IsSelf:        &isSelf,
		})
	}

	sort.Slice(users, func(i, j int) bool {
		if users[i].FullName != users[j].FullName {
			return users[i].FullName < users[j].FullName
		}
		return users[i].ID < users[j].ID
	})

	return users, len(users)
}

func inferAccountForRoom(roomID id.RoomID, lookup *accountLookup) (string, string) {
	if lookup == nil || len(lookup.Accounts) == 0 {
		return "", "Unknown"
	}
	server := roomServerPart(string(roomID))
	bridgeIDs := make([]string, 0, len(lookup.ByBridge))
	for bridgeID := range lookup.ByBridge {
		bridgeIDs = append(bridgeIDs, bridgeID)
	}
	sort.Slice(bridgeIDs, func(i, j int) bool {
		return len(bridgeIDs[i]) > len(bridgeIDs[j])
	})

	for _, bridgeID := range bridgeIDs {
		idx := strings.Index(server, bridgeID)
		if idx < 0 {
			continue
		}
		prefix := strings.Trim(server[:idx], "._-")
		if prefix != "" {
			candidate := bridgeID + "_" + prefix
			if account, ok := lookup.ByID[candidate]; ok {
				return account.AccountID, account.Network
			}
		}
		accounts := lookup.ByBridge[bridgeID]
		if len(accounts) > 0 {
			return accounts[0].AccountID, accounts[0].Network
		}
	}

	fallback := lookup.Accounts[0]
	return fallback.AccountID, fallback.Network
}

func roomServerPart(roomID string) string {
	parts := strings.SplitN(roomID, ":", 2)
	if len(parts) != 2 {
		return ""
	}
	return parts[1]
}

func ptrString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func firstString(m map[string]any, keys ...string) string {
	for _, key := range keys {
		value, ok := m[key]
		if !ok {
			continue
		}
		if s, ok := value.(string); ok {
			s = strings.TrimSpace(s)
			if s != "" {
				return s
			}
		}
	}
	return ""
}

func mustParseRFC3339(raw string) int64 {
	if raw == "" {
		return 0
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return 0
	}
	return parsed.UnixMilli()
}

func networkFromBridgeID(bridgeID string) string {
	if strings.HasPrefix(bridgeID, "local-") {
		bridgeID = strings.TrimPrefix(bridgeID, "local-")
	}
	switch bridgeID {
	case "whatsapp":
		return "WhatsApp"
	case "telegram":
		return "Telegram"
	case "twitter":
		return "Twitter/X"
	case "instagram":
		return "Instagram"
	case "signal":
		return "Signal"
	case "linkedin":
		return "LinkedIn"
	case "discordgo", "discord":
		return "Discord"
	case "slackgo", "slack":
		return "Slack"
	case "facebookgo", "facebook":
		return "Facebook"
	case "gmessages":
		return "Google Messages"
	case "gvoice":
		return "Google Voice"
	case "imessage", "imessagecloud":
		return "iMessage"
	default:
		if bridgeID == "" {
			return "Unknown"
		}
		return strings.ToUpper(bridgeID[:1]) + bridgeID[1:]
	}
}
