package proxy

import (
	"bufio"
	"context"
	"fmt"
	"time"

	"github.com/eznix86/mssh/internal/protocol"
	"github.com/eznix86/mssh/internal/stream"
	"github.com/eznix86/mssh/internal/transport"
)

func Dial(ctx context.Context, opts Options) (*stream.BufferedConn, error) {
	token, err := transport.LoadToken(opts.Security.TokenFile)
	if err != nil {
		return nil, err
	}
	header, err := protocol.Header("CLIENT", opts.NodeID, token)
	if err != nil {
		return nil, err
	}
	conn, err := transport.Dial(ctx, opts.Host, opts.Port, opts.Security)
	if err != nil {
		return nil, fmt.Errorf("connect proxy server: %w", err)
	}
	success := false
	defer func() {
		if !success {
			conn.Close()
		}
	}()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	conn.SetDeadline(time.Now().Add(protocol.SetupTimeout + protocol.HeartbeatTimeout))
	if err := protocol.WriteLine(conn, header); err != nil {
		return nil, err
	}
	reader := bufio.NewReader(conn)
	if err := protocol.Expect(reader, "OK"); err != nil {
		return nil, fmt.Errorf("connect node: %w", err)
	}
	conn.SetDeadline(time.Time{})
	success = true
	return stream.Wrap(conn, reader), nil
}
