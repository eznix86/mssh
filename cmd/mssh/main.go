package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"

	"github.com/alecthomas/kingpin/v2"
	"golang.org/x/crypto/ssh"
	"golang.org/x/term"

	agentpkg "github.com/eznix86/mssh/internal/agent"
	"github.com/eznix86/mssh/internal/config"
	"github.com/eznix86/mssh/internal/proxy"
	"github.com/eznix86/mssh/internal/server"
	"github.com/eznix86/mssh/internal/sshutil"
)

var (
	version   = "dev"
	commit    = "local"
	buildDate = "unknown"
)

const (
	defaultServerAddr = "localhost:8443"
)

func main() {
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

	args := os.Args[1:]
	if needsImplicitSSH(args) {
		args = append([]string{"ssh"}, args...)
	}

	cfg := loadConfig()

	switch kingpin.MustParse(app.Parse(args)) {
	case serverCmd.FullCommand():
		runServer(*serverHost, *serverPort)
	case agentCmd.FullCommand():
		serverAddr, err := resolveServer(*agentServer, cfg, *agentNodeID)
		if err != nil {
			log.Fatalf("[agent] %v", err)
		}
		runAgent(*agentNodeID, serverAddr, *agentSSHPort)
	case proxyCmd.FullCommand():
		serverAddr, err := resolveServer(*proxyServer, cfg, *proxyNodeID)
		if err != nil {
			log.Fatalf("[proxy] %v", err)
		}
		runProxy(*proxyNodeID, serverAddr)
	case sshCmd.FullCommand():
		user, node, err := parseTarget(*sshTarget)
		if err != nil {
			log.Fatalf("[ssh] %v", err)
		}
		serverAddr, err := resolveServer(*sshServer, cfg, node)
		if err != nil {
			log.Fatalf("[ssh] %v", err)
		}
		identity := resolveIdentity(*sshIdentity, cfg, node)
		if err := runSSH(user, node, serverAddr, identity); err != nil {
			log.Fatalf("[ssh] %v", err)
		}
	case configInitCmd.FullCommand():
		if err := runConfigInit(cfg); err != nil {
			log.Fatalf("[config] %v", err)
		}
		return
	}
}

func needsImplicitSSH(args []string) bool {
	if len(args) == 0 {
		return false
	}
	first := args[0]
	if strings.HasPrefix(first, "-") {
		return false
	}
	switch first {
	case "server", "agent", "proxy", "ssh", "help", "--help", "-h", "version", "--version", "-v":
		return false
	}
	return strings.Contains(first, "@")
}

func runServer(host string, port int) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Println("[server] received interrupt, shutting down...")
		cancel()
	}()

	srv := server.New(server.Options{Host: host, Port: port})
	if err := srv.Run(ctx); err != nil {
		log.Fatalf("[server] %v", err)
	}
}

func runAgent(nodeID, serverAddr string, sshPort int) {
	if nodeID == "" {
		nodeID = defaultNodeID()
		if nodeID == "" {
			log.Fatalf("[agent] unable to determine default node-id; specify one explicitly")
		}
		log.Printf("[agent] auto-detected node-id %s", nodeID)
	} else {
		nodeID = sanitizeNodeID(nodeID)
		if nodeID == "" {
			log.Fatalf("[agent] provided node-id is empty after sanitization")
		}
	}

	agentOpts, err := agentpkg.ParseServerAddr(serverAddr)
	if err != nil {
		log.Fatalf("[agent] invalid server address: %v", err)
	}
	agentOpts.NodeID = nodeID
	agentOpts.SSHPort = sshPort

	if err := agentpkg.Run(agentOpts); err != nil {
		log.Fatalf("[agent] %v", err)
	}
}

