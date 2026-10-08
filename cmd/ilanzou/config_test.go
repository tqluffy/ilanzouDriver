package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCommandLineOverridesWebDAVConfig(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "ilanzou-webdav.toml")
	if err := os.WriteFile(configPath, []byte("username = \"toml-user\"\npassword = \"toml-password\"\nlisten = \"127.0.0.1:9000\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ILANZOU_USERNAME", "environment-user")

	options, _, err := parseInvocation([]string{"--config", configPath, "--username", "command-user", "--listen=:0", "start"})
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := resolveSettings(options, "ilanzou-webdav.toml")
	if err != nil {
		t.Fatal(err)
	}
	if resolved.username != "command-user" || resolved.password != "toml-password" || resolved.listen != ":0" {
		t.Fatalf("resolved settings = %+v", resolved)
	}
}
