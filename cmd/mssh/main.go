package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/alecthomas/kingpin/v2"
	agentpkg "github.com/eznix86/mssh/internal/agent"
	"github.com/eznix86/mssh/internal/proxy"
	"github.com/eznix86/mssh/internal/server"
	"golang.org/x/crypto/ssh"
)

var version = "dev"

const defaultServerAddr = "localhost:8443"

func main() {
	err := runCLI(os.Args[1:])
	if err == nil {
		return
	}
	var exitErr *ssh.ExitError
	if errors.As(err, &exitErr) {
		os.Exit(exitErr.ExitStatus())
	}
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}

func runCLI(args []string) error {
	app := kingpin.New("mssh", "Minimal SSH rendezvous system").Version(version)

	serverCmd := app.Command("server", "Run the rendezvous server")
	serverHost := serverCmd.Flag("host", "Bind address").Default("0.0.0.0").String()
	serverPort := serverCmd.Flag("port", "Listen port").Default("8443").Int()

	agentCmd := app.Command("agent", "Run an agent behind NAT")
	agentNodeID := agentCmd.Arg("node-id", "Unique node identifier (defaults to primary host IP)").Default("").String()
	agentServer := agentCmd.Flag("server", "Rendezvous server host:port").String()
	agentSSHPort := agentCmd.Flag("ssh-port", "Local SSH port to tunnel to").Default("22").Int()

	proxyCmd := app.Command("proxy", "ProxyCommand helper that connects via rendezvous server")
	proxyNodeID := proxyCmd.Arg("node-id", "Node identifier to connect to").Required().String()
	proxyServer := proxyCmd.Flag("server", "Rendezvous server host:port").String()

	sshCmd := app.Command("ssh", "Connect to a node via rendezvous and open an interactive SSH session")
	sshTarget := sshCmd.Arg("target", "Target in the form user@node-id").Required().String()
	sshServer := sshCmd.Flag("server", "Rendezvous server host:port").String()
	sshIdentity := sshCmd.Flag("identity", "Path to private key used for authentication").String()

	configCmd := app.Command("config", "Manage mssh configuration")
	configInitCmd := configCmd.Command("init", "Interactively create or update ~/.mssh/config.yaml")

	if needsImplicitSSH(args) {
		args = append([]string{"ssh"}, args...)
	}

	command, err := app.Parse(args)
	if err != nil {
		return fmt.Errorf("parse arguments: %w", err)
	}
	switch command {
	case serverCmd.FullCommand():
		return runServer(*serverHost, *serverPort)
	case agentCmd.FullCommand():
		serverAddr := *agentServer
		if serverAddr == "" {
			serverAddr = defaultServerAddr
		}
		return runAgent(*agentNodeID, serverAddr, *agentSSHPort)
	case proxyCmd.FullCommand():
		cfg := loadConfig()
		serverAddr, err := resolveServer(*proxyServer, cfg, *proxyNodeID)
		if err != nil {
			return err
		}
		return runProxy(*proxyNodeID, serverAddr)

	case sshCmd.FullCommand():
		cfg := loadConfig()
		user, node, err := parseTarget(*sshTarget)
		if err != nil {
			return err
		}
		serverAddr, err := resolveServer(*sshServer, cfg, node)
		if err != nil {
			return err
		}
		identity := resolveIdentity(*sshIdentity, cfg, node)
		return runSSH(user, node, serverAddr, identity)
	case configInitCmd.FullCommand():
		cfg := loadConfig()
		return runConfigInit(cfg)
	}
	return nil
}
func needsImplicitSSH(args []string) bool {
	return len(args) > 0 && !strings.HasPrefix(args[0], "-") && strings.Contains(args[0], "@")
}
func runServer(host string, port int) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	srv := server.New(server.Options{Host: host, Port: port})
	if err := srv.Run(ctx); err != nil {
		return fmt.Errorf("run server: %w", err)
	}
	return nil
}

func runAgent(nodeID, serverAddr string, sshPort int) error {
	if nodeID == "" {
		nodeID = defaultNodeID()
		if nodeID == "" {
			return fmt.Errorf("unable to determine default node-id; specify one explicitly")
		}
		log.Printf("[agent] auto-detected node-id %s", nodeID)
	} else {
		nodeID = sanitizeNodeID(nodeID)
		if nodeID == "" {
			return fmt.Errorf("provided node-id is empty after sanitization")
		}
	}

	agentOpts, err := agentpkg.ParseServerAddr(serverAddr)
	if err != nil {
		return fmt.Errorf("invalid server address: %w", err)
	}
	agentOpts.NodeID = nodeID
	agentOpts.SSHPort = sshPort

	if err := agentpkg.Run(agentOpts); err != nil {
		return fmt.Errorf("run agent: %w", err)
	}
	return nil
}

func runProxy(nodeID, serverAddr string) error {
	addr, err := proxy.ParseServerAddr(serverAddr)
	if err != nil {
		return fmt.Errorf("invalid server address: %w", err)
	}
	addr.NodeID = nodeID

	if err := proxy.Run(addr, os.Stdin, os.Stdout); err != nil {
		return fmt.Errorf("run proxy: %w", err)
	}
	return nil
}
