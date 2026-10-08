package webdav

import (
	"encoding/xml"
	"net/http/httptest"
	"strings"
	"testing"

	"ilanzou"
)

func TestRootPropfindDepthZero(t *testing.T) {
	handler := NewHandler(ilanzou.NewScopedClient(nil, "0"), 1, 1)
	request := httptest.NewRequest("PROPFIND", "/", nil)
	request.Header.Set("Depth", "0")
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
