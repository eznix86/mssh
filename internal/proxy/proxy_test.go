package proxy

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/eznix86/mssh/internal/protocol"
)

func replyServer(t *testing.T, reply string) Options {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(time.Second))
		if _, err := protocol.ReadLine(bufio.NewReader(conn)); err != nil {
			return
		}
		io.WriteString(conn, reply)
	}()
	opts, err := ParseServerAddr(listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	opts.NodeID = "node"
	return opts
}

func TestProxyReturnsWhenRemoteClosesWithStdinOpen(t *testing.T) {
	opts := replyServer(t, "OK\nresponse")
	stdin, writer := io.Pipe()
	defer writer.Close()
	var output bytes.Buffer
	done := make(chan error, 1)
	go func() { done <- Run(context.Background(), opts, stdin, &output) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
		if output.String() != "response" {
			t.Fatalf("output %q", output.String())
		}
	case <-time.After(2 * time.Second):
		stdin.Close()
		t.Fatal("proxy waited on stdin after remote close")
	}
}

type failingWriter struct{ err error }

func (writer failingWriter) Write([]byte) (int, error) { return 0, writer.err }

func TestProxyReturnsOutputErrors(t *testing.T) {
	opts := replyServer(t, "OK\ndata")
	stdin, writer := io.Pipe()
	defer writer.Close()
	want := errors.New("output failed")
	if err := Run(context.Background(), opts, stdin, failingWriter{want}); !errors.Is(err, want) {
		t.Fatalf("error: %v", err)
	}
}

func TestDialRejectsUnexpectedResponse(t *testing.T) {
	opts := replyServer(t, "WELCOME\n")
	if conn, err := Dial(context.Background(), opts); err == nil {
		conn.Close()
		t.Fatal("unexpected response accepted")
	} else if !strings.Contains(err.Error(), "unexpected protocol response") {
		t.Fatal(err)
	}
}
