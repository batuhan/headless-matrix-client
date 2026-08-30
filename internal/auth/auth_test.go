package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAssetSignatureResolvesBearerOnlyForAssetEndpoint(t *testing.T) {
	middleware := New("server-token", false)
	middleware.SetAssetTokenResolver(func(signature, assetURL string) (string, bool) {
		if signature == "valid-signature" && assetURL == "mxc://example.test/media" {
			return "server-token", true
		}
		return "", false
	})
	handler := middleware.Wrap(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}),
		true,
		[]string{"read"},
	)

	request := httptest.NewRequest(
		http.MethodGet,
		"/v1/assets/serve?url=mxc%3A%2F%2Fexample.test%2Fmedia&assetAccessSignature=valid-signature",
		nil,
	)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("asset request returned %d, want %d", response.Code, http.StatusNoContent)
	}

	request = httptest.NewRequest(
		http.MethodGet,
		"/v1/accounts?url=mxc%3A%2F%2Fexample.test%2Fmedia&assetAccessSignature=valid-signature",
		nil,
	)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("non-asset request returned %d, want %d", response.Code, http.StatusUnauthorized)
	}
}
