package config

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/eznix86/mssh/internal/transport"
	"gopkg.in/yaml.v3"
)

// ErrNotFound is returned when the config file does not exist.
var ErrNotFound = errors.New("config not found")

// Config represents the structure stored in ~/.mssh/config.yaml.
type Config struct {
	Server             string               `yaml:"server"`
	Identity           string               `yaml:"identity,omitempty"`
	Nodes              map[string]NodeEntry `yaml:"nodes,omitempty"`
	transport.Security `yaml:",inline"`
}

// NodeEntry contains optional overrides for a specific node-id.
type NodeEntry struct {
	Server    string `yaml:"server,omitempty"`
	Identity  string `yaml:"identity,omitempty"`
	TLS       *bool  `yaml:"tls,omitempty"`
	CAFile    string `yaml:"tls_ca,omitempty"`
	TokenFile string `yaml:"token_file,omitempty"`
}

// Path returns the path to the config file.
func Path() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".mssh", "config.yaml"), nil
}

// Load reads the config file.
func Load() (Config, error) {
	path, err := Path()
	if err != nil {
		return Config{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Config{}, ErrNotFound
		}
		return Config{}, err
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// Save writes the config to disk, creating the directory if needed.
func Save(cfg Config) error {
	path, err := Path()
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// ServerFor returns the server override for a node or the global default.
func (c Config) ServerFor(nodeID string) string {
	if nodeID != "" && c.Nodes != nil {
		if entry, ok := c.Nodes[nodeID]; ok && entry.Server != "" {
			return entry.Server
		}
	}
	return c.Server
}

// IdentityFor returns the identity override for a node or the global default.
func (c Config) IdentityFor(nodeID string) string {
	if nodeID != "" && c.Nodes != nil {
		if entry, ok := c.Nodes[nodeID]; ok && entry.Identity != "" {
			return entry.Identity
		}
	}
	return c.Identity
}

func (c Config) SecurityFor(node string) transport.Security {
	security := c.Security
	entry, ok := c.Nodes[node]
	if !ok {
		return security
	}
	if entry.TLS != nil {
		security.TLS = *entry.TLS
	}
	if entry.CAFile != "" {
		security.CAFile = entry.CAFile
	}
	if entry.TokenFile != "" {
		security.TokenFile = entry.TokenFile
	}
	return security
}
