package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadWebDAVAccountsByUsername(t *testing.T) {
	configDir := t.TempDir()
	writeAccountConfig(t, configDir, "alice", `username = "cloud-a"
password = "cloud-a-pass"
webdav_username = "alice"
webdav_password = "alice-pass"
root_folder_id = "123"
listen = "0.0.0.0:8080"
`)
	writeAccountConfig(t, configDir, "bob", `username = "cloud-b"
password = "cloud-b-pass"
webdav_username = "bob"
webdav_password = "bob-pass"
root_folder_id = "456"
`)

	options := globalOptions{
		configPath:      configDir,
		configSpecified: true,
		values:          map[string]string{"listen": "127.0.0.1:9000"},
	}
	resolvedDir, accounts, listen, err := loadWebDAVAccounts(options)
	if err != nil {
		t.Fatal(err)
	}
	if resolvedDir != configDir || listen != "127.0.0.1:9000" {
		t.Fatalf("directory/listen = %q/%q", resolvedDir, listen)
	}
	if len(accounts) != 2 || accounts["alice"].config.username != "cloud-a" || accounts["alice"].config.rootFolderID != "123" || accounts["bob"].config.username != "cloud-b" || accounts["bob"].config.rootFolderID != "456" {
		t.Fatalf("accounts did not retain independent settings: %+v", accounts)
	}
}

func TestWebDAVAccountsAuthenticateIndependently(t *testing.T) {
	accounts := map[string]*webDAVAccount{
		"alice": {
			config: settings{webdavUsername: "alice", webdavPassword: "alice-pass"},
			handler: http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
				response.Header().Set("X-Account", "alice")
				response.WriteHeader(http.StatusNoContent)
			}),
		},
		"bob": {
			config: settings{webdavUsername: "bob", webdavPassword: "bob-pass"},
			handler: http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
				response.Header().Set("X-Account", "bob")
				response.WriteHeader(http.StatusNoContent)
			}),
		},
	}
	handler := &webDAVAccountsHandler{accounts: accounts}

	for _, test := range []struct {
		name     string
		username string
		password string
		want     string
		status   int
	}{
		{name: "alice", username: "alice", password: "alice-pass", want: "alice", status: http.StatusNoContent},
		{name: "bob", username: "bob", password: "bob-pass", want: "bob", status: http.StatusNoContent},
		{name: "wrong password", username: "alice", password: "bob-pass", status: http.StatusUnauthorized},
		{name: "unknown user", username: "mallory", password: "secret", status: http.StatusUnauthorized},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodOptions, "/", nil)
			request.SetBasicAuth(test.username, test.password)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.status || response.Header().Get("X-Account") != test.want {
				t.Fatalf("status/account = %d/%q, want %d/%q", response.Code, response.Header().Get("X-Account"), test.status, test.want)
			}
		})
	}
}

func writeAccountConfig(t *testing.T, directory, username, content string) {
	t.Helper()
	path := filepath.Join(directory, username+webDAVConfigSuffix)
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}
