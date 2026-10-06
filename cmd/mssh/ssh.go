package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/eznix86/mssh/internal/protocol"
	"github.com/eznix86/mssh/internal/proxy"
	"github.com/eznix86/mssh/internal/transport"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

func runSSH(ctx context.Context, user, node, serverAddr, identity string, security transport.Security) error {
	serverOpts, err := proxy.ParseServerAddr(serverAddr)
	if err != nil {
		return fmt.Errorf("invalid server address: %w", err)
	}
	serverOpts.NodeID = node
	serverOpts.Security = security

	auth, cleanupAgent, err := buildAuthMethods(identity)
	if err != nil {
		return err
	}
	if cleanupAgent != nil {
		defer cleanupAgent()
	}

	hostKeyCallback, err := loadHostKeys()
	if err != nil {
		return err
	}
	conn, err := proxy.Dial(ctx, serverOpts)
	if err != nil {
		return err
	}

	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	conn.SetDeadline(time.Now().Add(protocol.SetupTimeout))

	config := &ssh.ClientConfig{
		User:            user,
		Auth:            auth,
		HostKeyCallback: hostKeyCallback,
	}

	clientConn, chans, reqs, err := ssh.NewClientConn(conn, net.JoinHostPort(node, "22"), config)
	if err != nil {
		return fmt.Errorf("ssh handshake failed: %w", err)
	}
	conn.SetDeadline(time.Time{})
	client := ssh.NewClient(clientConn, chans, reqs)
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("create SSH session: %w", err)
	}
	defer session.Close()

	restore, err := prepareTerminal(session)
	if err != nil {
		return err
	}
	defer restore()

	session.Stdout = os.Stdout
	session.Stderr = os.Stderr
	session.Stdin = os.Stdin

	if err := session.Shell(); err != nil {
		return fmt.Errorf("start shell: %w", err)
	}

	return session.Wait()
}

func loadHostKeys() (ssh.HostKeyCallback, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("find home directory: %w", err)
	}
	callback, err := knownhosts.New(filepath.Join(home, ".ssh", "known_hosts"))
	if err != nil {
		return nil, fmt.Errorf("load trusted SSH host keys: %w", err)
	}
	return callback, nil
}
