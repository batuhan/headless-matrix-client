package server

import (
	"testing"
	"time"

	"github.com/batuhan/easymatrix/internal/config"
)

func TestResolveAssetAccessToken(t *testing.T) {
	server := &Server{
		cfg: config.Config{AccessToken: "static-token"},
		oauthTokens: map[string]oauthAccessToken{
			"oauth-token": {
				Value:  "oauth-token",
				Scopes: []string{"read"},
			},
		},
	}

	assetURL := "mxc://example.test/media"
	tests := []struct {
		name      string
		token     string
		signature string
		wantOK    bool
	}{
		{
			name:      "static token",
			token:     "static-token",
			signature: assetAccessSignature("static-token", assetURL),
			wantOK:    true,
		},
		{
			name:      "oauth token",
			token:     "oauth-token",
			signature: assetAccessSignature("oauth-token", assetURL),
			wantOK:    true,
		},
		{
			name:      "wrong URL",
			token:     "static-token",
			signature: assetAccessSignature("static-token", "mxc://example.test/other"),
			wantOK:    false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			token, ok := server.resolveAssetAccessToken(test.signature, assetURL)
			if ok != test.wantOK {
				t.Fatalf("resolveAssetAccessToken() ok = %v, want %v", ok, test.wantOK)
			}
			if ok && token != test.token {
				t.Fatalf("resolveAssetAccessToken() token = %q, want %q", token, test.token)
			}
		})
	}

	expiredAt := time.Now().Add(-time.Minute)
	server.oauthTokens["expired-token"] = oauthAccessToken{
		Value:     "expired-token",
		Scopes:    []string{"read"},
		ExpiresAt: &expiredAt,
	}
	if _, ok := server.resolveAssetAccessToken(
		assetAccessSignature("expired-token", assetURL),
		assetURL,
	); ok {
		t.Fatal("resolveAssetAccessToken() accepted an expired token")
	}
}
