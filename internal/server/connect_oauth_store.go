package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const oauthStateVersion = 1

type oauthPersistedState struct {
	Version int                               `json:"version"`
	Subject string                            `json:"subject"`
	Clients map[string]oauthClient            `json:"clients"`
	Codes   map[string]oauthAuthorizationCode `json:"codes"`
	Tokens  map[string]oauthAccessToken       `json:"tokens"`
}

func (s *Server) loadOAuthState() error {
	if strings.TrimSpace(s.oauthState) == "" {
		return nil
	}
	raw, err := os.ReadFile(s.oauthState)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("failed to read oauth state: %w", err)
	}

	var persisted oauthPersistedState
	if err = json.Unmarshal(raw, &persisted); err != nil {
		return fmt.Errorf("failed to parse oauth state: %w", err)
	}
	if persisted.Version != oauthStateVersion {
		return fmt.Errorf("unsupported oauth state version: %d", persisted.Version)
	}
	now := time.Now().UTC()

	clients := make(map[string]oauthClient, len(persisted.Clients))
	for key, value := range persisted.Clients {
		if strings.TrimSpace(key) == "" || strings.TrimSpace(value.ClientID) == "" {
			continue
		}
		clients[key] = value
	}
	codes := make(map[string]oauthAuthorizationCode, len(persisted.Codes))
	for key, value := range persisted.Codes {
		if strings.TrimSpace(key) == "" || strings.TrimSpace(value.Code) == "" {
			continue
		}
		if !value.ExpiresAt.IsZero() && now.After(value.ExpiresAt) {
			continue
		}
		codes[key] = value
	}
	tokens := make(map[string]oauthAccessToken, len(persisted.Tokens))
	for key, value := range persisted.Tokens {
		if strings.TrimSpace(key) == "" || strings.TrimSpace(value.Value) == "" {
			continue
		}
		if value.Static || value.RevokedAt != nil {
			continue
		}
		if value.ExpiresAt != nil && now.After(*value.ExpiresAt) {
			continue
		}
		tokens[key] = value
	}

	s.oauthMu.Lock()
	for key, value := range clients {
		s.oauthClients[key] = value
	}
	for key, value := range codes {
		s.oauthCodes[key] = value
	}
	for key, value := range tokens {
		s.oauthTokens[key] = value
	}
	s.pruneOAuthStateLocked(now)
	s.oauthMu.Unlock()
	return nil
}

func (s *Server) persistOAuthState() error {
	s.oauthMu.Lock()
	defer s.oauthMu.Unlock()
	return s.persistOAuthStateLocked()
}

func (s *Server) persistOAuthStateLocked() error {
	if strings.TrimSpace(s.oauthState) == "" {
		return nil
	}
	s.pruneOAuthStateLocked(time.Now().UTC())

	persisted := oauthPersistedState{
		Version: oauthStateVersion,
		Subject: s.oauthSubject,
		Clients: make(map[string]oauthClient, len(s.oauthClients)),
		Codes:   make(map[string]oauthAuthorizationCode, len(s.oauthCodes)),
		Tokens:  make(map[string]oauthAccessToken, len(s.oauthTokens)),
	}
	for key, value := range s.oauthClients {
		if strings.TrimSpace(key) == "" || strings.TrimSpace(value.ClientID) == "" {
			continue
		}
		persisted.Clients[key] = value
	}
	for key, value := range s.oauthCodes {
		if strings.TrimSpace(key) == "" || strings.TrimSpace(value.Code) == "" {
			continue
		}
		persisted.Codes[key] = value
	}
	for key, value := range s.oauthTokens {
		if strings.TrimSpace(key) == "" || strings.TrimSpace(value.Value) == "" || value.Static {
			continue
		}
		persisted.Tokens[key] = value
	}

	raw, err := json.Marshal(persisted)
	if err != nil {
		return fmt.Errorf("failed to encode oauth state: %w", err)
	}
	return writeAtomicFile(s.oauthState, raw, 0o600)
}

func (s *Server) pruneOAuthStateLocked(now time.Time) {
	for key, code := range s.oauthCodes {
		if code.ExpiresAt.IsZero() {
			continue
		}
		if now.After(code.ExpiresAt) {
			delete(s.oauthCodes, key)
		}
	}
	for key, token := range s.oauthTokens {
		if token.Static {
			continue
		}
		if token.RevokedAt != nil {
			delete(s.oauthTokens, key)
			continue
		}
		if token.ExpiresAt != nil && now.After(*token.ExpiresAt) {
			delete(s.oauthTokens, key)
		}
	}
}

func writeAtomicFile(path string, content []byte, mode os.FileMode) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("path is required")
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, ".tmp-oauth-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		_ = os.Remove(tmpName)
	}()

	if _, err = tmp.Write(content); err != nil {
		_ = tmp.Close()
		return err
	}
	if err = tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if err = tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
