package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/batuhan/easymatrix/internal/config"
	"github.com/batuhan/easymatrix/internal/gomuksruntime"
)

func TestManageUIRequiresSecretWhenConfigured(t *testing.T) {
	cfg := config.Config{
		ListenAddr:          "127.0.0.1:0",
		StateDir:            t.TempDir(),
		ManageSecret:        "open-sesame",
		MatrixHomeserverURL: "https://matrix.beeper.com",
	}
	rt, err := gomuksruntime.New(cfg)
	if err != nil {
		t.Fatalf("failed to create runtime: %v", err)
	}
	handler := New(cfg, rt).Handler()

	req := httptest.NewRequest(http.MethodGet, "/manage", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("/manage returned %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	if !strings.Contains(rec.Body.String(), "?secret=YOUR_SECRET") {
		t.Fatal("expected secret prompt in unauthorized manage response")
	}
}

func TestManageUIAcceptsSecretQueryAndSetsCookie(t *testing.T) {
	cfg := config.Config{
		ListenAddr:          "127.0.0.1:0",
		StateDir:            t.TempDir(),
		ManageSecret:        "open-sesame",
		MatrixHomeserverURL: "https://matrix.beeper.com",
	}
	rt, err := gomuksruntime.New(cfg)
	if err != nil {
		t.Fatalf("failed to create runtime: %v", err)
	}
	handler := New(cfg, rt).Handler()

	req := httptest.NewRequest(http.MethodGet, "/manage?secret=open-sesame", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("/manage with secret returned %d, want %d", rec.Code, http.StatusSeeOther)
	}
	if location := rec.Header().Get("Location"); location != "/manage" {
		t.Fatalf("Location = %q, want %q", location, "/manage")
	}
	cookies := rec.Result().Cookies()
	if len(cookies) == 0 || cookies[0].Name != manageSecretCookieName {
		t.Fatal("expected manage auth cookie to be set")
	}
}

func TestManageAPIRejectsMissingSecret(t *testing.T) {
	cfg := config.Config{
		ListenAddr:          "127.0.0.1:0",
		StateDir:            t.TempDir(),
		ManageSecret:        "open-sesame",
		MatrixHomeserverURL: "https://matrix.beeper.com",
	}
	rt, err := gomuksruntime.New(cfg)
	if err != nil {
		t.Fatalf("failed to create runtime: %v", err)
	}
	handler := New(cfg, rt).Handler()

	req := httptest.NewRequest(http.MethodPost, "/manage/login-flows", strings.NewReader(`{"homeserverURL":"https://matrix.beeper.com"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("/manage/login-flows returned %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}
