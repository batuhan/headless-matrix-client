package server

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"runtime"
	"strconv"
	"strings"
	"time"

	errs "github.com/batuhan/easymatrix/internal/errors"
	mcpauth "github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
)

func (s *Server) requestBaseURL(r *http.Request) string {
	proto := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")[0])
	if proto == "" {
		if r.TLS != nil {
			proto = "https"
		} else {
			proto = "http"
		}
	}
	host := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Host"), ",")[0])
	if host == "" {
		host = strings.TrimSpace(r.Host)
	}
	if host == "" {
		host = s.cfg.ListenAddr
	}
	return proto + "://" + host
}

func renderSimpleHTML(title, body string) string {
	return fmt.Sprintf(`<!doctype html>
<html>
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width,initial-scale=1">
  <title>%s</title>
  <style>
    body{font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,Arial,sans-serif;background:#f7f8fa;color:#111827;margin:0;display:flex;align-items:center;justify-content:center;min-height:100vh}
    .card{background:#fff;border:1px solid #e5e7eb;border-radius:12px;padding:20px 24px;max-width:560px;width:calc(100%% - 32px);box-shadow:0 8px 24px rgba(15,23,42,.08)}
    h1{font-size:18px;line-height:1.4;margin:0 0 8px}
    p{margin:0;color:#4b5563}
  </style>
</head>
<body><div class="card"><h1>%s</h1><p>%s</p></div></body>
</html>`, title, title, body)
}

func (s *Server) openAPISpec(w http.ResponseWriter, r *http.Request) error {
	http.Redirect(w, r, "https://developers.beeper.com/desktop-api", http.StatusTemporaryRedirect)
	return nil
}

func (s *Server) openAPISpecRedirect(w http.ResponseWriter, r *http.Request) error {
	http.Redirect(w, r, "/v1/spec", http.StatusMovedPermanently)
	return nil
}

func (s *Server) info(w http.ResponseWriter, r *http.Request) error {
	baseURL := s.requestBaseURL(r)
	serverStatus := "ready"
	if err := s.requireBeeperHomeserver(); err != nil {
		serverStatus = "not_ready"
	}
	listenHost, listenPort, splitErr := net.SplitHostPort(s.cfg.ListenAddr)
	if splitErr != nil {
		listenHost = s.cfg.ListenAddr
		listenPort = ""
	}
	if strings.TrimSpace(listenHost) == "" {
		listenHost = "localhost"
	}
	response := map[string]any{
		"app": map[string]any{
			"name":      "EasyMatrix",
			"version":   appVersion,
			"bundle_id": "com.beeper.desktop",
		},
		"platform": map[string]any{
			"os":      runtime.GOOS,
			"arch":    runtime.GOARCH,
			"release": runtime.Version(),
		},
		"server": map[string]any{
			"status":        serverStatus,
			"base_url":      baseURL,
			"port":          listenPort,
			"hostname":      listenHost,
			"remote_access": false,
			"mcp_enabled":   false,
		},
		"endpoints": map[string]any{
			"oauth": map[string]any{
				"authorization_endpoint": baseURL + "/oauth/authorize",
				"token_endpoint":         baseURL + "/oauth/token",
				"introspection_endpoint": baseURL + "/oauth/introspect",
				"userinfo_endpoint":      baseURL + "/oauth/userinfo",
				"revocation_endpoint":    baseURL + "/oauth/revoke",
				"registration_endpoint":  baseURL + "/oauth/register",
			},
			"spec":      baseURL + "/v1/spec",
			"ws_events": baseURL + "/v1/ws",
		},
	}
	return writeJSON(w, response)
}

