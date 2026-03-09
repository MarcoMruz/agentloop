package security

import (
	"regexp"
	"strings"

	"github.com/MarcoMruz/agentloop/internal/config"
)

// SecurityTier represents the security classification of an operation
type SecurityTier int

const (
	TierAllow SecurityTier = iota // Safe operations that run without approval
	TierLog                       // Moderate risk operations that run but are logged
	TierHITL                      // High risk operations requiring human approval
	TierBlock                     // Dangerous operations that are always blocked
)

// String returns the string representation of a SecurityTier
func (t SecurityTier) String() string {
	switch t {
	case TierAllow:
		return "allow"
	case TierLog:
		return "log"
	case TierHITL:
		return "hitl"
	case TierBlock:
		return "block"
	default:
		return "unknown"
	}
}

// ClassifyOperation determines the security tier for a given operation
func ClassifyOperation(toolName string, command string, filePath string, cfg *config.SecurityConfig) SecurityTier {
	// If policy mode is not selective, fall back to strict behavior
	if cfg.PolicyMode != "selective" {
		return TierHITL // Default to requiring approval for everything
	}

	switch toolName {
	case "bash":
		return classifyBashCommand(command, cfg)
	case "write", "edit":
		return classifyFileOperation(toolName, filePath, cfg)
	case "read":
		return classifyReadOperation(filePath, cfg)
	default:
		// Unknown tools default to logging
		return TierLog
	}
}

// classifyBashCommand determines the tier for bash commands
func classifyBashCommand(command string, cfg *config.SecurityConfig) SecurityTier {
	command = strings.TrimSpace(command)

	// Check always blocked patterns first (highest priority)
	for _, pattern := range cfg.Tiers.AlwaysBlocked.BashPatterns {
		if matched, _ := regexp.MatchString(pattern, command); matched {
			return TierBlock
		}
	}

	// Check HITL required patterns
	for _, pattern := range cfg.Tiers.HITLRequired.BashPatterns {
		if matched, _ := regexp.MatchString(pattern, command); matched {
			return TierHITL
		}
	}

	// Check logged operations patterns
	for _, pattern := range cfg.Tiers.LoggedOperations.BashPatterns {
		if matched, _ := regexp.MatchString(pattern, command); matched {
			return TierLog
		}
	}

	// Check safe operations patterns
	for _, pattern := range cfg.Tiers.SafeOperations.BashPatterns {
		if matched, _ := regexp.MatchString(pattern, command); matched {
			return TierAllow
		}
	}

	// Handle docker commands separately
	if strings.Contains(command, "docker") {
		return classifyDockerCommand(command, cfg)
	}

	// Unknown bash commands default to HITL for safety
	return TierHITL
}

// classifyDockerCommand determines the tier for docker commands
func classifyDockerCommand(command string, cfg *config.SecurityConfig) SecurityTier {
	// Extract docker subcommand
	words := strings.Fields(command)
	dockerIdx := -1
	for i, word := range words {
		if word == "docker" || word == "docker-compose" {
			dockerIdx = i
			break
		}
	}
	
	if dockerIdx == -1 || dockerIdx+1 >= len(words) {
		return TierHITL
	}
	
	subcmd := words[dockerIdx+1]

	// Check for dangerous volume mounts (always blocked)
	if strings.Contains(command, "-v") || strings.Contains(command, "--volume") {
		for i, word := range words {
			if (word == "-v" || word == "--volume") && i+1 < len(words) {
				hostPath := strings.SplitN(words[i+1], ":", 2)[0]
				for _, blockedPath := range cfg.Tiers.AlwaysBlocked.VolumeMounts {
					if strings.HasPrefix(hostPath, blockedPath) {
						return TierBlock
					}
				}
			}
		}
	}

	// Check HITL required docker commands
	for _, cmd := range cfg.Tiers.HITLRequired.DockerCommands {
		if subcmd == cmd {
			return TierHITL
		}
	}

	// Check logged docker commands
	for _, cmd := range cfg.Tiers.LoggedOperations.DockerCommands {
		if subcmd == cmd {
			return TierLog
		}
	}

	// Check safe docker commands
	for _, cmd := range cfg.Tiers.SafeOperations.DockerCommands {
		if subcmd == cmd {
			return TierAllow
		}
	}

	// Unknown docker commands default to HITL
	return TierHITL
}

// classifyFileOperation determines the tier for file write/edit operations
func classifyFileOperation(toolName string, filePath string, cfg *config.SecurityConfig) SecurityTier {
	// Check if path validation would fail (this would be blocked)
	if err := ValidatePath(filePath, cfg.AllowedPaths); err != nil {
		return TierHITL // Require approval for paths outside allowed dirs
	}

	// Check if it's in a sensitive location
	sensitivePatterns := []string{
		"/etc/", "/var/", "/root/", "/proc/", "/sys/", "/dev/",
		"/.git/", "/node_modules/", ".env", "credentials", "config",
	}

	for _, pattern := range sensitivePatterns {
		if strings.Contains(strings.ToLower(filePath), pattern) {
			return TierHITL
		}
	}

	// File operations in allowed paths are logged but allowed
	return TierLog
}

// classifyReadOperation determines the tier for read operations
func classifyReadOperation(filePath string, cfg *config.SecurityConfig) SecurityTier {
	// Reading is generally safe, but we might want to log access to sensitive files
	sensitivePatterns := []string{
		".env", "credentials", "config", "password", "secret", "key",
		"/etc/passwd", "/etc/shadow", "id_rsa", "private",
	}

	for _, pattern := range sensitivePatterns {
		if strings.Contains(strings.ToLower(filePath), pattern) {
			return TierLog // Log but allow reading sensitive files
		}
	}

	// Regular file reads are safe
	return TierAllow
}

// ShouldRequireApproval returns true if the operation requires user approval
func ShouldRequireApproval(tier SecurityTier) bool {
	return tier == TierHITL
}

// ShouldBlock returns true if the operation should be blocked entirely
func ShouldBlock(tier SecurityTier) bool {
	return tier == TierBlock
}

// ShouldLog returns true if the operation should be logged
func ShouldLog(tier SecurityTier) bool {
	return tier == TierLog || tier == TierHITL
}