package proxy

import (
	"context"
	"fmt"
	"io"

	"github.com/eznix86/mssh/internal/transport"
)

type Options struct {
	Host     string
	Port     int
	NodeID   string
	Security transport.Security
}

func ParseServerAddr(addr string) (Options, error) {
	host, port, err := transport.ParseAddr(addr)
	if err != nil {
		return Options{}, err
	}
	return Options{Host: host, Port: port}, nil
}

func Run(ctx context.Context, opts Options, stdin io.ReadCloser, stdout io.Writer) error {
	conn, err := Dial(ctx, opts)
	if err != nil {
		return err
	}
	defer conn.Close()
	defer stdin.Close()
	stop := context.AfterFunc(ctx, func() { conn.Close(); stdin.Close() })
	defer stop()
	input, output := make(chan error, 1), make(chan error, 1)
	go func() {
		_, err := io.Copy(conn, stdin)
		if err == nil {
			err = conn.CloseWrite()
		}
		input <- err
	}()
	go func() { _, err := io.Copy(stdout, conn); output <- err }()
	select {
	case err := <-output:
		conn.Close()
		stdin.Close()
		<-input
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			return fmt.Errorf("copy proxy output: %w", err)
		}
		return nil
	case err := <-input:
		if err != nil {
			conn.Close()
		}
		outputErr := <-output
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			return fmt.Errorf("copy proxy input: %w", err)
		}
		if outputErr != nil {
			return fmt.Errorf("copy proxy output: %w", outputErr)
		}
		return nil
	}
}
