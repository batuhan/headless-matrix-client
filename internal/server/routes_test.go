package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/batuhan/easymatrix/internal/config"
	"github.com/batuhan/easymatrix/internal/gomuksruntime"
	beeperdesktopapi "github.com/beeper/desktop-api-go"
)

func TestHandlerUsesV1RoutesOnly(t *testing.T) {
	cfg := config.Config{
		ListenAddr:          "127.0.0.1:0",
		StateDir:            t.TempDir(),
		AccessToken:         "test-token",
		MatrixHomeserverURL: "https://matrix.beeper.com",
	}
	rt, err := gomuksruntime.New(cfg)
	if err != nil {
		t.Fatalf("failed to create runtime: %v", err)
	}
	handler := New(cfg, rt).Handler()

	for _, path := range []string{"/v0/get-accounts", "/v0/search", "/v0/spec", "/ws"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s returned %d, expected 404", path, rec.Code)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/ws", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code == http.StatusNotFound {
		t.Fatalf("/v1/ws should remain registered")
	}
}

func TestOAuthProtectedResourceRejectsLegacyV0(t *testing.T) {
	cfg := config.Config{
		ListenAddr:          "127.0.0.1:0",
		StateDir:            t.TempDir(),
		AccessToken:         "test-token",
		MatrixHomeserverURL: "https://matrix.beeper.com",
	}
	rt, err := gomuksruntime.New(cfg)
	if err != nil {
		t.Fatalf("failed to create runtime: %v", err)
	}
	handler := New(cfg, rt).Handler()

	req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource/v0", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("/.well-known/oauth-protected-resource/v0 returned %d, expected 404", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource/v1", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code == http.StatusNotFound {
		t.Fatalf("/.well-known/oauth-protected-resource/v1 should remain registered")
	}
}

func TestInfoIncludesTypedMCPField(t *testing.T) {
	cfg := config.Config{
		ListenAddr:          "127.0.0.1:23373",
		StateDir:            t.TempDir(),
		AccessToken:         "test-token",
		MatrixHomeserverURL: "https://matrix.beeper.com",
	}
	rt, err := gomuksruntime.New(cfg)
	if err != nil {
		t.Fatalf("failed to create runtime: %v", err)
	}
	handler := New(cfg, rt).Handler()

	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:23373/v1/info", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("/v1/info returned %d, expected 200", rec.Code)
	}

	var payload beeperdesktopapi.InfoGetResponse
	if err = json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode /v1/info response: %v", err)
	}
	if payload.Endpoints.Mcp == "" {
		t.Fatal("expected /v1/info to include endpoints.mcp")
	}
}
