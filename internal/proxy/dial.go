package proxy

import (
	"bufio"
	"fmt"
	"net"
	"strings"

	"github.com/eznix86/mssh/internal/stream"
)

// Dial establishes a rendezvous proxy connection and returns a buffered connection.
func Dial(opts Options) (*stream.BufferedConn, error) {
	conn, err := net.Dial("tcp", fmt.Sprintf("%s:%d", opts.Host, opts.Port))
	if err != nil {
		return nil, fmt.Errorf("connect proxy server: %w", err)
	}

	if _, err := fmt.Fprintf(conn, "CLIENT %s\n", opts.NodeID); err != nil {
		conn.Close()
		return nil, fmt.Errorf("send client header: %w", err)
	}

	reader := bufio.NewReader(conn)
	response, err := reader.ReadString('\n')
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("reading server response: %w", err)
	}

	trim := strings.TrimSpace(response)
	if strings.HasPrefix(trim, "ERROR:") {
		conn.Close()
		return nil, fmt.Errorf(trim)
	}

	return stream.Wrap(conn, reader), nil
}
