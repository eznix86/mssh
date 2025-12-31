package sshutil

import (
	"fmt"
	"net"
	"os"
	"path/filepath"

	"golang.org/x/crypto/ssh"
	sshagent "golang.org/x/crypto/ssh/agent"
)

// LoadDefaultKeyMethods scans ~/.ssh for common private key names.
func LoadDefaultKeyMethods() []ssh.AuthMethod {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	keys := []string{"id_ed25519", "id_rsa", "id_ecdsa"}
	var methods []ssh.AuthMethod
	for _, name := range keys {
		path := filepath.Join(home, ".ssh", name)
		if method := loadKeyFromPath(path); method != nil {
			methods = append(methods, method)
		}
	}
	return methods
}

func loadKeyFromPath(path string) ssh.AuthMethod {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	signer, err := ssh.ParsePrivateKey(data)
	if err != nil {
		return nil
	}
	return ssh.PublicKeys(signer)
}

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
