package security

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// InjectionConfig holds prompt injection security configuration
type InjectionConfig struct {
	EnableProtection    bool     `mapstructure:"enable_protection"`
	WhitelistSources    []string `mapstructure:"whitelist_sources"`
	BlockedKeywords     []string `mapstructure:"blocked_keywords"`
	SanitizeMemory      bool     `mapstructure:"sanitize_memory"`
	RequireApproval     []string `mapstructure:"require_approval"`
	ApprovalTier        string   `mapstructure:"approval_tier"` // "owner", "admin", "auto-deny"
	SensitivePatterns   []string `mapstructure:"sensitive_patterns"`
	MaxContentLength    int      `mapstructure:"max_content_length"`
	DetectionThreshold  float64  `mapstructure:"detection_threshold"` // 0.0-1.0
}

// InjectionSource represents potential injection source types
type InjectionSource int

const (
	SourceUnknown InjectionSource = iota
	SourceSkill
	SourceNodeModules  
	SourceEmailAttachment
	SourceCloudFile
	SourceFetchResponse
	SourceGitRepo
	SourceUserInput
)

// InjectionRisk represents risk levels for content
type InjectionRisk int

const (
	RiskLow InjectionRisk = iota
	RiskMedium
	RiskHigh
	RiskCritical
)

// InjectionContext holds context about potential injection attempt
type InjectionContext struct {
	Source       InjectionSource
	FilePath     string
	Command      string
	Content      string
	Risk         InjectionRisk
	Triggers     []string
	NeedsApproval bool
}

// DefaultInjectionConfig returns secure defaults for injection protection
func DefaultInjectionConfig() InjectionConfig {
	return InjectionConfig{
		EnableProtection: true,
		WhitelistSources: []string{
			"~/projects",
			"~/agentloop-sandbox", 
			"~/.config/agentloop",
		},
		BlockedKeywords: []string{
			"ignore previous instructions",
			"forget everything above",  
			"act as if you are",
			"pretend you are",
			"roleplay as",
			"API_KEY",
			"SECRET",
			"TOKEN",
			"PASSWORD", 
			"PRIVATE_KEY",
			"credentials",
			"auth",
			"login",
		},
		SanitizeMemory: true,
		RequireApproval: []string{
			"skills/*",
			"node_modules/*", 
			"*/attachments/*",
			"cloud:*",
			"fetch:*",
			".git/*",
		},
		ApprovalTier: "owner",
		SensitivePatterns: []string{
			`\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Z|a-z]{2,}\b`, // emails
			`\b(?:[0-9]{1,3}\.){3}[0-9]{1,3}\b`,                    // IP addresses
			`sk-[a-zA-Z0-9]{48}`,                                   // OpenAI keys (must be before hex pattern!)
			`xoxb-[0-9]+-[0-9]+-[0-9]+-[a-zA-Z0-9]+`,              // Slack tokens
			`\b[A-Fa-f0-9]{32,}\b`,                                 // hex keys (after specific patterns)
		},
		MaxContentLength:   50000, // 50KB max
		DetectionThreshold: 0.7,   // 70% confidence threshold
	}
}

// DetectInjectionRisk analyzes content for prompt injection risks
func DetectInjectionRisk(content string, source InjectionSource, config InjectionConfig) (*InjectionContext, error) {
	if !config.EnableProtection {
		return &InjectionContext{Source: source, Risk: RiskLow}, nil
	}

	ctx := &InjectionContext{
		Source:   source,
		Content:  content,
		Risk:     RiskLow,
		Triggers: []string{},
	}

	// Check content length
	if len(content) > config.MaxContentLength {
		ctx.Risk = RiskHigh
		ctx.Triggers = append(ctx.Triggers, "content_too_large")
	}

	// Check for blocked keywords
	lowerContent := strings.ToLower(content)
	for _, keyword := range config.BlockedKeywords {
		if strings.Contains(lowerContent, strings.ToLower(keyword)) {
			ctx.Risk = max(ctx.Risk, RiskMedium)
			ctx.Triggers = append(ctx.Triggers, fmt.Sprintf("blocked_keyword:%s", keyword))
		}
	}

	// Check for sensitive patterns
	for i, pattern := range config.SensitivePatterns {
		if matched, _ := regexp.MatchString(pattern, content); matched {
			ctx.Risk = max(ctx.Risk, RiskHigh)
			ctx.Triggers = append(ctx.Triggers, fmt.Sprintf("sensitive_pattern:%d", i))
		}
	}

	// Source-specific risk assessment
	switch source {
	case SourceSkill:
		ctx.Risk = max(ctx.Risk, RiskMedium) // Skills can execute code
	case SourceNodeModules:
		ctx.Risk = max(ctx.Risk, RiskHigh) // Node modules are external
	case SourceEmailAttachment, SourceCloudFile:
		ctx.Risk = max(ctx.Risk, RiskHigh) // External sources
	case SourceFetchResponse:
		ctx.Risk = max(ctx.Risk, RiskMedium) // Network content
	case SourceGitRepo:
		ctx.Risk = max(ctx.Risk, RiskMedium) // Could be malicious repo
	}

	// Determine if approval is needed
	ctx.NeedsApproval = ctx.Risk >= RiskMedium || isRequiredApproval(ctx.FilePath, config.RequireApproval)

	return ctx, nil
}

