package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigResolverSeparatesWebDAVAccountFields(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "ilanzou-webdav.toml")
	config := `username = "ilanzou-user"
password = "ilanzou-password"
webdav_username = "toml-dav-user"
webdav_password = "toml-dav-password"
listen = "127.0.0.1:9000"
`
	if err := os.WriteFile(configPath, []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ILANZOU_USERNAME", "environment-ilanzou-user")

	options, _, err := parseInvocation([]string{
		"--config", configPath,
		"--username", "command-ilanzou-user",
		"--listen=:0",
		"start",
	})
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := resolveSettings(options, "ilanzou-webdav.toml")
	if err != nil {
		t.Fatal(err)
	}
	if resolved.username != "command-ilanzou-user" || resolved.password != "ilanzou-password" || resolved.webdavUsername != "toml-dav-user" || resolved.webdavPassword != "toml-dav-password" || resolved.listen != ":0" {
		t.Fatalf("resolved settings = %+v", resolved)
	}
}
