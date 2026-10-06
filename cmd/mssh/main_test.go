package main

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/eznix86/mssh/internal/config"
	"github.com/eznix86/mssh/internal/transport"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

func TestTargetAcceptsAtSignInUsername(t *testing.T) {
	user, node, err := parseTarget("alice@example.org@prod-db")
	if err != nil || user != "alice@example.org" || node != "prod-db" {
		t.Fatalf("got %q, %q, %v", user, node, err)
	}
	for _, target := range []string{"alice", "@prod-db", "alice@"} {
		if _, _, err := parseTarget(target); err == nil {
			t.Fatalf("accepted invalid target %q", target)
		}
	}
}

func TestImplicitSSHDoesNotInterceptCommandsOrFlags(t *testing.T) {
	for _, args := range [][]string{nil, {"server"}, {"ssh", "alice@node"}, {"--server=alice@node"}} {
		if needsImplicitSSH(args) {
			t.Fatalf("intercepted %v", args)
		}
	}
	if !needsImplicitSSH([]string{"alice@node"}) {
		t.Fatal("target was not recognized")
	}
}

func TestConfigPrecedence(t *testing.T) {
	cfg := config.Config{Server: "global:8443", Identity: "global-key", Nodes: map[string]config.NodeEntry{
		"node": {Server: "node:8443", Identity: "node-key"},
	}}
	for _, tc := range []struct{ flag, node, server, identity string }{
		{"", "other", "global:8443", "global-key"},
		{"", "node", "node:8443", "node-key"},
		{"override", "node", "override", "override"},
	} {
		got, err := resolveServer(tc.flag, cfg, tc.node)
		if err != nil || got != tc.server {
			t.Fatalf("server: %q, %v", got, err)
		}
		if got := resolveIdentity(tc.flag, cfg, tc.node); got != tc.identity {
			t.Fatalf("identity: %q", got)
		}
	}
	if _, err := resolveServer("", config.Config{}, "node"); err == nil {
		t.Fatal("missing server accepted")
	}
}

func TestConnectionFlagsOverrideNodeSecurity(t *testing.T) {
	server, ca, token, tls := "", "flag-ca", "flag-token", false
	flags := connectionFlags{server: &server, ca: &ca, token: &token, tls: &tls}
	cfg := config.Config{Security: transport.Security{TokenFile: "global-token"},
		Nodes: map[string]config.NodeEntry{"node": {CAFile: "node-ca", TokenFile: "node-token"}}}
	security := flags.security(cfg, "node")
	if !security.TLS || security.CAFile != ca || security.TokenFile != token {
		t.Fatalf("security: %+v", security)
	}
}

func TestPathExpansionPreservesNamedUsersAndRelativePaths(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, tc := range []struct{ input, want string }{
		{"~", home}, {"~/.ssh/key", filepath.Join(home, ".ssh", "key")},
		{"~alice/key", "~alice/key"}, {"key", "key"},
	} {
		got, err := expandPath(tc.input)
		if err != nil || got != tc.want {
			t.Fatalf("%q: %q, %v", tc.input, got, err)
		}
	}
}

func TestNodeSanitization(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"prod.db_1", "prod.db_1"}, {" db / 1 ", "db-1"}, {"///", ""},
	} {
		if got := sanitizeNodeID(tc.input); got != tc.want {
			t.Fatalf("%q: %q", tc.input, got)
		}
	}
}

func TestPromptsStopAtEOF(t *testing.T) {
	for _, current := range []string{"", "existing"} {
		if _, err := promptServer(bufio.NewReader(strings.NewReader("")), current); !errors.Is(err, io.EOF) {
			t.Fatalf("server: %v", err)
		}
		if _, err := promptIdentity(bufio.NewReader(strings.NewReader("")), current); !errors.Is(err, io.EOF) {
			t.Fatalf("identity: %v", err)
		}
	}
}

func TestExplicitIdentityFailuresAreReturned(t *testing.T) {
	for _, name := range []string{"missing", "invalid"} {
		path := filepath.Join(t.TempDir(), name)
		if name == "invalid" {
			if err := os.WriteFile(path, []byte("invalid private key"), 0600); err != nil {
				t.Fatal(err)
			}
		}
		if _, _, err := buildAuthMethods(path); err == nil {
			t.Fatalf("accepted %s identity", name)
		}
	}
}

func testSigner(t *testing.T) (ssh.Signer, string) {
	t.Helper()
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "key")
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: encoded}), 0600); err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return signer, path
}

func TestHostKeysRejectUnknownAndChangedNodes(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	signer, _ := testSigner(t)
	other, _ := testSigner(t)
	if err := os.Mkdir(filepath.Join(home, ".ssh"), 0700); err != nil {
		t.Fatal(err)
	}
	line := knownhosts.Line([]string{"prod-db"}, signer.PublicKey()) + "\n"
	if err := os.WriteFile(filepath.Join(home, ".ssh", "known_hosts"), []byte(line), 0600); err != nil {
		t.Fatal(err)
	}
	callback, err := loadHostKeys()
	if err != nil {
		t.Fatal(err)
	}
	remote := &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8443}
	if err := callback("prod-db:22", remote, signer.PublicKey()); err != nil {
		t.Fatal(err)
	}
	if err := callback("other-db:22", remote, signer.PublicKey()); err == nil {
		t.Fatal("unknown node accepted")
	}
	if err := callback("prod-db:22", remote, other.PublicKey()); err == nil {
		t.Fatal("changed key accepted")
	}
}