func (s *Server) oauthProtectedResourceMetadata(w http.ResponseWriter, r *http.Request) error {
	suffix := strings.TrimPrefix(r.URL.Path, "/.well-known/oauth-protected-resource")
	normalized := strings.TrimSuffix("/"+strings.TrimPrefix(suffix, "/"), "/")
	if normalized == "" {
		normalized = "/"
	}
	switch normalized {
	case "/", "/v0", "/v1":
	default:
		w.WriteHeader(http.StatusNotFound)
		return writeJSON(w, map[string]string{"error": "Not Found"})
	}

	targetPath := normalized
	if targetPath == "/" {
		targetPath = "/v1"
	}
	baseURL := s.requestBaseURL(r)
	metadata := &oauthex.ProtectedResourceMetadata{
		Resource:                          baseURL + targetPath,
		AuthorizationServers:              []string{baseURL},
		BearerMethodsSupported:            []string{"header", "query"},
		ScopesSupported:                   []string{"read", "write"},
		ResourceName:                      "EasyMatrix",
		ResourceDocumentation:             baseURL + "/v1/spec",
		ResourcePolicyURI:                 baseURL + "/v1/spec",
		ResourceSigningAlgValuesSupported: []string{},
	}
	mcpauth.ProtectedResourceMetadataHandler(metadata).ServeHTTP(w, r)
	return nil
}

func (s *Server) oauthAuthorizationServerMetadata(w http.ResponseWriter, r *http.Request) error {
	baseURL := s.requestBaseURL(r)
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET")
	w.Header().Set("Cache-Control", "no-cache")
	return writeJSON(w, map[string]any{
		"issuer":                                baseURL,
		"authorization_endpoint":                baseURL + "/oauth/authorize",
		"token_endpoint":                        baseURL + "/oauth/token",
		"revocation_endpoint":                   baseURL + "/oauth/revoke",
		"userinfo_endpoint":                     baseURL + "/oauth/userinfo",
		"registration_endpoint":                 baseURL + "/oauth/register",
		"grant_types_supported":                 []string{"authorization_code"},
		"token_endpoint_auth_methods_supported": []string{"none"},
		"response_types_supported":              []string{"code"},
		"scopes_supported":                      []string{"read", "write"},
		"code_challenge_methods_supported":      []string{"S256"},
		"service_documentation":                 baseURL + "/v1/spec",
	})
}

func (s *Server) oauthAuthorize(w http.ResponseWriter, r *http.Request) error {
	query := r.URL.Query()
	clientID := strings.TrimSpace(query.Get("client_id"))
	redirectURI := strings.TrimSpace(query.Get("redirect_uri"))
	responseType := strings.TrimSpace(query.Get("response_type"))
	scope := strings.TrimSpace(query.Get("scope"))
	state := strings.TrimSpace(query.Get("state"))
	codeChallenge := strings.TrimSpace(query.Get("code_challenge"))
	codeChallengeMethod := strings.TrimSpace(query.Get("code_challenge_method"))
	resource := strings.TrimSpace(query.Get("resource"))

	if responseType != "code" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(renderSimpleHTML("Invalid request", `Only response_type="code" is supported.`)))
		return nil
	}
	if clientID == "" || redirectURI == "" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(renderSimpleHTML("Invalid request", "Missing required client_id or redirect_uri.")))
		return nil
	}
	if codeChallenge == "" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(renderSimpleHTML("Invalid request", "PKCE code_challenge is required.")))
		return nil
	}
	if codeChallengeMethod == "" {
		codeChallengeMethod = oauthCodeChallengeMethodS2
	}
	if codeChallengeMethod != oauthCodeChallengeMethodS2 {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(renderSimpleHTML("Invalid request", "Only S256 code_challenge_method is supported.")))
		return nil
	}
	if _, err := url.Parse(redirectURI); err != nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(renderSimpleHTML("Invalid request", "Invalid redirect_uri.")))
		return nil
	}

	scopes := normalizeOAuthScopes(scope)

	s.oauthMu.RLock()
	client, hasClient := s.oauthClients[clientID]
	s.oauthMu.RUnlock()
	if hasClient {
		allowedRedirect := len(client.RedirectURIs) == 0
		for _, candidate := range client.RedirectURIs {
			if candidate == redirectURI {
				allowedRedirect = true
				break
			}
		}
		if !allowedRedirect {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(renderSimpleHTML("Invalid request", "redirect_uri does not match registered client.")))
			return nil
		}
	}

	code, err := s.createAuthorizationCode(clientID, redirectURI, scopes, state, codeChallenge, codeChallengeMethod, resource)
	if err != nil {
		return errs.Internal(fmt.Errorf("failed to create authorization code: %w", err))
	}

	redirect, err := url.Parse(redirectURI)
	if err != nil {
		return errs.Validation(map[string]any{"redirect_uri": "invalid redirect uri"})
	}
	values := redirect.Query()
	values.Set("code", code.Code)
	if state != "" {
		values.Set("state", state)
	}
	redirect.RawQuery = values.Encode()
	http.Redirect(w, r, redirect.String(), http.StatusFound)
	return nil
}

