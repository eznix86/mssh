package agent

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"net"
	"strconv"
	"time"

	"github.com/eznix86/mssh/internal/protocol"
	"github.com/eznix86/mssh/internal/stream"
	"github.com/eznix86/mssh/internal/transport"
)

type Options struct {
	Host     string
	Port     int
	NodeID   string
	SSHPort  int
	Security transport.Security
}

func ParseServerAddr(addr string) (Options, error) {
	host, port, err := transport.ParseAddr(addr)
	if err != nil {
		return Options{}, err
	}
	return Options{Host: host, Port: port}, nil
}

func Run(ctx context.Context, opts Options) error {
	if !protocol.ValidNode(opts.NodeID) || opts.SSHPort < 1 || opts.SSHPort > 65535 {
		return fmt.Errorf("invalid node-id or SSH port")
	}
	token, err := transport.LoadToken(opts.Security.TokenFile)
	if err != nil {
		return err
	}
	for {
		if err := runOnce(ctx, opts, token); err != nil && ctx.Err() == nil {
			log.Printf("[agent] %v", err)
		}
		timer := time.NewTimer(2 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
}

func runOnce(ctx context.Context, opts Options, token string) error {
	conn, err := transport.Dial(ctx, opts.Host, opts.Port, opts.Security)
	if err != nil {
		return fmt.Errorf("connect rendezvous server: %w", err)
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	conn.SetDeadline(time.Now().Add(protocol.SetupTimeout))
	header, err := protocol.Header("AGENT", opts.NodeID, token)
	if err != nil {
		return err
	}
	if err := protocol.WriteLine(conn, header); err != nil {
		return err
	}
	reader := bufio.NewReader(conn)
	if err := protocol.Expect(reader, "OK"); err != nil {
		return fmt.Errorf("register agent: %w", err)
	}
	log.Printf("[agent] registered as %s", opts.NodeID)
	for {
		conn.SetReadDeadline(time.Now().Add(protocol.HeartbeatTimeout + protocol.HeartbeatInterval))
		message, err := protocol.ReadLine(reader)
		if err != nil {
			return err
		}
		conn.SetWriteDeadline(time.Now().Add(protocol.SetupTimeout))
		switch message {
		case "PING":
			if err := protocol.WriteLine(conn, "PONG"); err != nil {
				return err
			}
		case "CONNECT":
			dialer := &net.Dialer{Timeout: protocol.SetupTimeout}
			sshConn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(opts.SSHPort)))
			if err != nil {
				protocol.WriteLine(conn, "ERROR: local SSH unavailable")
				return fmt.Errorf("connect local SSH: %w", err)
			}
			defer sshConn.Close()
			if err := protocol.WriteLine(conn, "READY"); err != nil {
				return err
			}
			conn.SetDeadline(time.Time{})
			return stream.Pipe(ctx, stream.Wrap(conn, reader), sshConn)
		default:
			return fmt.Errorf("unexpected agent control message %q", message)
		}
	}
}
