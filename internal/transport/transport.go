package transport

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/eznix86/mssh/internal/protocol"
)

type Security struct {
	TLS       bool   `yaml:"tls,omitempty"`
	CAFile    string `yaml:"tls_ca,omitempty"`
	TokenFile string `yaml:"token_file,omitempty"`
}

func ParseAddr(address string) (string, int, error) {
	host, value, err := net.SplitHostPort(address)
	if err != nil {
		return "", 0, fmt.Errorf("parse server address: %w", err)
	}
	port, err := strconv.Atoi(value)
	if err != nil || port < 1 || port > 65535 || host == "" {
		return "", 0, fmt.Errorf("server address requires a host and port between 1 and 65535")
	}
	return host, port, nil
}

func ExpandPath(path string) (string, error) {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find home directory: %w", err)
	}
	return filepath.Join(home, strings.TrimPrefix(path, "~")), nil
}

func LoadToken(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	expanded, err := ExpandPath(path)
	if err != nil {
		return "", err
	}
	file, err := os.Open(expanded)
	if err != nil {
		return "", fmt.Errorf("open token file: %w", err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 514))
	if err != nil {
		return "", fmt.Errorf("read token file: %w", err)
	}
	token := strings.TrimSpace(string(data))
	if len(data) > 513 || len(token) < 32 || len(token) > 512 || strings.ContainsAny(token, " \t\r\n") {
		return "", fmt.Errorf("token must contain 32 to 512 characters without whitespace")
	}
	return token, nil
}

func TokenMatches(want, got string) bool {
	a, b := sha256.Sum256([]byte(want)), sha256.Sum256([]byte(got))
	return subtle.ConstantTimeCompare(a[:], b[:]) == 1
}

func Loopback(host string) bool {
	return host == "localhost" || net.ParseIP(host).IsLoopback()
}

func Dial(ctx context.Context, host string, port int, security Security) (net.Conn, error) {
	if port < 1 || port > 65535 {
		return nil, fmt.Errorf("invalid server port")
	}
	if security.TokenFile != "" && !security.TLS && !Loopback(host) {
		return nil, fmt.Errorf("TLS is required when sending a token to a remote server")
	}
	address := net.JoinHostPort(host, strconv.Itoa(port))
	dialer := &net.Dialer{Timeout: protocol.SetupTimeout}
	if !security.TLS {
		if security.CAFile != "" {
			return nil, fmt.Errorf("tls_ca requires TLS")
		}
		return dialer.DialContext(ctx, "tcp", address)
	}
	cfg := &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
	if security.CAFile != "" {
		path, err := ExpandPath(security.CAFile)
		if err != nil {
			return nil, err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read TLS CA: %w", err)
		}
		cfg.RootCAs = x509.NewCertPool()
		if !cfg.RootCAs.AppendCertsFromPEM(data) {
			return nil, fmt.Errorf("TLS CA contains no certificates")
		}
	}
	tlsDialer := &tls.Dialer{NetDialer: dialer, Config: cfg}
	return tlsDialer.DialContext(ctx, "tcp", address)
}

func Listen(host string, port int, certFile, keyFile, token string) (net.Listener, error) {
	if port < 0 || port > 65535 {
		return nil, fmt.Errorf("invalid listen port")
	}
	if !Loopback(host) && (certFile == "" || keyFile == "" || token == "") {
		return nil, fmt.Errorf("public listeners require --tls-cert, --tls-key and --token-file")
	}
	if (certFile == "") != (keyFile == "") {
		return nil, fmt.Errorf("both TLS certificate and key are required")
	}
	var cfg *tls.Config
	if certFile != "" {
		certPath, err := ExpandPath(certFile)
		if err != nil {
			return nil, err
		}
		keyPath, err := ExpandPath(keyFile)
		if err != nil {
			return nil, err
		}
		cert, err := tls.LoadX509KeyPair(certPath, keyPath)
		if err != nil {
			return nil, fmt.Errorf("load TLS certificate: %w", err)
		}
		cfg = &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
	}
	listener, err := net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return nil, fmt.Errorf("listen: %w", err)
	}
	if cfg != nil {
		return tls.NewListener(listener, cfg), nil
	}
	return listener, nil
}
