package sshutil

import (
	"fmt"
	"net"
	"os"

	"golang.org/x/crypto/ssh"
	sshagent "golang.org/x/crypto/ssh/agent"
)

// LoadSSHAgent returns an auth method backed by the SSH agent, if available.
func LoadSSHAgent() (ssh.AuthMethod, func(), error) {
	sock := os.Getenv("SSH_AUTH_SOCK")
	if sock == "" {
		return nil, nil, fmt.Errorf("SSH_AUTH_SOCK not set")
	}

	conn, err := net.Dial("unix", sock)
	if err != nil {
		return nil, nil, err
	}

	agentClient := sshagent.NewClient(conn)
	cleanup := func() { conn.Close() }
	return ssh.PublicKeysCallback(agentClient.Signers), cleanup, nil
}
