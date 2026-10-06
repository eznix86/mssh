package server

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"github.com/eznix86/mssh/internal/protocol"
	"github.com/eznix86/mssh/internal/stream"
	"github.com/eznix86/mssh/internal/transport"
)

type Options struct {
	Host           string
	Port           int
	TLSCert        string
	TLSKey         string
	TokenFile      string
	MaxConnections int
}

type registration struct {
	pair chan *stream.BufferedConn
	busy bool
	done chan struct{}
}

type Server struct {
	opts        Options
	mu          sync.Mutex
	agents      map[string]*registration
	connections map[net.Conn]struct{}
	token       string
	heartbeat   time.Duration
}

func New(opts Options) *Server {
	if opts.MaxConnections <= 0 {
		opts.MaxConnections = 1024
	}
	return &Server{
		opts: opts, agents: make(map[string]*registration), connections: make(map[net.Conn]struct{}),
		heartbeat: protocol.HeartbeatInterval,
	}
}

func (s *Server) Run(ctx context.Context) error {
	token, err := transport.LoadToken(s.opts.TokenFile)
	if err != nil {
		return err
	}
	listener, err := transport.Listen(s.opts.Host, s.opts.Port, s.opts.TLSCert, s.opts.TLSKey, token)
	if err != nil {
		return err
	}
	return s.serve(ctx, listener, token)
}

func (s *Server) serve(ctx context.Context, listener net.Listener, token string) error {
	s.token = token
	log.Printf("[server] listening on %s", listener.Addr())
	defer listener.Close()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var handlers sync.WaitGroup
	shutdownDone := make(chan struct{})
	context.AfterFunc(ctx, func() {
		defer close(shutdownDone)
		listener.Close()
		s.mu.Lock()
		for conn := range s.connections {
			conn.SetDeadline(time.Now())
			conn.Close()
		}
		s.mu.Unlock()
	})
	defer func() { cancel(); <-shutdownDone; handlers.Wait() }()
	slots := make(chan struct{}, s.opts.MaxConnections)
	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("accept connection: %w", err)
		}
		select {
		case slots <- struct{}{}:
		default:
			conn.Close()
			continue
		}
		s.mu.Lock()
		if ctx.Err() != nil {
			s.mu.Unlock()
			conn.Close()
			<-slots
			return nil
		}
		s.connections[conn] = struct{}{}
		s.mu.Unlock()
		handlers.Add(1)
		go func() {
			defer handlers.Done()
			defer func() {
				conn.Close()
				s.mu.Lock()
				delete(s.connections, conn)
				s.mu.Unlock()
				<-slots
			}()
			s.handleConn(ctx, conn)
		}()
	}
}

func (s *Server) handleConn(ctx context.Context, raw net.Conn) {
	raw.SetDeadline(time.Now().Add(protocol.SetupTimeout))
	reader := bufio.NewReader(raw)
	line, err := protocol.ReadLine(reader)
	if err != nil {
		return
	}
	kind, node, token, err := protocol.ParseHeader(line)
	if err != nil {
		protocol.WriteLine(raw, "ERROR: invalid protocol header")
		return
	}
	if !transport.TokenMatches(s.token, token) {
		protocol.WriteLine(raw, "ERROR: unauthorized")
		return
	}
	conn := stream.Wrap(raw, reader)
	switch kind {
	case "AGENT":
		s.registerAgent(ctx, conn, node)
	case "CLIENT":
		s.handleClient(ctx, conn, node)
	}
}

func (s *Server) registerAgent(ctx context.Context, conn *stream.BufferedConn, node string) {
	entry := &registration{pair: make(chan *stream.BufferedConn, 1), done: make(chan struct{})}
	s.mu.Lock()
	if _, exists := s.agents[node]; exists {
		s.mu.Unlock()
		protocol.WriteLine(conn, "ERROR: node-id already registered")
		return
	}
	s.agents[node] = entry
	s.mu.Unlock()
	defer func() {
		defer close(entry.done)
		s.mu.Lock()
		if s.agents[node] == entry {
			delete(s.agents, node)
		}
		s.mu.Unlock()
	}()
	if err := protocol.WriteLine(conn, "OK"); err != nil {
		return
	}
	conn.SetDeadline(time.Time{})
	ticker := time.NewTicker(s.heartbeat)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case client := <-entry.pair:
			conn.SetDeadline(time.Now().Add(protocol.SetupTimeout))
			if err := protocol.WriteLine(conn, "CONNECT"); err != nil {
				return
			}
			if err := protocol.Expect(conn.Reader(), "READY"); err != nil {
				protocol.WriteLine(client, "ERROR: agent could not open SSH")
				return
			}
			if err := protocol.WriteLine(client, "OK"); err != nil {
				return
			}
			conn.SetDeadline(time.Time{})
			client.SetDeadline(time.Time{})
			if err := stream.Pipe(ctx, conn, client); err != nil && ctx.Err() == nil {
				log.Printf("[server] tunnel %s: %v", node, err)
			}
			return
		case <-ticker.C:
			conn.SetDeadline(time.Now().Add(protocol.HeartbeatTimeout))
			if err := protocol.WriteLine(conn, "PING"); err != nil {
				return
			}
			if err := protocol.Expect(conn.Reader(), "PONG"); err != nil {
				return
			}
			conn.SetDeadline(time.Time{})
		}
	}
}

func (s *Server) handleClient(ctx context.Context, conn *stream.BufferedConn, node string) {
	s.mu.Lock()
	entry := s.agents[node]
	if entry == nil {
		s.mu.Unlock()
		protocol.WriteLine(conn, "ERROR: agent offline")
		return
	}
	if entry.busy {
		s.mu.Unlock()
		protocol.WriteLine(conn, "ERROR: agent busy")
		return
	}
	entry.busy = true
	conn.SetDeadline(time.Now().Add(protocol.SetupTimeout + protocol.HeartbeatTimeout))
	entry.pair <- conn
	s.mu.Unlock()
	select {
	case <-ctx.Done():
	case <-entry.done:
	}
}
