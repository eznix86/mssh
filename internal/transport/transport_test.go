package transport

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAddressesAcceptIPv6AndRejectInvalidPorts(t *testing.T) {
	host, port, err := ParseAddr("[::1]:8443")
	if err != nil || host != "::1" || port != 8443 {
		t.Fatalf("%q, %d, %v", host, port, err)
	}
	for _, address := range []string{":8443", "host:0", "host:65536", "host:-1", "host:not-a-port"} {
		if _, _, err := ParseAddr(address); err == nil {
			t.Fatalf("accepted %q", address)
		}
	}
}

func TestTokensAreBoundedAndCannotInjectHeaders(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	for _, value := range []string{"short", strings.Repeat("x", 514), strings.Repeat("x", 32) + "\nCLIENT node"} {
		if err := os.WriteFile(path, []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadToken(path); err == nil {
			t.Fatal("invalid token accepted")
		}
	}
	if err := os.WriteFile(path, []byte(strings.Repeat("x", 64)+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	token, err := LoadToken(path)
	if err != nil || token != strings.Repeat("x", 64) {
		t.Fatalf("token: %q, %v", token, err)
	}
}

func TestRemoteTokensRequireTLSBeforeDial(t *testing.T) {
	if conn, err := Dial(context.Background(), "example.com", 8443, Security{TokenFile: "token"}); err == nil {
		conn.Close()
		t.Fatal("remote token permitted without TLS")
	}
}
