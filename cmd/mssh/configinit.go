package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/eznix86/mssh/internal/config"
	"golang.org/x/term"
)

func runConfigInit(existing config.Config) error {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return fmt.Errorf("config init requires an interactive terminal")
	}

	reader := bufio.NewReader(os.Stdin)
	fmt.Println("This utility will create ~/.mssh/config.yaml")

	server, err := promptServer(reader, existing.Server)
	if err != nil {
		return fmt.Errorf("read server: %w", err)
	}
	identity, err := promptIdentity(reader, existing.Identity)
	if err != nil {
		return fmt.Errorf("read identity: %w", err)
	}

	existing.Server = server
	existing.Identity = strings.TrimSpace(identity)

	if err := config.Save(existing); err != nil {
		return err
	}
	path, err := config.Path()
	if err == nil {
		fmt.Printf("Configuration written to %s\n", path)
	}
	return nil
}

func promptServer(reader *bufio.Reader, current string) (string, error) {
	for {
		label := "Enter rendezvous server (host:port)"
		if current != "" {
			label = fmt.Sprintf("%s [%s]", label, current)
		}
		fmt.Printf("%s: ", label)
		input, err := reader.ReadString('\n')
		if err != nil {
			return "", err
		}
		value := strings.TrimSpace(input)
		if value == "" {
			value = current
		}
		if value != "" {
			return value, nil
		}
		fmt.Println("Server is required.")
	}
}

func promptIdentity(reader *bufio.Reader, current string) (string, error) {
	label := "Default identity path (leave blank to auto-detect from ~/.ssh)"
	if current != "" {
		label = fmt.Sprintf("%s [%s]", label, current)
	}
	fmt.Printf("%s: ", label)
	input, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	value := strings.TrimSpace(input)
	if value == "" {
		return current, nil
	}
	return value, nil
}