func (s *Server) oauthAuthorizeCallback(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		ClientInfo struct {
			ClientID string `json:"clientID"`
			Name     string `json:"name"`
		} `json:"clientInfo"`
		Scopes              []string `json:"scopes"`
		State               string   `json:"state,omitempty"`
		CodeChallenge       string   `json:"codeChallenge,omitempty"`
		CodeChallengeMethod string   `json:"codeChallengeMethod,omitempty"`
		Resource            string   `json:"resource,omitempty"`
	}
	if err := decodeJSON(r, &req); err != nil {
		return err
	}

	clientID := strings.TrimSpace(req.ClientInfo.ClientID)
	if clientID == "" {
		clientID = "unregistered-client"
	}
	scopes := normalizeOAuthScopes(strings.Join(req.Scopes, " "))
	codeChallengeMethod := strings.TrimSpace(req.CodeChallengeMethod)
	if codeChallengeMethod == "" {
		codeChallengeMethod = oauthCodeChallengeMethodS2
	}
	code, err := s.createAuthorizationCode(
		clientID,
		"urn:beeper:oauth:callback",
		scopes,
		req.State,
		strings.TrimSpace(req.CodeChallenge),
		codeChallengeMethod,
		strings.TrimSpace(req.Resource),
	)
	if err != nil {
		return errs.Internal(fmt.Errorf("failed to create authorization code: %w", err))
	}
	return writeJSON(w, map[string]any{
		"code":  code.Code,
		"state": req.State,
	})
}

func parseBodyValues(r *http.Request) (map[string]string, error) {
	values := make(map[string]string)
	contentType := strings.ToLower(strings.TrimSpace(strings.Split(r.Header.Get("Content-Type"), ";")[0]))
	switch contentType {
	case "application/json":
		var raw map[string]any
		if err := decodeJSON(r, &raw); err != nil {
			return nil, err
		}
		for key, value := range raw {
			switch typed := value.(type) {
			case string:
				values[key] = typed
			case float64:
				values[key] = strconv.FormatFloat(typed, 'f', -1, 64)
			case bool:
				values[key] = strconv.FormatBool(typed)
			}
		}
	default:
		if err := r.ParseForm(); err != nil {
			return nil, errs.Validation(map[string]any{"error": err.Error()})
		}
		for key, valueList := range r.PostForm {
			if len(valueList) > 0 {
				values[key] = valueList[0]
			}
		}
	}
	return values, nil
}