func TestRemoteShellFailureReturnsAndClosesConnection(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SSH_AUTH_SOCK", "")
	signer, identity := testSigner(t)
	if err := os.Mkdir(filepath.Join(home, ".ssh"), 0700); err != nil {
		t.Fatal(err)
	}
	line := knownhosts.Line([]string{"node"}, signer.PublicKey()) + "\n"
	if err := os.WriteFile(filepath.Join(home, ".ssh", "known_hosts"), []byte(line), 0600); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	done := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			done <- err
			return
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(5 * time.Second))
		reader := bufio.NewReader(conn)
		if _, err := reader.ReadString('\n'); err != nil {
			done <- err
			return
		}
		if _, err := io.WriteString(conn, "OK\n"); err != nil {
			done <- err
			return
		}
		cfg := &ssh.ServerConfig{NoClientAuth: true}
		cfg.AddHostKey(signer)
		serverConn, channels, requests, err := ssh.NewServerConn(conn, cfg)
		if err != nil {
			done <- err
			return
		}
		go ssh.DiscardRequests(requests)
		channel := <-channels
		session, sessionRequests, err := channel.Accept()
		if err != nil {
			done <- err
			return
		}
		for request := range sessionRequests {
			if request.Type == "shell" {
				request.Reply(true, nil)
				_, err = session.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{7}))
				session.Close()
				break
			}
		}
		serverConn.Wait()
		done <- err
	}()
	err = runSSH(context.Background(), "alice", "node", listener.Addr().String(), identity, transport.Security{})
	var exitErr *ssh.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitStatus() != 7 {
		t.Fatalf("exit: %v", err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("SSH connection was not closed")
	}
}

func TestSSHProcess(t *testing.T) {
	if os.Getenv("MSSH_TEST_PROCESS") != "1" {
		return
	}
	os.Args = []string{"mssh", "ssh", "alice@node", "--server", os.Getenv("MSSH_TEST_SERVER"),
		"--identity", os.Getenv("MSSH_TEST_IDENTITY")}
	main()
}

func TestTerminalIsRestoredAfterRemoteFailureAndPTYRejection(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is needed for the PTY regression")
	}
	for _, rejectPTY := range []bool{false, true} {
		t.Run(map[bool]string{false: "shell failure", true: "PTY rejection"}[rejectPTY], func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("SSH_AUTH_SOCK", "")
			signer, identity := testSigner(t)
			if err := os.Mkdir(filepath.Join(home, ".ssh"), 0700); err != nil {
				t.Fatal(err)
			}
			line := knownhosts.Line([]string{"node"}, signer.PublicKey()) + "\n"
			if err := os.WriteFile(filepath.Join(home, ".ssh", "known_hosts"), []byte(line), 0600); err != nil {
				t.Fatal(err)
			}
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			done := make(chan error, 1)
			go func() {
				conn, err := listener.Accept()
				if err != nil {
					done <- err
					return
				}
				defer conn.Close()
				conn.SetDeadline(time.Now().Add(5 * time.Second))
				if _, err := bufio.NewReader(conn).ReadString('\n'); err != nil {
					done <- err
					return
				}
				if _, err := io.WriteString(conn, "OK\n"); err != nil {
					done <- err
					return
				}
				cfg := &ssh.ServerConfig{NoClientAuth: true}
				cfg.AddHostKey(signer)
				serverConn, channels, requests, err := ssh.NewServerConn(conn, cfg)
				if err != nil {
					done <- err
					return
				}
				go ssh.DiscardRequests(requests)
				channel := <-channels
				session, sessionRequests, err := channel.Accept()
				if err != nil {
					done <- err
					return
				}
				for request := range sessionRequests {
					switch request.Type {
					case "pty-req":
						request.Reply(!rejectPTY, nil)
					case "shell":
						request.Reply(true, nil)
						_, err = session.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{7}))
						session.Close()
					}
				}
				serverConn.Wait()
				done <- err
			}()
			t.Setenv("MSSH_TEST_PROCESS", "1")
			t.Setenv("MSSH_TEST_SERVER", listener.Addr().String())
			t.Setenv("MSSH_TEST_IDENTITY", identity)
			code := "7"
			if rejectPTY {
				code = "1"
			}
			script := "import os,pty,subprocess,sys,termios\n" +
				"master,slave=pty.openpty()\n" +
				"before=termios.tcgetattr(slave)\n" +
				"child=subprocess.run([sys.argv[1],'-test.run=^TestSSHProcess$'],stdin=slave,stdout=slave,stderr=slave,timeout=10)\n" +
				"after=termios.tcgetattr(slave)\n" +
				"assert child.returncode==int(sys.argv[2]), child.returncode\n" +
				"if sys.platform=='darwin':\n" +
				" before[3]&=~termios.PENDIN;after[3]&=~termios.PENDIN\n" +
				"assert before==after, (before,after)\n" +
				"os.close(master);os.close(slave)\n"
			command := exec.Command(python, "-c", script, os.Args[0], code)
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("PTY check: %v\n%s", err, output)
			}
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(time.Second):
				t.Fatal("SSH server did not close")
			}
		})
	}
}
