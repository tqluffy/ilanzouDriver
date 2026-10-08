package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCommandLineOverridesWebDAVConfig(t *testing.T) {
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
	t.Setenv("ILANZOU_WEBDAV_USERNAME", "environment-dav-user")
	t.Setenv("ILANZOU_WEBDAV_PASSWORD", "environment-dav-password")

	options, _, err := parseInvocation([]string{
		"--config", configPath,
		"--webdav-username", "command-dav-user",
		"--webdav-password", "command-dav-password",
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
	if resolved.username != "ilanzou-user" || resolved.password != "ilanzou-password" || resolved.webdavUsername != "command-dav-user" || resolved.webdavPassword != "command-dav-password" || resolved.listen != ":0" {
		t.Fatalf("resolved settings = %+v", resolved)
	}

	envOptions, _, err := parseInvocation([]string{"--config", configPath, "start"})
	if err != nil {
		t.Fatal(err)
	}
	envResolved, err := resolveSettings(envOptions, "ilanzou-webdav.toml")
	if err != nil {
		t.Fatal(err)
	}
	if envResolved.username != "ilanzou-user" || envResolved.webdavUsername != "environment-dav-user" || envResolved.webdavPassword != "environment-dav-password" {
		t.Fatalf("environment-resolved settings = %+v", envResolved)
	}
}
