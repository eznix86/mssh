package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/eznix86/mssh/internal/sshutil"
	"golang.org/x/crypto/ssh"
	"golang.org/x/term"
)

func buildAuthMethods(identity string) ([]ssh.AuthMethod, func(), error) {
	var auth []ssh.AuthMethod
	var cleanup func()

	candidates := []string{}
	if identity != "" {
		candidates = append(candidates, identity)
	} else {
		candidates = append(candidates, defaultIdentityCandidates()...)
	}

	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		signer, err := loadSigner(candidate)
		if err != nil {
			if identity != "" {
				return nil, nil, fmt.Errorf("load identity %s: %w", candidate, err)
			}
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			fmt.Fprintf(os.Stderr, "Skipping %s: %v\n", candidate, err)
			continue
		}
		auth = append(auth, ssh.PublicKeys(signer))
	}

	if method, agentCleanup, err := sshutil.LoadSSHAgent(); err == nil {
		auth = append(auth, method)
		cleanup = agentCleanup
	}

	if len(auth) == 0 {
		return nil, cleanup, fmt.Errorf("no SSH authentication methods available; specify --identity or run ssh-add")
	}

	return auth, cleanup, nil
}

func defaultIdentityCandidates() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	paths := []string{"id_ed25519", "id_rsa", "id_ecdsa"}
	var expanded []string
	for _, name := range paths {
		expanded = append(expanded, filepath.Join(home, ".ssh", name))
	}
	return expanded
}

func loadSigner(path string) (ssh.Signer, error) {
	expanded, err := expandPath(path)
	if err != nil {
		return nil, err
	}
	keyData, err := os.ReadFile(expanded)
	if err != nil {
		return nil, err
	}

	signer, err := ssh.ParsePrivateKey(keyData)
	if err == nil {
		return signer, nil
	}

	var perr *ssh.PassphraseMissingError
	if !errors.As(err, &perr) {
		return nil, err
	}

	const maxAttempts = 3
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		passphrase, promptErr := promptPassphrase(expanded)
		if promptErr != nil {
			return nil, promptErr
		}
		signer, err = ssh.ParsePrivateKeyWithPassphrase(keyData, passphrase)
		zeroBytes(passphrase)
		if err == nil {
			return signer, nil
		}
		fmt.Fprintf(os.Stderr, "Incorrect passphrase (attempt %d/%d)\n", attempt, maxAttempts)
	}

	return nil, fmt.Errorf("failed to decrypt %s: %w", expanded, err)
}

func expandPath(path string) (string, error) {
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, strings.TrimPrefix(path, "~")), nil
	}
	return path, nil
}

func promptPassphrase(identityPath string) ([]byte, error) {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return nil, fmt.Errorf("passphrase required for %s but stdin is not a terminal; run ssh-add or specify --identity", identityPath)
	}

	fmt.Fprintf(os.Stderr, "Enter passphrase for %s: ", identityPath)
	pass, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return nil, err
	}
	return pass, nil
}

func zeroBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
