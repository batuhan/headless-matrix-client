package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/batuhan/gomuks-beeper-api/internal/auth"
	"github.com/batuhan/gomuks-beeper-api/internal/config"
	"github.com/batuhan/gomuks-beeper-api/internal/cursor"
	errs "github.com/batuhan/gomuks-beeper-api/internal/errors"
	"github.com/batuhan/gomuks-beeper-api/internal/gomuksruntime"
)

type Server struct {
	cfg  config.Config
	rt   *gomuksruntime.Runtime
	auth *auth.Middleware
}

type apiHandler func(http.ResponseWriter, *http.Request) error

func New(cfg config.Config, rt *gomuksruntime.Runtime) *Server {
	return &Server{
		cfg:  cfg,
		rt:   rt,
		auth: auth.New(cfg.AccessToken, cfg.AllowQueryTokenAuth),
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	s.handle(mux, "GET /v1/accounts", s.getAccounts, false)

	s.handle(mux, "GET /v1/chats", s.listChats, false)
	s.handle(mux, "POST /v1/chats", s.createChat, false)
	s.handle(mux, "GET /v1/chats/{chatID}", s.getChat, false)
	s.handle(mux, "GET /v1/chats/search", s.searchChats, false)
	s.handle(mux, "POST /v1/chats/{chatID}/archive", s.archiveChat, false)
	s.handle(mux, "POST /v1/chats/{chatID}/reminders", s.setChatReminder, false)
	s.handle(mux, "DELETE /v1/chats/{chatID}/reminders", s.clearChatReminder, false)

	s.handle(mux, "GET /v1/chats/{chatID}/messages", s.listMessages, false)
	s.handle(mux, "POST /v1/chats/{chatID}/messages", s.sendMessage, false)
	s.handle(mux, "PUT /v1/chats/{chatID}/messages/{messageID}", s.editMessage, false)
	s.handle(mux, "POST /v1/chats/{chatID}/messages/{messageID}/reactions", s.addReaction, false)
	s.handle(mux, "DELETE /v1/chats/{chatID}/messages/{messageID}/reactions", s.removeReaction, false)
	s.handle(mux, "GET /v1/messages/search", s.searchMessages, false)

	s.handle(mux, "POST /v1/assets/download", s.downloadAsset, false)
	s.handle(mux, "GET /v1/assets/serve", s.serveAsset, true)
	s.handle(mux, "POST /v1/assets/upload", s.uploadAsset, false)
	s.handle(mux, "POST /v1/assets/upload/base64", s.uploadAsset, false)

	s.handle(mux, "GET /v1/accounts/{accountID}/contacts", s.searchContacts, false)
	s.handle(mux, "GET /v1/accounts/{accountID}/contacts/list", s.listContacts, false)
	s.handle(mux, "GET /v1/search", s.search, false)
	s.handle(mux, "POST /v1/focus", s.focusApp, false)

	return mux
}

func (s *Server) handle(mux *http.ServeMux, pattern string, handler apiHandler, allowQueryToken bool) {
	wrapped := s.wrap(handler)
	mux.Handle(pattern, s.auth.Wrap(wrapped, allowQueryToken))
}

func (s *Server) wrap(handler apiHandler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := s.requireBeeperHomeserver(); err != nil {
			errs.Write(w, err)
			return
		}
		if err := handler(w, r); err != nil {
			errs.Write(w, err)
		}
	})
}

func (s *Server) requireBeeperHomeserver() error {
	cli := s.rt.Client()
	if cli == nil || cli.Account == nil || cli.Client == nil || cli.Client.HomeserverURL == nil {
		return errs.Forbidden("A logged-in Beeper Matrix session is required")
	}
	hostname := strings.ToLower(strings.TrimSpace(cli.Client.HomeserverURL.Hostname()))
	switch {
	case hostname == "matrix.beeper.com",
		hostname == "matrix.beeper-staging.com",
		hostname == "matrix.beeper-dev.com":
		return nil
	default:
		return errs.Forbidden("Only Beeper homeserver sessions are supported")
	}
}

func writeJSON(w http.ResponseWriter, value any) error {
	w.Header().Set("Content-Type", "application/json")
	return json.NewEncoder(w).Encode(value)
}

func decodeJSON(r *http.Request, out any) error {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		return errs.Validation(map[string]any{"error": err.Error()})
	}
	return nil
}

func readChatID(r *http.Request, bodyChatID string) string {
	if chatID := r.PathValue("chatID"); chatID != "" {
		return chatID
	}
	if bodyChatID != "" {
		return bodyChatID
	}
	if chatID := r.URL.Query().Get("chatID"); chatID != "" {
		return chatID
	}
	return ""
}

func parseDirection(raw string) (string, error) {
	direction := strings.TrimSpace(raw)
	if direction == "" {
		return "before", nil
	}
	if direction != "before" && direction != "after" {
		return "", errs.Validation(map[string]any{"direction": "must be one of: before, after"})
	}
	return direction, nil
}

func parseParticipantLimit(raw string) (int, error) {
	if raw == "" {
		return -1, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil {
		return 0, errs.Validation(map[string]any{"maxParticipantCount": "must be an integer"})
	}
	if limit < -1 || limit > 500 {
		return 0, errs.Validation(map[string]any{"maxParticipantCount": "must be between -1 and 500"})
	}
	return limit, nil
}

func parseMessageCursor(raw string) (int64, error) {
	if raw == "" {
		return 0, nil
	}
	if rowID, err := strconv.ParseInt(raw, 10, 64); err == nil {
		return rowID, nil
	}
	var decoded cursor.MessageCursor
	if err := cursor.Decode(raw, &decoded); err != nil {
		return 0, errs.Validation(map[string]any{"cursor": err.Error()})
	}
	if decoded.TimelineRowID == 0 {
		return 0, errs.Validation(map[string]any{"cursor": "timeline_row_id is required"})
	}
	return decoded.TimelineRowID, nil
}

func parseChatCursor(raw string) (*cursor.ChatCursor, error) {
	if raw == "" {
		return nil, nil
	}
	if ts, err := strconv.ParseInt(raw, 10, 64); err == nil {
		return &cursor.ChatCursor{TS: ts}, nil
	}
	var decoded cursor.ChatCursor
	if err := cursor.Decode(raw, &decoded); err != nil {
		return nil, errs.Validation(map[string]any{"cursor": err.Error()})
	}
	if decoded.TS == 0 {
		return nil, errs.Validation(map[string]any{"cursor": "ts is required"})
	}
	return &decoded, nil
}

func parseAccountIDs(r *http.Request) []string {
	var accountIDs []string
	for _, raw := range r.URL.Query()["accountIDs"] {
		for _, part := range strings.Split(raw, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				accountIDs = append(accountIDs, part)
			}
		}
	}
	return accountIDs
}

func equalsAny(value string, candidates []string) bool {
	for _, candidate := range candidates {
		if value == candidate {
			return true
		}
	}
	return false
}

func firstErr(errsToCheck ...error) error {
	for _, err := range errsToCheck {
		if err != nil && !errors.Is(err, http.ErrNotSupported) {
			return err
		}
	}
	return nil
}