func (s *Server) oauthToken(w http.ResponseWriter, r *http.Request) error {
	body, err := parseBodyValues(r)
	if err != nil {
		return err
	}
	grantType := strings.TrimSpace(body["grant_type"])
	if grantType != "authorization_code" {
		w.WriteHeader(http.StatusBadRequest)
		return writeJSON(w, map[string]string{
			"error":             "unsupported_grant_type",
			"error_description": "only authorization_code is supported",
		})
	}

	codeValue := strings.TrimSpace(body["code"])
	clientID := strings.TrimSpace(body["client_id"])
	redirectURI := strings.TrimSpace(body["redirect_uri"])
	codeVerifier := strings.TrimSpace(body["code_verifier"])
	resource := strings.TrimSpace(body["resource"])

	code, ok, popErr := s.popAuthorizationCode(codeValue)
	if popErr != nil {
		return errs.Internal(fmt.Errorf("failed to consume authorization code: %w", popErr))
	}
	if !ok {
		w.WriteHeader(http.StatusBadRequest)
		return writeJSON(w, map[string]string{
			"error":             "invalid_grant",
			"error_description": "authorization code is invalid or expired",
		})
	}
	if clientID != "" && code.ClientID != "" && code.ClientID != clientID {
		w.WriteHeader(http.StatusBadRequest)
		return writeJSON(w, map[string]string{
			"error":             "invalid_client",
			"error_description": "client_id mismatch",
		})
	}
	if redirectURI != "" && code.RedirectURI != "" && code.RedirectURI != "urn:beeper:oauth:callback" && redirectURI != code.RedirectURI {
		w.WriteHeader(http.StatusBadRequest)
		return writeJSON(w, map[string]string{
			"error":             "invalid_grant",
			"error_description": "redirect_uri mismatch",
		})
	}
	if code.CodeChallenge != "" {
		if code.CodeChallengeMethod != oauthCodeChallengeMethodS2 || !verifyPKCES256(codeVerifier, code.CodeChallenge) {
			w.WriteHeader(http.StatusBadRequest)
			return writeJSON(w, map[string]string{
				"error":             "invalid_grant",
				"error_description": "PKCE validation failed",
			})
		}
	}

	issued, err := s.issueOAuthAccessToken(code.ClientID, code.Scopes, resource)
	if err != nil {
		return errs.Internal(fmt.Errorf("failed to issue access token: %w", err))
	}
	expiresIn := int64(oauthAccessTokenTTL.Seconds())
	return writeJSON(w, map[string]any{
		"access_token": issued.Value,
		"token_type":   issued.TokenType,
		"expires_in":   expiresIn,
		"scope":        oauthScopeString(issued.Scopes),
	})
}

func (s *Server) oauthUserInfo(w http.ResponseWriter, r *http.Request) error {
	tokenValue := parseAuthTokenFromRequest(r)
	token, ok := s.oauthTokenByValue(tokenValue)
	if !ok {
		return errs.Unauthorized("Unauthorized: missing or invalid token")
	}

	response := map[string]any{
		"sub":       token.Subject,
		"aud":       token.ClientID,
		"scope":     oauthScopeString(token.Scopes),
		"iat":       token.CreatedAt.Unix(),
		"client_id": token.ClientID,
		"token_use": "access",
	}
	if token.ExpiresAt != nil {
		response["exp"] = token.ExpiresAt.Unix()
	}
	return writeJSON(w, response)
}

func (s *Server) oauthRevoke(w http.ResponseWriter, r *http.Request) error {
	body, err := parseBodyValues(r)
	if err != nil {
		// RFC 7009 requires success response even for malformed input.
		return writeJSON(w, map[string]any{})
	}
	tokenValue := strings.TrimSpace(body["token"])
	if tokenValue != "" {
		s.oauthMu.Lock()
		entry, ok := s.oauthTokens[tokenValue]
		if ok && !entry.Static {
			now := time.Now().UTC()
			entry.RevokedAt = &now
			s.oauthTokens[tokenValue] = entry
			_ = s.persistOAuthStateLocked()
		}
		s.oauthMu.Unlock()
	}
	return writeJSON(w, map[string]any{})
}

func (s *Server) oauthIntrospect(w http.ResponseWriter, r *http.Request) error {
	body, err := parseBodyValues(r)
	if err != nil {
		return err
	}
	tokenValue := strings.TrimSpace(body["token"])
	if tokenValue == "" {
		w.WriteHeader(http.StatusBadRequest)
		return writeJSON(w, map[string]any{
			"error":             "invalid_request",
			"error_description": "Token parameter is required",
		})
	}
	token, ok := s.oauthTokenByValue(tokenValue)
	if !ok {
		return writeJSON(w, map[string]any{"active": false})
	}

	response := map[string]any{
		"active":     true,
		"scope":      oauthScopeString(token.Scopes),
		"token_type": oauthTokenTypeBearer,
		"iat":        token.CreatedAt.Unix(),
		"nbf":        token.CreatedAt.Unix(),
		"sub":        token.Subject,
		"aud":        token.ClientID,
		"client_id":  token.ClientID,
		"iss":        s.requestBaseURL(r),
		"app": map[string]any{
			"version":   appVersion,
			"name":      "EasyMatrix",
			"bundle_id": "com.beeper.desktop",
		},
		"client": map[string]any{
			"id":   token.ClientID,
			"name": token.ClientName,
		},
	}
	if token.ExpiresAt != nil {
		response["exp"] = token.ExpiresAt.Unix()
	}
	return writeJSON(w, response)
}

