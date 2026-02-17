package auth

import (
	"context"
	"crypto/subtle"
	"fmt"
	"net/http"
	"strings"
	"time"

	mcpauth "github.com/modelcontextprotocol/go-sdk/auth"
)

type Middleware struct {
	token               string
	allowQueryTokenAuth bool
	tokenInfoProvider   func(string) (*mcpauth.TokenInfo, bool)
}

func New(token string, allowQueryTokenAuth bool) *Middleware {
	return &Middleware{token: token, allowQueryTokenAuth: allowQueryTokenAuth}
}

func (m *Middleware) SetTokenInfoProvider(provider func(string) (*mcpauth.TokenInfo, bool)) {
	m.tokenInfoProvider = provider
}

func (m *Middleware) Wrap(next http.Handler, allowQueryToken bool, requiredScopes []string) http.Handler {
	verifier := func(_ context.Context, token string, _ *http.Request) (*mcpauth.TokenInfo, error) {
		if subtle.ConstantTimeCompare([]byte(token), []byte(m.token)) == 1 {
			return &mcpauth.TokenInfo{
				Scopes:     []string{"read", "write"},
				Expiration: time.Now().Add(10 * 365 * 24 * time.Hour),
				UserID:     "static-token-user",
			}, nil
		}
		if m.tokenInfoProvider != nil {
			if info, ok := m.tokenInfoProvider(token); ok && info != nil {
				if info.Expiration.IsZero() {
					info.Expiration = time.Now().Add(10 * 365 * 24 * time.Hour)
				}
				return info, nil
			}
		}
		return nil, fmt.Errorf("%w: invalid bearer token", mcpauth.ErrInvalidToken)
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := parseToken(r, allowQueryToken && m.allowQueryTokenAuth)
		if token != "" {
			r = withBearerToken(r, token)
		}
		opts := &mcpauth.RequireBearerTokenOptions{
			Scopes:              requiredScopes,
			ResourceMetadataURL: protectedResourceMetadataURL(r),
		}
		mcpauth.RequireBearerToken(verifier, opts)(next).ServeHTTP(w, r)
	})
}

func parseToken(r *http.Request, allowQueryToken bool) string {
	authz := r.Header.Get("Authorization")
	if strings.HasPrefix(authz, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(authz, "Bearer "))
	}
	if token := r.Header.Get("X-Beeper-Access-Token"); token != "" {
		return strings.TrimSpace(token)
	}
	if allowQueryToken {
		if token := strings.TrimSpace(r.URL.Query().Get("dangerouslyUseTokenInQuery")); token != "" {
			return token
		}
	}
	return ""
}

func withBearerToken(r *http.Request, token string) *http.Request {
	clone := r.Clone(r.Context())
	clone.Header = r.Header.Clone()
	clone.Header.Set("Authorization", "Bearer "+token)
	return clone
}

func protectedResourceMetadataURL(r *http.Request) string {
	scheme := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")[0])
	if scheme == "" {
		if r.TLS != nil {
			scheme = "https"
		} else {
			scheme = "http"
		}
	}
	host := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Host"), ",")[0])
	if host == "" {
		host = strings.TrimSpace(r.Host)
	}
	if host == "" {
		return "/.well-known/oauth-protected-resource"
	}
	return scheme + "://" + host + "/.well-known/oauth-protected-resource"
}