func runProxy(nodeID, serverAddr string) {
	addr, err := proxy.ParseServerAddr(serverAddr)
	if err != nil {
		log.Fatalf("[proxy] invalid server address: %v", err)
	}
	addr.NodeID = nodeID

	if err := proxy.Run(addr, os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func runSSH(user, node, serverAddr, identity string) error {
	serverOpts, err := proxy.ParseServerAddr(serverAddr)
	if err != nil {
		return fmt.Errorf("invalid server address: %w", err)
	}
	serverOpts.NodeID = node

	conn, err := proxy.Dial(serverOpts)
	if err != nil {
		return err
	}

	auth, cleanupAgent, err := buildAuthMethods(identity)
	if err != nil {
		return err
	}
	if cleanupAgent != nil {
		defer cleanupAgent()
	}

	if len(auth) == 0 {
		return fmt.Errorf("no SSH authentication methods available; provide --identity or configure SSH_AUTH_SOCK")
	}

	config := &ssh.ClientConfig{
		User:            user,
		Auth:            auth,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}

	clientConn, chans, reqs, err := ssh.NewClientConn(conn, node, config)
	if err != nil {
		return fmt.Errorf("ssh handshake failed: %w", err)
	}
	client := ssh.NewClient(clientConn, chans, reqs)
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("create SSH session: %w", err)
	}
	defer session.Close()

	restore := prepareTerminal(session)
	defer restore()

	session.Stdout = os.Stdout
	session.Stderr = os.Stderr
	session.Stdin = os.Stdin

	if err := session.Shell(); err != nil {
		return fmt.Errorf("start shell: %w", err)
	}

	if err := session.Wait(); err != nil {
		var exitErr *ssh.ExitError
		if errors.As(err, &exitErr) {
			os.Exit(exitErr.ExitStatus())
		}
		return err
	}

	return nil
}

func parseTarget(target string) (string, string, error) {
	parts := strings.Split(target, "@")
	if len(parts) != 2 {
		return "", "", fmt.Errorf("target must be user@node-id, got %q", target)
	}
	user := parts[0]
	if user == "" {
		return "", "", fmt.Errorf("missing user in %q", target)
	}
	node := parts[1]
	if node == "" {
		return "", "", fmt.Errorf("missing node-id in %q", target)
	}
	return user, node, nil
}

func loadConfig() config.Config {
	cfg, err := config.Load()
	if err != nil {
		if errors.Is(err, config.ErrNotFound) {
			return config.Config{}
		}
		log.Fatalf("[config] failed to load config: %v", err)
	}
	return cfg
}

func resolveServer(flagValue string, cfg config.Config, nodeID string) (string, error) {
	if flagValue != "" {
		return flagValue, nil
	}
	if server := cfg.ServerFor(nodeID); server != "" {
		return server, nil
	}
	return defaultServerAddr, nil
}

func resolveIdentity(flagValue string, cfg config.Config, nodeID string) string {
	if flagValue != "" {
		return flagValue
	}
	if identity := cfg.IdentityFor(nodeID); identity != "" {
		return identity
	}
	if cfg.Identity != "" {
		return cfg.Identity
	}
	return ""
}

var nodeIDSanitizePattern = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func defaultNodeID() string {
	if ip := primaryIPv4(); ip != "" {
		return ip
	}
	if host, err := os.Hostname(); err == nil {
		return sanitizeNodeID(host)
	}
	return ""
}

func primaryIPv4() string {
	ifs, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, iface := range ifs {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ipNet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			ip := ipNet.IP.To4()
			if ip == nil {
				continue
			}
			if !ip.IsGlobalUnicast() {
				continue
			}
			return ip.String()
		}
	}
	return ""
}

func sanitizeNodeID(value string) string {
	cleaned := nodeIDSanitizePattern.ReplaceAllString(value, "-")
	return strings.Trim(cleaned, "-")
}

func runConfigInit(existing config.Config) error {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return fmt.Errorf("config init requires an interactive terminal")
	}

	reader := bufio.NewReader(os.Stdin)
	fmt.Println("This utility will create ~/.mssh/config.yaml")

	server := promptServer(reader, existing.Server)
	identity := promptIdentity(reader, existing.Identity)

	existing.Server = server
	existing.Identity = strings.TrimSpace(identity)

	if err := config.Save(existing); err != nil {
		return err
	}
	path, err := config.Path()
	if err == nil {
		fmt.Printf("Configuration written to %s\n", path)
	}
	return nil
}

