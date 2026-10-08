package webdav

import (
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ilanzou"
)

func TestRootPropfindDepthZero(t *testing.T) {
	handler := NewHandler(ilanzou.NewScopedClient(nil, "0"), "test-user", "test-password", 1, 1)
	request := httptest.NewRequest("PROPFIND", "/", nil)
	request.Header.Set("Depth", "0")
	request.SetBasicAuth("test-user", "test-password")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != 207 {
		t.Fatalf("status = %d, want 207", response.Code)
	}
	if err := xml.Unmarshal(response.Body.Bytes(), new(any)); err != nil {
		t.Fatalf("invalid multistatus XML: %v", err)
	}
	if !strings.Contains(response.Body.String(), "<D:href>/</D:href>") || !strings.Contains(response.Body.String(), "<D:collection/>") {
		t.Fatalf("root collection properties missing: %s", response.Body.String())
	}
}

func TestAuthenticationRequired(t *testing.T) {
	handler := NewHandler(ilanzou.NewScopedClient(nil, "0"), "test-user", "test-password", 1, 1)
	for _, setCredentials := range []func(*http.Request){
		func(*http.Request) {},
		func(request *http.Request) { request.SetBasicAuth("test-user", "wrong-password") },
	} {
		request := httptest.NewRequest(http.MethodOptions, "/", nil)
		setCredentials(request)
		response := httptest.NewRecorder()

		handler.ServeHTTP(response, request)

		if response.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
		}
		if response.Header().Get("WWW-Authenticate") == "" {
			t.Fatal("missing Basic authentication challenge")
		}
	}
}