func (s *Server) oauthRegister(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		ClientName              string   `json:"client_name"`
		ClientURI               string   `json:"client_uri,omitempty"`
		GrantTypes              []string `json:"grant_types,omitempty"`
		ResponseTypes           []string `json:"response_types,omitempty"`
		RedirectURIs            []string `json:"redirect_uris,omitempty"`
		Scope                   string   `json:"scope,omitempty"`
		TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method,omitempty"`
	}
	if err := decodeJSON(r, &req); err != nil {
		return err
	}

	clientID, err := randomHexToken(12)
	if err != nil {
		return errs.Internal(fmt.Errorf("failed to generate client id: %w", err))
	}
	if strings.TrimSpace(req.ClientName) == "" {
		req.ClientName = oauthDefaultClientName
	}
	if len(req.GrantTypes) == 0 {
		req.GrantTypes = []string{"authorization_code"}
	}
	if len(req.ResponseTypes) == 0 {
		req.ResponseTypes = []string{"code"}
	}
	if strings.TrimSpace(req.Scope) == "" {
		req.Scope = "read write"
	}
	if strings.TrimSpace(req.TokenEndpointAuthMethod) == "" {
		req.TokenEndpointAuthMethod = "none"
	}

	client := oauthClient{
		ClientID:                clientID,
		ClientName:              req.ClientName,
		ClientURI:               req.ClientURI,
		RedirectURIs:            req.RedirectURIs,
		GrantTypes:              req.GrantTypes,
		ResponseTypes:           req.ResponseTypes,
		Scope:                   oauthScopeString(normalizeOAuthScopes(req.Scope)),
		TokenEndpointAuthMethod: req.TokenEndpointAuthMethod,
		CreatedAt:               time.Now().Unix(),
	}
	s.oauthMu.Lock()
	s.oauthClients[client.ClientID] = client
	if err = s.persistOAuthStateLocked(); err != nil {
		s.oauthMu.Unlock()
		return errs.Internal(fmt.Errorf("failed to persist oauth client: %w", err))
	}
	s.oauthMu.Unlock()

	baseURL := s.requestBaseURL(r)
	w.WriteHeader(http.StatusCreated)
	return writeJSON(w, map[string]any{
		"client_id":                  client.ClientID,
		"client_name":                client.ClientName,
		"client_uri":                 client.ClientURI,
		"redirect_uris":              client.RedirectURIs,
		"grant_types":                client.GrantTypes,
		"response_types":             client.ResponseTypes,
		"scope":                      client.Scope,
		"token_endpoint_auth_method": client.TokenEndpointAuthMethod,
		"client_id_issued_at":        client.CreatedAt,
		"authorization_endpoint":     baseURL + "/oauth/authorize",
		"token_endpoint":             baseURL + "/oauth/token",
	})
}

func (s *Server) deeplink(w http.ResponseWriter, r *http.Request) error {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(renderSimpleHTML("Beeper Desktop API", "You can close this window.")))
	return nil
}

func (s *Server) focusPage(w http.ResponseWriter, r *http.Request) error {
	chatID := strings.TrimSpace(r.PathValue("chatID"))
	draftText := strings.TrimSpace(r.URL.Query().Get("draft"))
	if chatID == "" && draftText != "" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(renderSimpleHTML("Bad Request", "Draft text requires a chat ID.")))
		return nil
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(renderSimpleHTML("Beeper Desktop API", "You can close this window.")))
	return nil
}
