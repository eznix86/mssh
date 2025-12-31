package server

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"net"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/eznix86/mssh/internal/stream"
)

// Options describes the server bind address.
type Options struct {
	Host string
	Port int
}

// Server implements the rendezvous service.
type Server struct {
	opts   Options
	mu     sync.Mutex
	agents map[string]*stream.BufferedConn
}

var nodeIDPattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// New initializes a new Server.
func New(opts Options) *Server {
	return &Server{opts: opts, agents: make(map[string]*stream.BufferedConn)}
}

// Run starts accepting incoming connections until the context is canceled.
func (s *Server) Run(ctx context.Context) error {
	addr := fmt.Sprintf("%s:%d", s.opts.Host, s.opts.Port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	log.Printf("[server] listening on %s", addr)

	defer listener.Close()

	go func() {
		<-ctx.Done()
		listener.Close()
	}()

	for {
		conn, err := listener.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return nil
			default:
				log.Printf("[server] accept error: %v", err)
			}
			continue
		}
		go s.handleConn(conn)
	}
}

func (s *Server) handleConn(raw net.Conn) {
	reader := bufio.NewReader(raw)
	raw.SetReadDeadline(time.Now().Add(30 * time.Second))
	line, err := reader.ReadString('\n')
	if err != nil {
		log.Printf("[server] failed reading header: %v", err)
		raw.Close()
		return
	}
	raw.SetReadDeadline(time.Time{})

	parts := strings.Fields(strings.TrimSpace(line))
	if len(parts) != 2 {
		log.Printf("[server] invalid header: %q", line)
		raw.Write([]byte("ERROR: invalid header\n"))
		raw.Close()
		return
	}

	typ := strings.ToUpper(parts[0])
	nodeID := parts[1]
	if !nodeIDPattern.MatchString(nodeID) {
		log.Printf("[server] invalid node-id format: %s", nodeID)
		raw.Write([]byte("ERROR: invalid node-id\n"))
		raw.Close()
		return
	}
	conn := stream.Wrap(raw, reader)

	switch typ {
	case "AGENT":
		s.registerAgent(conn, nodeID)
	case "CLIENT":
		s.handleClient(conn, nodeID)
	default:
		log.Printf("[server] unknown type %q", typ)
		raw.Write([]byte("ERROR: unknown type\n"))
		raw.Close()
	}
}

func (s *Server) registerAgent(conn *stream.BufferedConn, nodeID string) {
	s.mu.Lock()
	if _, exists := s.agents[nodeID]; exists {
		s.mu.Unlock()
		log.Printf("[server] agent collision for node %s", nodeID)
		conn.Write([]byte("ERROR: node-id already registered\n"))
		conn.Close()
		return
	}
	s.agents[nodeID] = conn
	total := len(s.agents)
	s.mu.Unlock()

	log.Printf("[server] agent connected: %s (total: %d)", nodeID, total)
	conn.Write([]byte("OK\n"))
}

func (s *Server) handleClient(conn *stream.BufferedConn, nodeID string) {
	agentConn := s.popAgent(nodeID)
	if agentConn == nil {
		conn.Write([]byte("ERROR: agent offline\n"))
		conn.Close()
		return
	}

	log.Printf("[server] pairing client with %s", nodeID)
	conn.Write([]byte("OK\n"))
	stream.Pipe(agentConn, conn)
	log.Printf("[server] connection closed: %s", nodeID)
}

func (s *Server) popAgent(nodeID string) *stream.BufferedConn {
	s.mu.Lock()
	defer s.mu.Unlock()
	agent, ok := s.agents[nodeID]
	if !ok {
		return nil
	}
	delete(s.agents, nodeID)
	return agent
}
