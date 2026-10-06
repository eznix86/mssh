package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/eznix86/mssh/internal/config"
)

func parseTarget(target string) (string, string, error) {
	separator := strings.LastIndex(target, "@")
	if separator < 0 {
		return "", "", fmt.Errorf("target must be user@node-id, got %q", target)
	}
	user := target[:separator]
	if user == "" {
		return "", "", fmt.Errorf("missing user in %q", target)
	}
	node := target[separator+1:]
	if node == "" {
		return "", "", fmt.Errorf("missing node-id in %q", target)
	}
	return user, node, nil
}

func loadConfig() (config.Config, error) {
	cfg, err := config.Load()
	if errors.Is(err, config.ErrNotFound) {
		return config.Config{}, nil
	}
	if err != nil {
		return config.Config{}, fmt.Errorf("load config: %w", err)
	}
	return cfg, nil
}

func resolveServer(flagValue string, cfg config.Config, nodeID string) (string, error) {
	if flagValue != "" {
		return flagValue, nil
	}
	if server := cfg.ServerFor(nodeID); server != "" {
		return server, nil
	}
	return "", fmt.Errorf("no server configured; run 'mssh config init' or pass --server")
}

func resolveIdentity(flagValue string, cfg config.Config, nodeID string) string {
	if flagValue != "" {
		return flagValue
	}
	return cfg.IdentityFor(nodeID)
}
