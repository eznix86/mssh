package stream

import (
	"context"
	"errors"
	"io"
	"net"
	"testing"
	"time"
)

func tcpPair(t *testing.T) (*net.TCPConn, *net.TCPConn) {
	t.Helper()
	listener, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	client, err := net.DialTCP("tcp", nil, listener.Addr().(*net.TCPAddr))
	if err != nil {
		t.Fatal(err)
	}
	server, err := listener.AcceptTCP()
	if err != nil {
		client.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close(); server.Close() })
	client.SetDeadline(time.Now().Add(2 * time.Second))
	server.SetDeadline(time.Now().Add(2 * time.Second))
	return client, server
}

func TestPipePreservesResponseAfterInputEOF(t *testing.T) {
	client, a := tcpPair(t)
	peer, b := tcpPair(t)
	done := make(chan error, 1)
	go func() { done <- Pipe(context.Background(), a, b) }()
	if _, err := io.WriteString(client, "request"); err != nil {
		t.Fatal(err)
	}
	if err := client.CloseWrite(); err != nil {
		t.Fatal(err)
	}
	request, err := io.ReadAll(peer)
	if err != nil || string(request) != "request" {
		t.Fatalf("request: %q, %v", request, err)
	}
	if _, err := io.WriteString(peer, "response"); err != nil {
		t.Fatal(err)
	}
	if err := peer.CloseWrite(); err != nil {
		t.Fatal(err)
	}
	response, err := io.ReadAll(client)
	if err != nil || string(response) != "response" {
		t.Fatalf("response: %q, %v", response, err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestPipeCancellationClosesBothDirections(t *testing.T) {
	a, peerA := net.Pipe()
	b, peerB := net.Pipe()
	defer peerA.Close()
	defer peerB.Close()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Pipe(ctx, a, b) }()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("pipe did not stop")
	}
}
