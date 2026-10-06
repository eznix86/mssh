package protocol

import (
	"bufio"
	"io"
	"strings"
	"testing"
)

func TestControlMessagesAreBoundedAndKeepTunnelBytes(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader("OK\nSSH-2.0-test\r\n"))
	if err := Expect(reader, "OK"); err != nil {
		t.Fatal(err)
	}
	remaining, err := io.ReadAll(reader)
	if err != nil || string(remaining) != "SSH-2.0-test\r\n" {
		t.Fatalf("tunnel bytes: %q, %v", remaining, err)
	}
	if _, err := ReadLine(bufio.NewReader(strings.NewReader(strings.Repeat("x", MaxLine+1)))); err == nil {
		t.Fatal("oversized message accepted")
	}
}

func TestUnexpectedRepliesAndOldHeadersAreRejected(t *testing.T) {
	for _, reply := range []string{"\n", "WELCOME\n", "ERROR: offline\n", "OK extra\n"} {
		if err := Expect(bufio.NewReader(strings.NewReader(reply)), "OK"); err == nil {
			t.Fatalf("accepted %q", reply)
		}
	}
	for _, header := range []string{"AGENT node", "MSSH/2 AGENT bad/node -", "MSSH/2 ADMIN node -"} {
		if _, _, _, err := ParseHeader(header); err == nil {
			t.Fatalf("accepted %q", header)
		}
	}
}
