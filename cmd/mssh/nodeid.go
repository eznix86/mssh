package main

import (
	"net"
	"os"
	"regexp"
	"strings"
)

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