func promptServer(reader *bufio.Reader, current string) string {
	for {
		label := "Enter rendezvous server (host:port)"
		if current != "" {
			label = fmt.Sprintf("%s [%s]", label, current)
		}
		fmt.Printf("%s: ", label)
		input, _ := reader.ReadString('\n')
		value := strings.TrimSpace(input)
		if value == "" {
			value = current
		}
		if value != "" {
			return value
		}
		fmt.Println("Server is required.")
	}
}

func promptIdentity(reader *bufio.Reader, current string) string {
	label := "Default identity path (leave blank to auto-detect from ~/.ssh)"
	if current != "" {
		label = fmt.Sprintf("%s [%s]", label, current)
	}
	fmt.Printf("%s: ", label)
	input, _ := reader.ReadString('\n')
	value := strings.TrimSpace(input)
	if value == "" {
		return current
	}
	return value
}

func buildAuthMethods(identity string) ([]ssh.AuthMethod, func(), error) {
	var auth []ssh.AuthMethod
	var cleanup func()

	candidates := []string{}
	if identity != "" {
		candidates = append(candidates, identity)
	} else {
		candidates = append(candidates, defaultIdentityCandidates()...)
	}

	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		signer, err := loadSigner(candidate)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			fmt.Fprintf(os.Stderr, "Skipping %s: %v\n", candidate, err)
			continue
		}
		auth = append(auth, ssh.PublicKeys(signer))
	}

	if method, agentCleanup, err := sshutil.LoadSSHAgent(); err == nil {
		auth = append(auth, method)
		cleanup = agentCleanup
	}

	if len(auth) == 0 {
		return nil, cleanup, fmt.Errorf("no SSH authentication methods available; specify --identity or run ssh-add")
	}

	return auth, cleanup, nil
}

func defaultIdentityCandidates() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	paths := []string{"id_ed25519", "id_rsa", "id_ecdsa"}
	var expanded []string
	for _, name := range paths {
		expanded = append(expanded, filepath.Join(home, ".ssh", name))
	}
	return expanded
}

func loadSigner(path string) (ssh.Signer, error) {
	expanded, err := expandPath(path)
	if err != nil {
		return nil, err
	}
	keyData, err := os.ReadFile(expanded)
	if err != nil {
		return nil, err
	}

	signer, err := ssh.ParsePrivateKey(keyData)
	if err == nil {
		return signer, nil
	}

	var perr *ssh.PassphraseMissingError
	if !errors.As(err, &perr) {
		return nil, err
	}

	const maxAttempts = 3
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		passphrase, promptErr := promptPassphrase(expanded)
		if promptErr != nil {
			return nil, promptErr
		}
		signer, err = ssh.ParsePrivateKeyWithPassphrase(keyData, passphrase)
		zeroBytes(passphrase)
		if err == nil {
			return signer, nil
		}
		fmt.Fprintf(os.Stderr, "Incorrect passphrase (attempt %d/%d)\n", attempt, maxAttempts)
	}

	return nil, fmt.Errorf("failed to decrypt %s: %w", expanded, err)
}

func expandPath(path string) (string, error) {
	if strings.HasPrefix(path, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, strings.TrimPrefix(path, "~")), nil
	}
	return path, nil
}

func prepareTerminal(session *ssh.Session) func() {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return func() {}
	}

	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return func() {}
	}

	width, height, err := term.GetSize(fd)
	if err != nil {
		width, height = 80, 24
	}

	termName := os.Getenv("TERM")
	if termName == "" {
		termName = "xterm-256color"
	}

	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}

	if err := session.RequestPty(termName, height, width, modes); err != nil {
		term.Restore(fd, oldState)
		return func() {}
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGWINCH)

	go func() {
		for range sigCh {
			if w, h, err := term.GetSize(fd); err == nil {
				_ = session.WindowChange(h, w)
			}
		}
	}()

	return func() {
		signal.Stop(sigCh)
		close(sigCh)
		term.Restore(fd, oldState)
	}
}

func promptPassphrase(identityPath string) ([]byte, error) {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return nil, fmt.Errorf("passphrase required for %s but stdin is not a terminal; run ssh-add or specify --identity", identityPath)
	}

	fmt.Fprintf(os.Stderr, "Enter passphrase for %s: ", identityPath)
	pass, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return nil, err
	}
	return pass, nil
}

func zeroBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
