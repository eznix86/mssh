package protocol

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"
)

const (
	Version           = "MSSH/2"
	MaxLine           = 4096
	SetupTimeout      = 10 * time.Second
	HeartbeatInterval = 10 * time.Second
	HeartbeatTimeout  = 30 * time.Second
)

var nodePattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)

func ValidNode(node string) bool { return nodePattern.MatchString(node) }

func ReadLine(reader *bufio.Reader) (string, error) {
	line, err := reader.ReadSlice('\n')
	if errors.Is(err, bufio.ErrBufferFull) || len(line) > MaxLine {
		return "", fmt.Errorf("protocol message exceeds %d bytes", MaxLine)
	}
	if err != nil {
		return "", fmt.Errorf("read protocol message: %w", err)
	}
	return strings.TrimSuffix(strings.TrimSuffix(string(line), "\n"), "\r"), nil
}

func WriteLine(writer io.Writer, line string) error {
	if len(line)+1 > MaxLine || strings.ContainsAny(line, "\r\n") {
		return fmt.Errorf("invalid protocol message")
	}
	_, err := io.WriteString(writer, line+"\n")
	if err != nil {
		return fmt.Errorf("write protocol message: %w", err)
	}
	return nil
}

func Header(kind, node, token string) (string, error) {
	if (kind != "AGENT" && kind != "CLIENT") || !ValidNode(node) {
		return "", fmt.Errorf("invalid connection type or node-id")
	}
	if token == "" {
		token = "-"
	}
	if strings.ContainsAny(token, " \t\r\n") {
		return "", fmt.Errorf("invalid token")
	}
	return fmt.Sprintf("%s %s %s %s", Version, kind, node, token), nil
}

func ParseHeader(line string) (kind, node, token string, err error) {
	parts := strings.Fields(line)
	if len(parts) != 4 || parts[0] != Version {
		return "", "", "", fmt.Errorf("expected %s header; update server, agent and client together", Version)
	}
	if !ValidNode(parts[2]) || (parts[1] != "AGENT" && parts[1] != "CLIENT") {
		return "", "", "", fmt.Errorf("invalid connection type or node-id")
	}
	token = parts[3]
	if token == "-" {
		token = ""
	}
	return parts[1], parts[2], token, nil
}

func Expect(reader *bufio.Reader, want string) error {
	line, err := ReadLine(reader)
	if err != nil {
		return err
	}
	if strings.HasPrefix(line, "ERROR: ") {
		return errors.New(line)
	}
	if line != want {
		return fmt.Errorf("unexpected protocol response %q; expected %s", line, want)
	}
	return nil
}
