package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"
)

func assetAccessSignature(accessToken, assetURL string) string {
	mac := hmac.New(sha256.New, []byte(accessToken))
	_, _ = mac.Write([]byte(assetURL))
	return hex.EncodeToString(mac.Sum(nil))
}

func (s *Server) resolveAssetAccessToken(signature, assetURL string) (string, bool) {
	signature = strings.TrimSpace(signature)
	assetURL = strings.TrimSpace(assetURL)
	if signature == "" || assetURL == "" {
		return "", false
	}

	staticToken := strings.TrimSpace(s.cfg.AccessToken)
	if staticToken != "" && validAssetAccessSignature(signature, assetURL, staticToken) {
		return staticToken, true
	}

	s.oauthMu.RLock()
	defer s.oauthMu.RUnlock()
	for token, entry := range s.oauthTokens {
		if entry.RevokedAt != nil {
			continue
		}
		if entry.ExpiresAt != nil && time.Now().After(*entry.ExpiresAt) {
			continue
		}
		if validAssetAccessSignature(signature, assetURL, token) {
			return token, true
		}
	}
	return "", false
}

func validAssetAccessSignature(signature, assetURL, accessToken string) bool {
	expected, err := hex.DecodeString(assetAccessSignature(accessToken, assetURL))
	if err != nil {
		return false
	}
	provided, err := hex.DecodeString(signature)
	if err != nil {
		return false
	}
	return hmac.Equal(provided, expected)
}
