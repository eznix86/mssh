package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/crypto/ssh"
	"golang.org/x/term"
)

func prepareTerminal(session *ssh.Session) (func(), error) {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return func() {}, nil
	}

	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return nil, fmt.Errorf("set terminal raw mode: %w", err)
	}

	width, height, err := term.GetSize(fd)
	if err != nil {
		width, height = 80, 24
	}

	termName := os.Getenv("TERM")
	if termName == "" {
		termName = "xterm-256color"
	}

	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}

	if err := session.RequestPty(termName, height, width, modes); err != nil {
		term.Restore(fd, oldState)
		return nil, fmt.Errorf("request SSH terminal: %w", err)
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGWINCH)

	go func() {
		for range sigCh {
			if w, h, err := term.GetSize(fd); err == nil {
				_ = session.WindowChange(h, w)
			}
		}
	}()

	return func() {
		signal.Stop(sigCh)
		close(sigCh)
		term.Restore(fd, oldState)
	}, nil
}
