package main

import (
	"fmt"
	"net"
	"os"
	"path/filepath"

	"github.com/eznix86/mssh/internal/proxy"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

func runSSH(user, node, serverAddr, identity string) error {
	serverOpts, err := proxy.ParseServerAddr(serverAddr)
	if err != nil {
		return fmt.Errorf("invalid server address: %w", err)
	}
	serverOpts.NodeID = node

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
	conn, err := proxy.Dial(serverOpts)
	if err != nil {
		return err
	}

	defer conn.Close()

	config := &ssh.ClientConfig{
		User:            user,
		Auth:            auth,
		HostKeyCallback: hostKeyCallback,
	}

	clientConn, chans, reqs, err := ssh.NewClientConn(conn, net.JoinHostPort(node, "22"), config)
	if err != nil {
		return fmt.Errorf("ssh handshake failed: %w", err)
	}
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
