package config

import (
	"github.com/eznix86/mssh/internal/transport"
	"os"
	"path/filepath"
	"testing"
)

func TestSecuritySettingsLoadAndUseNodeOverrides(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.Mkdir(filepath.Join(home, ".mssh"), 0700); err != nil {
		t.Fatal(err)
	}
	data := "server: localhost:8443\ntls: true\ntoken_file: global-token\nnodes:\n  node:\n    token_file: node-token\n    tls_ca: node-ca\n"
	if err := os.WriteFile(filepath.Join(home, ".mssh", "config.yaml"), []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	security := cfg.SecurityFor("node")
	if !security.TLS || security.TokenFile != "node-token" || security.CAFile != "node-ca" {
		t.Fatalf("node security: %+v", security)
	}
	security = cfg.SecurityFor("other")
	if !security.TLS || security.TokenFile != "global-token" || security.CAFile != "" {
		t.Fatalf("global security: %+v", security)
	}
}

func TestNodeCanDisableInheritedTLS(t *testing.T) {
	disabled := false
	cfg := Config{Security: transport.Security{TLS: true, CAFile: "global-ca"},
		Nodes: map[string]NodeEntry{"local": {TLS: &disabled}}}
	security := cfg.SecurityFor("local")
	if security.TLS || security.CAFile != "" {
		t.Fatalf("security: %+v", security)
	}
}
