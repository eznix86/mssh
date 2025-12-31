package stream

import (
	"bufio"
	"net"
)

// BufferedConn wraps a net.Conn so that reads go through a bufio.Reader.
type BufferedConn struct {
	net.Conn
	reader *bufio.Reader
}

// Wrap creates a BufferedConn using an existing bufio.Reader.
func Wrap(conn net.Conn, reader *bufio.Reader) *BufferedConn {
	return &BufferedConn{Conn: conn, reader: reader}
}

// New creates a BufferedConn with a fresh bufio.Reader.
func New(conn net.Conn) *BufferedConn {
	return Wrap(conn, bufio.NewReader(conn))
}

// Read satisfies the io.Reader interface, ensuring buffered data is consumed first.
func (b *BufferedConn) Read(p []byte) (int, error) {
	return b.reader.Read(p)
}

// CloseWrite closes the write side of the underlying connection if possible.
func (b *BufferedConn) CloseWrite() error {
	if cw, ok := b.Conn.(interface{ CloseWrite() error }); ok {
		return cw.CloseWrite()
	}
	return b.Conn.Close()
}

// CloseRead closes the read side of the underlying connection if possible.
func (b *BufferedConn) CloseRead() error {
	if cr, ok := b.Conn.(interface{ CloseRead() error }); ok {
		return cr.CloseRead()
	}
	return b.Conn.Close()
}
