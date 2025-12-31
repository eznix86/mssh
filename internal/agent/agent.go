package agent

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/eznix86/mssh/internal/stream"
)

// Options defines how the agent connects.
type Options struct {
	Host    string
	Port    int
	NodeID  string
	SSHPort int
}

// ParseServerAddr converts host:port into Options with only network fields set.
func ParseServerAddr(addr string) (Options, error) {
	host, port, err := parseAddr(addr)
	if err != nil {
		return Options{}, err
	}
	return Options{Host: host, Port: port}, nil
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

// Run connects the agent to the rendezvous server and continually proxies SSH traffic.
func Run(opts Options) error {
	for {
		if err := runOnce(opts); err != nil {
			log.Printf("[agent] error: %v", err)
		}
		log.Printf("[agent] reconnecting to rendezvous server in 2s...")
		time.Sleep(2 * time.Second)
	}
}

func runOnce(opts Options) error {
	srvAddr := fmt.Sprintf("%s:%d", opts.Host, opts.Port)
	conn, err := net.Dial("tcp", srvAddr)
	if err != nil {
		return fmt.Errorf("connect to server: %w", err)
	}
	defer conn.Close()

	if _, err := fmt.Fprintf(conn, "AGENT %s\n", opts.NodeID); err != nil {
		return fmt.Errorf("register agent: %w", err)
	}

	reader := bufio.NewReader(conn)
	response, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("wait ack: %w", err)
	}
	if strings.TrimSpace(response) != "OK" {
		return fmt.Errorf("registration failed: %s", strings.TrimSpace(response))
	}

	sshConn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", opts.SSHPort))
	if err != nil {
		return fmt.Errorf("connect to ssh: %w", err)
	}
	defer sshConn.Close()

	log.Printf("[agent] registered as %s, piping traffic", opts.NodeID)
	serverConn := stream.Wrap(conn, reader)
	stream.Pipe(serverConn, sshConn)
	log.Printf("[agent] client disconnected")
	return nil
}
