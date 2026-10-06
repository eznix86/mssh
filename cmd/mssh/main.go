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
	"github.com/eznix86/mssh/internal/config"
	"github.com/eznix86/mssh/internal/proxy"
	"github.com/eznix86/mssh/internal/server"
	"github.com/eznix86/mssh/internal/transport"
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
	serverHost := serverCmd.Flag("host", "Bind address").Default("127.0.0.1").String()
	serverPort := serverCmd.Flag("port", "Listen port").Default("8443").Int()

	serverCert := serverCmd.Flag("tls-cert", "TLS certificate file").String()
	serverKey := serverCmd.Flag("tls-key", "TLS private key file").String()
	serverToken := serverCmd.Flag("token-file", "Shared rendezvous token file").String()
	agentCmd := app.Command("agent", "Run an agent behind NAT")
	agentNodeID := agentCmd.Arg("node-id", "Unique node identifier (defaults to primary host IP)").Default("").String()
	agentFlags := addConnectionFlags(agentCmd)
	agentSSHPort := agentCmd.Flag("ssh-port", "Local SSH port to tunnel to").Default("22").Int()

	proxyCmd := app.Command("proxy", "ProxyCommand helper that connects via rendezvous server")
	proxyNodeID := proxyCmd.Arg("node-id", "Node identifier to connect to").Required().String()
	proxyFlags := addConnectionFlags(proxyCmd)

	sshCmd := app.Command("ssh", "Connect to a node via rendezvous and open an interactive SSH session")
	sshTarget := sshCmd.Arg("target", "Target in the form user@node-id").Required().String()
	sshFlags := addConnectionFlags(sshCmd)
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
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	switch command {
	case serverCmd.FullCommand():
		return runServer(ctx, server.Options{
			Host: *serverHost, Port: *serverPort, TLSCert: *serverCert,
			TLSKey: *serverKey, TokenFile: *serverToken,
		})
	case agentCmd.FullCommand():
		return runAgent(ctx, *agentNodeID, agentFlags, *agentSSHPort)
	case proxyCmd.FullCommand():
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		serverAddr, err := resolveServer(*proxyFlags.server, cfg, *proxyNodeID)
		if err != nil {
			return err
		}
		return runProxy(ctx, *proxyNodeID, serverAddr, proxyFlags.security(cfg, *proxyNodeID))

	case sshCmd.FullCommand():
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		user, node, err := parseTarget(*sshTarget)
		if err != nil {
			return err
		}
		serverAddr, err := resolveServer(*sshFlags.server, cfg, node)
		if err != nil {
			return err
		}
		identity := resolveIdentity(*sshIdentity, cfg, node)
		return runSSH(ctx, user, node, serverAddr, identity, sshFlags.security(cfg, node))
	case configInitCmd.FullCommand():
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		return runConfigInit(cfg)
	}
	return nil
}
func needsImplicitSSH(args []string) bool {
	return len(args) > 0 && !strings.HasPrefix(args[0], "-") && strings.Contains(args[0], "@")
}
func runServer(ctx context.Context, opts server.Options) error {
	return server.New(opts).Run(ctx)
}

func runAgent(ctx context.Context, nodeID string, flags connectionFlags, sshPort int) error {
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

	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	serverAddr, err := resolveServer(*flags.server, cfg, nodeID)
	if err != nil {
		serverAddr = defaultServerAddr
	}
	agentOpts, err := agentpkg.ParseServerAddr(serverAddr)
	if err != nil {
		return fmt.Errorf("invalid server address: %w", err)
	}
	agentOpts.NodeID = nodeID
	agentOpts.SSHPort = sshPort
	agentOpts.Security = flags.security(cfg, nodeID)

	if err := agentpkg.Run(ctx, agentOpts); err != nil {
		return fmt.Errorf("run agent: %w", err)
	}
	return nil
}

func runProxy(ctx context.Context, nodeID, serverAddr string, security transport.Security) error {
	addr, err := proxy.ParseServerAddr(serverAddr)
	if err != nil {
		return fmt.Errorf("invalid server address: %w", err)
	}
	addr.NodeID = nodeID
	addr.Security = security

	if err := proxy.Run(ctx, addr, os.Stdin, os.Stdout); err != nil {
		return fmt.Errorf("run proxy: %w", err)
	}
	return nil
}

type connectionFlags struct {
	server *string
	tls    *bool
	tlsSet *bool
	ca     *string
	token  *string
}

func addConnectionFlags(command *kingpin.CmdClause) connectionFlags {
	tlsSet := new(bool)
	return connectionFlags{
		tlsSet: tlsSet,
		server: command.Flag("server", "Rendezvous server host:port").String(),
		tls:    command.Flag("tls", "Use verified TLS for rendezvous").IsSetByUser(tlsSet).Bool(),
		ca:     command.Flag("tls-ca", "TLS CA certificate file (enables TLS)").String(),
		token:  command.Flag("token-file", "Shared rendezvous token file").String(),
	}
}

func (flags connectionFlags) security(cfg config.Config, node string) transport.Security {
	security := cfg.SecurityFor(node)
	if *flags.tls || (flags.tlsSet != nil && *flags.tlsSet) {
		security.TLS = *flags.tls
		if !security.TLS {
			security.CAFile = ""
		}
	}
	if *flags.ca != "" {
		security.CAFile = *flags.ca
	}
	if *flags.token != "" {
		security.TokenFile = *flags.token
	}
	if security.CAFile != "" {
		security.TLS = true
	}
	return security
}
