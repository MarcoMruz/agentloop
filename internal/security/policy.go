package security

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// ValidatePath checks if a path is within allowed paths. Uses filepath.Clean.
func ValidatePath(path string, allowedPaths []string) error {
	if len(allowedPaths) == 0 {
		return nil
	}
	clean := filepath.Clean(expandHome(path))
	for _, ap := range allowedPaths {
		allowed := filepath.Clean(expandHome(ap))
		if strings.HasPrefix(clean, allowed+string(filepath.Separator)) || clean == allowed {
			return nil
		}
	}
	return fmt.Errorf("path %q outside allowed paths", path)
}

// ValidateURL checks for SSRF against blocked CIDRs.
func ValidateURL(rawURL string, blockedCIDRs []string, allowedDomains []string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("only http/https allowed")
	}
	if len(allowedDomains) > 0 {
		host := parsed.Hostname()
		ok := false
		for _, d := range allowedDomains {
			if host == d || strings.HasSuffix(host, "."+d) {
				ok = true
				break
			}
		}
		if !ok {
			return fmt.Errorf("domain %q not allowed", parsed.Hostname())
		}
	}
	host := parsed.Hostname()
	ips, err := net.LookupHost(host)
	if err != nil {
		return fmt.Errorf("DNS resolution failed for %q: %w", host, err)
	}
	for _, ipStr := range ips {
		ip := net.ParseIP(ipStr)
		if ip == nil {
			continue
		}
		for _, cidr := range blockedCIDRs {
			_, network, err := net.ParseCIDR(cidr)
			if err != nil {
				continue
			}
			if network.Contains(ip) {
				return fmt.Errorf("URL resolves to blocked IP range %s (SSRF)", cidr)
			}
		}
	}
	return nil
}

// ValidateDockerCommand checks subcommands and volume mounts.
func ValidateDockerCommand(cmd string, allowedSubs []string, blockedVolPaths []string) error {
	fields := strings.Fields(cmd)
	subcmd := ""
	for i, f := range fields {
		if f == "docker" || f == "docker-compose" {
			if i+1 < len(fields) && !strings.HasPrefix(fields[i+1], "-") {
				subcmd = fields[i+1]
				break
			}
		}
	}
	if len(allowedSubs) > 0 {
		ok := false
		for _, a := range allowedSubs {
			if subcmd == a {
				ok = true
				break
			}
		}
		if !ok {
			return fmt.Errorf("docker subcommand %q not allowed", subcmd)
		}
	}
	for i, f := range fields {
		if (f == "-v" || f == "--volume") && i+1 < len(fields) {
			hostPath := strings.SplitN(fields[i+1], ":", 2)[0]
			for _, bp := range blockedVolPaths {
				if strings.HasPrefix(hostPath, bp) {
					return fmt.Errorf("volume mount to %q blocked", hostPath)
				}
			}
		}
	}
	return nil
}

func expandHome(p string) string {
	if strings.HasPrefix(p, "~/") {
		if h, err := os.UserHomeDir(); err == nil {
			return filepath.Join(h, p[2:])
		}
	}
	return p
}
