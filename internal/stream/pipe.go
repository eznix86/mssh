package stream

import (
	"io"
	"net"
	"sync"
)

// Pipe forwards bytes in both directions until both sides close.
func Pipe(a, b net.Conn) {
	var wg sync.WaitGroup
	copy := func(dst, src net.Conn) {
		defer wg.Done()
		_, _ = io.Copy(dst, src)
		if cw, ok := dst.(interface{ CloseWrite() error }); ok {
			_ = cw.CloseWrite()
		} else {
			_ = dst.Close()
		}
	}

	wg.Add(2)
	go copy(a, b)
	go copy(b, a)
	wg.Wait()
	_ = a.Close()
	_ = b.Close()
}
