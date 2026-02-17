package server

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"time"

	mcpauth "github.com/modelcontextprotocol/go-sdk/auth"
)

const (
	appVersion                 = "0.3.0"
	oauthAuthorizationCodeTTL  = 5 * time.Minute
	oauthAccessTokenTTL        = 24 * time.Hour
	oauthDefaultClientName     = "Unknown Client"
	oauthTokenTypeBearer       = "Bearer"
	oauthCodeChallengeMethodS2 = "S256"
)

type oauthClient struct {
	ClientID                string   `json:"client_id"`
	ClientName              string   `json:"client_name"`
	ClientURI               string   `json:"client_uri,omitempty"`
	RedirectURIs            []string `json:"redirect_uris"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
	Scope                   string   `json:"scope"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	CreatedAt               int64    `json:"created_at"`
}

type oauthAuthorizationCode struct {
	Code                string
	ClientID            string
	RedirectURI         string
	Scopes              []string
	State               string
	CodeChallenge       string
	CodeChallengeMethod string
	Resource            string
	CreatedAt           time.Time
	ExpiresAt           time.Time
}

type oauthAccessToken struct {
	Value         string
	TokenType     string
	ClientID      string
	Subject       string
	Scopes        []string
	CreatedAt     time.Time
	ExpiresAt     *time.Time
	RevokedAt     *time.Time
	Static        bool
	Resource      string
	ClientName    string
	ClientVersion string
}

func normalizeOAuthScopes(raw string) []string {
	parts := strings.Fields(raw)
	if len(parts) == 0 {
		return []string{"read"}
	}

	seen := map[string]struct{}{}
	out := make([]string, 0, len(parts)+1)
	for _, scope := range parts {
		scope = strings.TrimSpace(scope)
		if scope == "" {
			continue
		}
		switch scope {
		case "read", "write":
		default:
			continue
		}
		if _, ok := seen[scope]; ok {
			continue
		}
		seen[scope] = struct{}{}
		out = append(out, scope)
	}
	if len(out) == 0 {
		return []string{"read"}
	}
	if _, ok := seen["read"]; !ok {
		out = append([]string{"read"}, out...)
	}
	return out
}

func oauthScopeString(scopes []string) string {
	if len(scopes) == 0 {
		return "read"
	}
	return strings.Join(scopes, " ")
}

func verifyPKCES256(codeVerifier, codeChallenge string) bool {
	if codeVerifier == "" || codeChallenge == "" {
		return false
	}
	sum := sha256.Sum256([]byte(codeVerifier))
	encoded := base64.RawURLEncoding.EncodeToString(sum[:])
	return encoded == codeChallenge
}

func randomHexToken(byteLen int) (string, error) {
	if byteLen <= 0 {
		return "", fmt.Errorf("invalid token length: %d", byteLen)
	}
	buf := make([]byte, byteLen)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func parseAuthTokenFromRequest(r *http.Request) string {
	authz := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(authz, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(authz, "Bearer "))
	}
	if token := strings.TrimSpace(r.Header.Get("X-Beeper-Access-Token")); token != "" {
		return token
	}
	return ""
}

func (s *Server) initOAuthState(staticToken string) {
	now := time.Now().UTC()
	s.oauthTokens[staticToken] = oauthAccessToken{
		Value:      staticToken,
		TokenType:  oauthTokenTypeBearer,
		ClientID:   "gomuks-beeper-api-static",
		Subject:    s.oauthSubject,
		Scopes:     []string{"read", "write"},
		CreatedAt:  now,
		ExpiresAt:  nil,
		RevokedAt:  nil,
		Static:     true,
		ClientName: "gomuks-beeper-api",
	}
}

func (s *Server) tokenInfoForBearer(token string) (*mcpauth.TokenInfo, bool) {
	entry, ok := s.oauthTokenByValue(token)
	if !ok {
		return nil, false
	}
	tokenInfo := &mcpauth.TokenInfo{
		Scopes: entry.Scopes,
		UserID: entry.Subject,
		Extra: map[string]any{
			"client_id": entry.ClientID,
		},
	}
	if entry.ExpiresAt != nil {
		tokenInfo.Expiration = *entry.ExpiresAt
	} else {
		tokenInfo.Expiration = time.Now().Add(10 * 365 * 24 * time.Hour)
	}
	return tokenInfo, true
}

func (s *Server) oauthTokenByValue(token string) (oauthAccessToken, bool) {
	if strings.TrimSpace(token) == "" {
		return oauthAccessToken{}, false
	}
	s.oauthMu.RLock()
	defer s.oauthMu.RUnlock()
	entry, ok := s.oauthTokens[token]
	if !ok {
		return oauthAccessToken{}, false
	}
	if entry.RevokedAt != nil {
		return oauthAccessToken{}, false
	}
	if entry.ExpiresAt != nil && time.Now().After(*entry.ExpiresAt) {
		return oauthAccessToken{}, false
	}
	return entry, true
}

func (s *Server) issueOAuthAccessToken(clientID string, scopes []string, resource string) (oauthAccessToken, error) {
	tokenValue, err := randomHexToken(32)
	if err != nil {
		return oauthAccessToken{}, err
	}

	now := time.Now().UTC()
	expiresAt := now.Add(oauthAccessTokenTTL)

	s.oauthMu.Lock()
	client := s.oauthClients[clientID]
	entry := oauthAccessToken{
		Value:      tokenValue,
		TokenType:  oauthTokenTypeBearer,
		ClientID:   clientID,
		Subject:    s.oauthSubject,
		Scopes:     scopes,
		CreatedAt:  now,
		ExpiresAt:  &expiresAt,
		RevokedAt:  nil,
		Resource:   resource,
		ClientName: client.ClientName,
	}
	s.oauthTokens[tokenValue] = entry
	s.oauthMu.Unlock()

	return entry, nil
}

func (s *Server) createAuthorizationCode(
	clientID string,
	redirectURI string,
	scopes []string,
	state string,
	codeChallenge string,
	codeChallengeMethod string,
	resource string,
) (oauthAuthorizationCode, error) {
	codeValue, err := randomHexToken(24)
	if err != nil {
		return oauthAuthorizationCode{}, err
	}
	now := time.Now().UTC()
	code := oauthAuthorizationCode{
		Code:                codeValue,
		ClientID:            clientID,
		RedirectURI:         redirectURI,
		Scopes:              scopes,
		State:               state,
		CodeChallenge:       codeChallenge,
		CodeChallengeMethod: codeChallengeMethod,
		Resource:            resource,
		CreatedAt:           now,
		ExpiresAt:           now.Add(oauthAuthorizationCodeTTL),
	}

	s.oauthMu.Lock()
	s.oauthCodes[codeValue] = code
	s.oauthMu.Unlock()

	return code, nil
}

func (s *Server) popAuthorizationCode(codeValue string) (oauthAuthorizationCode, bool) {
	s.oauthMu.Lock()
	defer s.oauthMu.Unlock()
	code, ok := s.oauthCodes[codeValue]
	if !ok {
		return oauthAuthorizationCode{}, false
	}
	delete(s.oauthCodes, codeValue)
	if time.Now().After(code.ExpiresAt) {
		return oauthAuthorizationCode{}, false
	}
	return code, true
}
