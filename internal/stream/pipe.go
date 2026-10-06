package stream

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
)

func Pipe(ctx context.Context, a, b net.Conn) error {
	closeBoth := func() { a.Close(); b.Close() }
	defer closeBoth()
	stop := context.AfterFunc(ctx, closeBoth)
	defer stop()
	results := make(chan error, 2)
	transfer := func(dst, src net.Conn) {
		_, err := io.Copy(dst, src)
		if err != nil {
			closeBoth()
		} else if cw, ok := dst.(interface{ CloseWrite() error }); ok {
			err = cw.CloseWrite()
			if err != nil {
				closeBoth()
			}
		} else {
			dst.Close()
		}
		results <- err
	}
	go transfer(a, b)
	go transfer(b, a)
	first, second := <-results, <-results
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err := errors.Join(first, second); err != nil {
		return fmt.Errorf("copy tunnel: %w", err)
	}
	return nil
}