// ValidateToolCallSource checks if a tool call is from a trusted source
func ValidateToolCallSource(toolName, filePath, command string, config InjectionConfig) error {
	if !config.EnableProtection {
		return nil
	}

	source := detectSourceFromPath(filePath)
	
	// Check if source requires whitelist approval
	if len(config.WhitelistSources) > 0 && !isWhitelistedSource(filePath, config.WhitelistSources) {
		return fmt.Errorf("tool call from non-whitelisted source: %s", filePath)
	}

	// Validate specific tool patterns
	return validateToolPattern(toolName, command, source, config)
}

// SanitizeContent removes or masks sensitive content
func SanitizeContent(content string, config InjectionConfig) string {
	if !config.SanitizeMemory {
		return content
	}

	sanitized := content
	
	// Mask sensitive patterns
	for _, pattern := range config.SensitivePatterns {
		re := regexp.MustCompile(pattern)
		sanitized = re.ReplaceAllString(sanitized, "[REDACTED]")
	}

	// Remove blocked keywords context
	for _, keyword := range config.BlockedKeywords {
		// Remove sentences containing blocked keywords
		lines := strings.Split(sanitized, "\n")
		var cleanLines []string
		for _, line := range lines {
			if !strings.Contains(strings.ToLower(line), strings.ToLower(keyword)) {
				cleanLines = append(cleanLines, line)
			}
		}
		sanitized = strings.Join(cleanLines, "\n")
	}

	return sanitized
}

// Helper functions

func detectSourceFromPath(path string) InjectionSource {
	if path == "" {
		return SourceUnknown
	}
	
	cleanPath := filepath.Clean(path)
	
	switch {
	case strings.Contains(cleanPath, "node_modules"):
		return SourceNodeModules
	case strings.Contains(cleanPath, "/skills/"):
		return SourceSkill
	case strings.Contains(cleanPath, "/attachments/"):
		return SourceEmailAttachment
	case strings.Contains(cleanPath, "/.git/"):
		return SourceGitRepo
	case strings.HasPrefix(path, "cloud:"):
		return SourceCloudFile
	case strings.HasPrefix(path, "fetch:"):
		return SourceFetchResponse
	default:
		return SourceUserInput
	}
}

func isWhitelistedSource(path string, whitelist []string) bool {
	if len(whitelist) == 0 {
		return true // No whitelist = everything allowed
	}
	
	cleanPath := filepath.Clean(expandHome(path))
	for _, allowed := range whitelist {
		allowedPath := filepath.Clean(expandHome(allowed))
		if strings.HasPrefix(cleanPath, allowedPath) {
			return true
		}
	}
	return false
}

func isRequiredApproval(path string, patterns []string) bool {
	for _, pattern := range patterns {
		if matched, _ := filepath.Match(pattern, path); matched {
			return true
		}
		if strings.Contains(path, strings.ReplaceAll(pattern, "*", "")) {
			return true
		}
	}
	return false
}

func validateToolPattern(toolName, command string, source InjectionSource, config InjectionConfig) error {
	// High-risk tool calls from external sources (exclude user input and unknown)
	dangerousTools := []string{"bash", "write", "edit", "git"}
	externalSources := []InjectionSource{
		SourceNodeModules, SourceEmailAttachment, SourceCloudFile, 
		SourceFetchResponse, SourceGitRepo,
	}
	
	for _, externalSource := range externalSources {
		if source == externalSource {
			for _, tool := range dangerousTools {
				if toolName == tool {
					return fmt.Errorf("dangerous tool '%s' from external source requires approval", toolName)
				}
			}
			break
		}
	}

	// Command-specific validations
	if toolName == "bash" && command != "" {
		// Check for fetch/curl from skills
		if (strings.Contains(command, "curl") || strings.Contains(command, "wget") || strings.Contains(command, "fetch")) && 
		   source == SourceSkill {
			return fmt.Errorf("network requests from skills require approval")
		}
		
		// Check for node_modules access
		if strings.Contains(command, "node_modules") {
			return fmt.Errorf("node_modules access requires approval")
		}
	}

	return nil
}

func max(a, b InjectionRisk) InjectionRisk {
	if a > b {
		return a
	}
	return b
}

