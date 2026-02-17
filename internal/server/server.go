package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"

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

	oauthMu      sync.RWMutex
	oauthClients map[string]oauthClient
	oauthCodes   map[string]oauthAuthorizationCode
	oauthTokens  map[string]oauthAccessToken
	oauthSubject string

	ws *wsHub
}

type apiHandler func(http.ResponseWriter, *http.Request) error

func New(cfg config.Config, rt *gomuksruntime.Runtime) *Server {
	s := &Server{
		cfg:          cfg,
		rt:           rt,
		auth:         auth.New(cfg.AccessToken, cfg.AllowQueryTokenAuth),
		oauthClients: make(map[string]oauthClient),
		oauthCodes:   make(map[string]oauthAuthorizationCode),
		oauthTokens:  make(map[string]oauthAccessToken),
		oauthSubject: "local-user",
	}
	s.initOAuthState(cfg.AccessToken)
	s.auth.SetExtraValidator(s.validateBearerToken)
	s.ws = newWSHub(s)
	return s
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.Handle("GET /v1/spec", s.public(s.openAPISpec))
	mux.Handle("GET /v0/spec", s.public(s.openAPISpecRedirect))
	mux.Handle("GET /v1/info", s.public(s.info))
	mux.Handle("GET /.well-known/oauth-protected-resource", s.public(s.oauthProtectedResourceMetadata))
	mux.Handle("GET /.well-known/oauth-protected-resource/", s.public(s.oauthProtectedResourceMetadata))
	mux.Handle("GET /.well-known/oauth-authorization-server", s.public(s.oauthAuthorizationServerMetadata))
	mux.Handle("GET /oauth/authorize", s.public(s.oauthAuthorize))
	mux.Handle("POST /oauth/authorize/callback", s.public(s.oauthAuthorizeCallback))
	mux.Handle("POST /oauth/token", s.public(s.oauthToken))
	mux.Handle("GET /oauth/userinfo", s.public(s.oauthUserInfo))
	mux.Handle("POST /oauth/revoke", s.public(s.oauthRevoke))
	mux.Handle("POST /oauth/introspect", s.public(s.oauthIntrospect))
	mux.Handle("POST /oauth/register", s.public(s.oauthRegister))
	mux.Handle("POST /register", s.public(s.oauthRegister))
	mux.Handle("GET /deeplink", s.public(s.deeplink))
	mux.Handle("GET /deeplink/", s.public(s.deeplink))
	mux.Handle("GET /focus", s.public(s.focusPage))
	mux.Handle("GET /focus/{chatID}", s.public(s.focusPage))
	mux.Handle("GET /focus/{chatID}/{messageID}", s.public(s.focusPage))

	s.handle(mux, "GET /v1/accounts", s.getAccounts, false)
	s.handle(mux, "GET /v0/get-accounts", s.getAccounts, false)

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
	s.handle(mux, "GET /v0/search-messages", s.searchMessages, false)
	s.handle(mux, "GET /v0/search-chats", s.searchChats, false)
	s.handle(mux, "GET /v0/get-chat", s.getChat, false)
	s.handle(mux, "POST /v0/create-chat", s.createChat, false)
	s.handle(mux, "POST /v0/archive-chat", s.archiveChat, false)
	s.handle(mux, "POST /v0/set-chat-reminder", s.setChatReminder, false)
	s.handle(mux, "POST /v0/clear-chat-reminder", s.clearChatReminder, false)
	s.handle(mux, "POST /v0/send-message", s.sendMessage, false)
	s.handle(mux, "GET /v1/ws", s.wsEvents, true)
	s.handle(mux, "GET /ws", s.wsEvents, true)

	s.handle(mux, "POST /v1/assets/download", s.downloadAsset, false)
	s.handle(mux, "POST /v0/download-asset", s.downloadAsset, false)
	s.handle(mux, "GET /v1/assets/serve", s.serveAsset, true)
	s.handle(mux, "POST /v1/assets/upload", s.uploadAsset, false)
	s.handle(mux, "POST /v1/assets/upload/base64", s.uploadAsset, false)

	s.handle(mux, "GET /v1/accounts/{accountID}/contacts", s.searchContacts, false)
	s.handle(mux, "GET /v1/accounts/{accountID}/contacts/list", s.listContacts, false)
	s.handle(mux, "GET /v1/search", s.search, false)
	s.handle(mux, "GET /v0/search", s.search, false)
	s.handle(mux, "POST /v1/focus", s.focusApp, false)
	s.handle(mux, "POST /v0/focus-app", s.focusApp, false)
	s.handle(mux, "POST /v0/open-app", s.focusApp, false)

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

func (s *Server) public(handler apiHandler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
