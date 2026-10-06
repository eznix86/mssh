package server

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/eznix86/mssh/internal/agent"
	"github.com/eznix86/mssh/internal/protocol"
	"github.com/eznix86/mssh/internal/proxy"
	"github.com/eznix86/mssh/internal/transport"
)

func startTestServer(t *testing.T, opts Options) (*Server, string, context.CancelFunc, <-chan error) {
	t.Helper()
	opts.Host = "127.0.0.1"
	s := New(opts)
	s.heartbeat = 10 * time.Millisecond
	token, err := transport.LoadToken(opts.TokenFile)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := transport.Listen(opts.Host, 0, opts.TLSCert, opts.TLSKey, token)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.serve(ctx, listener, token) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(3 * time.Second):
			t.Error("server did not shut down")
		}
	})
	return s, listener.Addr().String(), cancel, done
}

func waitRegistration(t *testing.T, s *Server, node string, present bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		_, found := s.agents[node]
		s.mu.Unlock()
		if found == present {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("registration %q did not reach present=%v", node, present)
}

func registerTestAgent(t *testing.T, address, node string) net.Conn {
	t.Helper()
	conn, err := net.DialTimeout("tcp", address, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	conn.SetDeadline(time.Now().Add(time.Second))
	header, err := protocol.Header("AGENT", node, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := protocol.WriteLine(conn, header); err != nil {
		t.Fatal(err)
	}
	if err := protocol.Expect(bufio.NewReader(conn), "OK"); err != nil {
		t.Fatal(err)
	}
	conn.SetDeadline(time.Time{})
	return conn
}

func TestDisconnectedAgentCanRegisterAgain(t *testing.T) {
	s, address, _, _ := startTestServer(t, Options{})
	conn := registerTestAgent(t, address, "node")
	conn.Close()
	waitRegistration(t, s, "node", false)
	registerTestAgent(t, address, "node")
}

func TestAgentWaitsForClientAndReportsBusy(t *testing.T) {
	t.Run("TCP", func(t *testing.T) { testAgentTunnel(t, Options{}, transport.Security{}) })
	t.Run("TLS", func(t *testing.T) {
		cert, key, token := tlsFixture(t)
		testAgentTunnel(t, Options{TLSCert: cert, TLSKey: key, TokenFile: token},
			transport.Security{TLS: true, CAFile: cert, TokenFile: token})
	})
}

func testAgentTunnel(t *testing.T, serverOptions Options, security transport.Security) {
	t.Helper()
	s, address, _, _ := startTestServer(t, serverOptions)
	sshListener, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	defer sshListener.Close()
	opts, err := agent.ParseServerAddr(address)
	if err != nil {
		t.Fatal(err)
	}
	opts.NodeID, opts.SSHPort = "node", sshListener.Addr().(*net.TCPAddr).Port
	opts.Security = security
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	agentDone := make(chan error, 1)
	go func() { agentDone <- agent.Run(ctx, opts) }()
	waitRegistration(t, s, "node", true)
	sshListener.SetDeadline(time.Now().Add(75 * time.Millisecond))
	if conn, err := sshListener.Accept(); err == nil {
		conn.Close()
		t.Fatal("agent opened SSH before a client arrived")
	} else if networkErr, ok := err.(net.Error); !ok || !networkErr.Timeout() {
		t.Fatal(err)
	}
	sshListener.SetDeadline(time.Time{})
	echoDone := make(chan error, 1)
	go func() {
		conn, err := sshListener.Accept()
		if err != nil {
			echoDone <- err
			return
		}
		defer conn.Close()
		_, err = io.Copy(conn, conn)
		echoDone <- err
	}()
	clientOpts, err := proxy.ParseServerAddr(address)
	if err != nil {
		t.Fatal(err)
	}
	clientOpts.NodeID = "node"
	clientOpts.Security = security
	client, err := proxy.Dial(ctx, clientOpts)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	client.SetDeadline(time.Now().Add(time.Second))
	if _, err := client.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	response := make([]byte, 5)
	if _, err := io.ReadFull(client, response); err != nil {
		t.Fatal(err)
	}
	if string(response) != "hello" {
		t.Fatalf("response %q", response)
	}
	if second, err := proxy.Dial(ctx, clientOpts); err == nil {
		second.Close()
		t.Fatal("second client was accepted")
	} else if !strings.Contains(err.Error(), "agent busy") {
		t.Fatal(err)
	}
	client.Close()
	waitRegistration(t, s, "node", false)
	cancel()
	select {
	case err := <-agentDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("agent did not stop")
	}
	select {
	case err := <-echoDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("local SSH connection remained open")
	}
}

func tlsFixture(t *testing.T, names ...string) (certFile, keyFile, tokenFile string) {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "mssh test"},
		NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true, IsCA: true,
		DNSNames: []string{"localhost"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
	}
	if len(names) > 0 {
		template.DNSNames = names
		template.IPAddresses = nil
	}
	cert, err := x509.CreateCertificate(rand.Reader, template, template, public, private)
	if err != nil {
		t.Fatal(err)
	}
	key, err := x509.MarshalPKCS8PrivateKey(private)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	certFile, keyFile, tokenFile = filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem"), filepath.Join(dir, "token")
	for name, data := range map[string][]byte{
		certFile:  pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert}),
		keyFile:   pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: key}),
		tokenFile: []byte(strings.Repeat("a", 64)),
	} {
		if err := os.WriteFile(name, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	return certFile, keyFile, tokenFile
}

func TestTLSAndTokenAreVerifiedBeforePairing(t *testing.T) {
	cert, key, token := tlsFixture(t)
	_, address, _, _ := startTestServer(t, Options{TLSCert: cert, TLSKey: key, TokenFile: token})
	opts, err := proxy.ParseServerAddr(address)
	if err != nil {
		t.Fatal(err)
	}
	opts.NodeID = "node"
	opts.Security = transport.Security{TLS: true, CAFile: cert, TokenFile: token}
	if conn, err := proxy.Dial(context.Background(), opts); err == nil {
		conn.Close()
		t.Fatal("offline node was accepted")
	} else if !strings.Contains(err.Error(), "agent offline") {
		t.Fatal(err)
	}
	opts.Security.TokenFile = ""
	if conn, err := proxy.Dial(context.Background(), opts); err == nil {
		conn.Close()
		t.Fatal("missing token was accepted")
	} else if !strings.Contains(err.Error(), "unauthorized") {
		t.Fatal(err)
	}
	opts.Security.TokenFile, opts.Security.CAFile = token, ""
	if conn, err := proxy.Dial(context.Background(), opts); err == nil {
		conn.Close()
		t.Fatal("untrusted certificate was accepted")
	}
}

func TestPublicServerRejectsMissingSecurity(t *testing.T) {
	s := New(Options{Host: "0.0.0.0", Port: 8443})
	if err := s.Run(context.Background()); err == nil {
		t.Fatal("public plaintext server accepted")
	}
}

func TestTrustedTLSCertificateMustMatchServerName(t *testing.T) {
	cert, key, token := tlsFixture(t, "different.example.com")
	_, address, _, _ := startTestServer(t, Options{TLSCert: cert, TLSKey: key, TokenFile: token})
	opts, err := proxy.ParseServerAddr(address)
	if err != nil {
		t.Fatal(err)
	}
	opts.NodeID = "node"
	opts.Security = transport.Security{TLS: true, CAFile: cert, TokenFile: token}
	conn, err := proxy.Dial(context.Background(), opts)
	if err == nil {
		conn.Close()
		t.Fatal("hostname mismatch was accepted")
	}
	var hostnameErr x509.HostnameError
	if !errors.As(err, &hostnameErr) {
		t.Fatal(err)
	}
}

func TestWrongTokenCannotRegisterAgent(t *testing.T) {
	cert, key, token := tlsFixture(t)
	s, address, _, _ := startTestServer(t, Options{TLSCert: cert, TLSKey: key, TokenFile: token})
	host, port, err := transport.ParseAddr(address)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := transport.Dial(context.Background(), host, port, transport.Security{TLS: true, CAFile: cert})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(time.Second))
	header, err := protocol.Header("AGENT", "node", strings.Repeat("b", 64))
	if err != nil {
		t.Fatal(err)
	}
	if err := protocol.WriteLine(conn, header); err != nil {
		t.Fatal(err)
	}
	if err := protocol.Expect(bufio.NewReader(conn), "OK"); err == nil || !strings.Contains(err.Error(), "unauthorized") {
		t.Fatalf("registration error: %v", err)
	}
	waitRegistration(t, s, "node", false)
}

func TestConnectionLimitRejectsExcessConnections(t *testing.T) {
	_, address, _, _ := startTestServer(t, Options{MaxConnections: 1})
	first := registerTestAgent(t, address, "node")
	defer first.Close()
	second, err := net.DialTimeout("tcp", address, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	second.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := second.Read(make([]byte, 1)); err == nil {
		t.Fatal("excess connection remained open")
	} else if networkErr, ok := err.(net.Error); ok && networkErr.Timeout() {
		t.Fatal("limit did not close the connection")
	}
}

func TestShutdownClosesConnectionsBeforeReturning(t *testing.T) {
	s, address, cancel, _ := startTestServer(t, Options{})
	registered := registerTestAgent(t, address, "node")
	pending, err := net.DialTimeout("tcp", address, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer pending.Close()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		count := len(s.connections)
		s.mu.Unlock()
		if count == 2 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	for _, conn := range []net.Conn{registered, pending} {
		conn.SetReadDeadline(time.Now().Add(time.Second))
		if _, err := io.ReadAll(conn); err != nil {
			t.Fatalf("connection did not close: %v", err)
		}
	}
}
