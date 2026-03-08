package server

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

import errs "github.com/batuhan/easymatrix/internal/errors"

const (
	manageSecretCookieName = "easymatrix_manage_access"
	manageSecretHeaderName = "X-EasyMatrix-Manage-Secret"
	manageSecretQueryName  = "secret"
)

func (s *Server) authorizeManageRequest(w http.ResponseWriter, r *http.Request) (bool, error) {
	expectedSecret := strings.TrimSpace(s.cfg.ManageSecret)
	if expectedSecret == "" {
		return false, nil
	}

	secret, source := readManageSecret(r)
	if subtle.ConstantTimeCompare([]byte(secret), []byte(expectedSecret)) != 1 {
		if r.Method == http.MethodGet && (r.URL.Path == "/manage" || r.URL.Path == "/manage/") {
			writeManageSecretPrompt(w)
			return true, nil
		}
		return false, errs.Unauthorized("Unauthorized: missing or invalid manage secret")
	}

	setManageSecretCookie(w, r, secret)
	if source == "query" && r.Method == http.MethodGet && (r.URL.Path == "/manage" || r.URL.Path == "/manage/") {
		redirectURL := *r.URL
		query := redirectURL.Query()
		query.Del(manageSecretQueryName)
		redirectURL.RawQuery = query.Encode()
		http.Redirect(w, r, redirectURL.RequestURI(), http.StatusSeeOther)
		return true, nil
	}

	return false, nil
}

func readManageSecret(r *http.Request) (string, string) {
	if secret := strings.TrimSpace(r.Header.Get(manageSecretHeaderName)); secret != "" {
		return secret, "header"
	}
	if secret := strings.TrimSpace(r.URL.Query().Get(manageSecretQueryName)); secret != "" {
		return secret, "query"
	}
	if cookie, err := r.Cookie(manageSecretCookieName); err == nil {
		if secret := strings.TrimSpace(cookie.Value); secret != "" {
			return secret, "cookie"
		}
	}
	return "", ""
}

func setManageSecretCookie(w http.ResponseWriter, r *http.Request, secret string) {
	http.SetCookie(w, &http.Cookie{
		Name:     manageSecretCookieName,
		Value:    secret,
		Path:     "/manage",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   requestUsesHTTPS(r),
	})
}

func requestUsesHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	scheme := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")[0])
	return strings.EqualFold(scheme, "https")
}

func writeManageSecretPrompt(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte(`<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <title>EasyMatrix Manage Access</title>
    <style>
      body {
        margin: 0;
        font-family: ui-sans-serif, system-ui, sans-serif;
        background: #0b1020;
        color: #f3f5f7;
      }
      main {
        max-width: 720px;
        margin: 0 auto;
        padding: 64px 24px;
      }
      code {
        background: rgba(255, 255, 255, 0.08);
        border-radius: 6px;
        padding: 2px 6px;
      }
      .card {
        background: rgba(255, 255, 255, 0.06);
        border: 1px solid rgba(255, 255, 255, 0.12);
        border-radius: 16px;
        padding: 24px;
      }
      p {
        line-height: 1.5;
      }
    </style>
  </head>
  <body>
    <main>
      <div class="card">
        <h1>Manage panel is protected</h1>
        <p>Open this page with <code>?secret=YOUR_SECRET</code> once, or send the secret in the <code>X-EasyMatrix-Manage-Secret</code> header.</p>
      </div>
    </main>
  </body>
</html>`))
}
