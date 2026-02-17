package auth

import (
	"crypto/subtle"
	"net/http"
	"strings"

	errs "github.com/batuhan/gomuks-beeper-api/internal/errors"
)

type Middleware struct {
	token               string
	allowQueryTokenAuth bool
	extraValidator      func(string, []string) bool
}

func New(token string, allowQueryTokenAuth bool) *Middleware {
	return &Middleware{token: token, allowQueryTokenAuth: allowQueryTokenAuth}
}

func (m *Middleware) SetExtraValidator(validator func(string, []string) bool) {
	m.extraValidator = validator
}

func (m *Middleware) Wrap(next http.Handler, allowQueryToken bool, requiredScopes []string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := parseToken(r)
		if token == "" && allowQueryToken && m.allowQueryTokenAuth {
			token = r.URL.Query().Get("dangerouslyUseTokenInQuery")
		}
		staticTokenValid := subtle.ConstantTimeCompare([]byte(token), []byte(m.token)) == 1
		extraTokenValid := m.extraValidator != nil && m.extraValidator(token, requiredScopes)
		if token == "" || (!staticTokenValid && !extraTokenValid) {
			errs.Write(w, errs.Unauthorized("Unauthorized: missing or invalid token"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func parseToken(r *http.Request) string {
	authz := r.Header.Get("Authorization")
	if strings.HasPrefix(authz, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(authz, "Bearer "))
	}
	if token := r.Header.Get("X-Beeper-Access-Token"); token != "" {
		return token
	}
	return ""
}
