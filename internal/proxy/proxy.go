package proxy

import (
	"io"
	"net"
	"strconv"
)

// Options describes how to connect to the rendezvous server.
type Options struct {
	Host   string
	Port   int
	NodeID string
}

// ParseServerAddr converts host:port to Options with network fields populated.
func ParseServerAddr(addr string) (Options, error) {
	host, port, err := parseAddr(addr)
	if err != nil {
		return Options{}, err
	}
	return Options{Host: host, Port: port}, nil
}

// Run dials the rendezvous server and proxies stdin/stdout through it.
func Run(opts Options, stdin io.Reader, stdout io.Writer) error {
	serverConn, err := Dial(opts)
	if err != nil {
		return err
	}
	defer serverConn.Close()

	done := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(serverConn, stdin)
		_ = serverConn.CloseWrite()
		done <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(stdout, serverConn)
		done <- struct{}{}
	}()

	<-done
	<-done
	return nil
}

func parseAddr(raw string) (string, int, error) {
	host, portStr, err := net.SplitHostPort(raw)
	if err != nil {
		return "", 0, err
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return "", 0, err
	}
	return host, port, nil
}
