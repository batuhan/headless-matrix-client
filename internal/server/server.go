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
	s.handle(mux, "GET /v0/get-accounts", s.getAccounts, false)

	s.handle(mux, "GET /v1/chats", s.listChats, false)
	s.handle(mux, "GET /v1/chats/{chatID}", s.getChat, false)
	s.handle(mux, "GET /v0/get-chat", s.getChat, false)

	s.handle(mux, "GET /v1/chats/{chatID}/messages", s.listMessages, false)
	s.handle(mux, "POST /v1/chats/{chatID}/messages", s.sendMessage, false)
	s.handle(mux, "POST /v0/send-message", s.sendMessage, false)
	s.handle(mux, "PUT /v1/chats/{chatID}/messages/{messageID}", s.editMessage, false)
	s.handle(mux, "POST /v1/chats/{chatID}/messages/{messageID}/reactions", s.addReaction, false)
	s.handle(mux, "DELETE /v1/chats/{chatID}/messages/{messageID}/reactions", s.removeReaction, false)

	s.handle(mux, "POST /v1/assets/download", s.downloadAsset, false)
	s.handle(mux, "POST /v0/download-asset", s.downloadAsset, false)
	s.handle(mux, "GET /v1/assets/serve", s.serveAsset, true)
	s.handle(mux, "POST /v1/assets/upload", s.uploadAsset, false)
	s.handle(mux, "POST /v1/assets/upload/base64", s.uploadAsset, false)

	s.handleUnsupported(mux, "GET /v1/messages/search", "GET /v0/search-messages")
	s.handleUnsupported(mux, "GET /v1/chats/search", "GET /v0/search-chats")
	s.handleUnsupported(mux, "GET /v1/accounts/{accountID}/contacts", "GET /v0/search-users")
	s.handleUnsupported(mux, "GET /v1/search", "GET /v0/search")
	s.handleUnsupported(mux, "POST /v1/focus", "POST /v0/focus-app")
	s.handleUnsupported(mux, "POST /v1/chats", "POST /v0/create-chat")
	s.handleUnsupported(mux, "POST /v1/chats/{chatID}/archive", "POST /v0/archive-chat")
	s.handleUnsupported(mux, "POST /v1/chats/{chatID}/reminders", "POST /v0/set-chat-reminder")
	s.handleUnsupported(mux, "DELETE /v1/chats/{chatID}/reminders", "DELETE /v0/clear-chat-reminder")

	return mux
}

func (s *Server) handle(mux *http.ServeMux, pattern string, handler apiHandler, allowQueryToken bool) {
	wrapped := s.wrap(handler)
	mux.Handle(pattern, s.auth.Wrap(wrapped, allowQueryToken))
}

func (s *Server) handleUnsupported(mux *http.ServeMux, patterns ...string) {
	for _, pattern := range patterns {
		s.handle(mux, pattern, func(w http.ResponseWriter, _ *http.Request) error {
			return errs.NotImplemented("This endpoint is not implemented in gomuks-beeper-api")
		}, false)
	}
}

func (s *Server) wrap(handler apiHandler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := handler(w, r); err != nil {
			errs.Write(w, err)
		}
	})
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
