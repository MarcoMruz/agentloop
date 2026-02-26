package config

type Config struct {
	Vault    VaultConfig    `mapstructure:"vault"`
	Pi       PiConfig       `mapstructure:"pi"`
	Agents   AgentConfig    `mapstructure:"agents"`
	HITL     HITLConfig     `mapstructure:"hitl"`
	Security SecurityConfig `mapstructure:"security"`
	Tools    ToolsConfig    `mapstructure:"tools"`
	Logging  LoggingConfig  `mapstructure:"logging"`
}

// PiConfig controls how the pi subprocess is launched.
type PiConfig struct {
	// Binary is the path to the pi executable. Default: "pi" (from PATH).
	Binary string `mapstructure:"binary"`
	// Provider passed as --provider to pi. E.g. "anthropic", "openai", "ollama".
	Provider string `mapstructure:"provider"`
	// Model passed as --model to pi. E.g. "claude-sonnet-4-20250514", "gpt-4o".
	Model string `mapstructure:"model"`
	// ExtraArgs are additional CLI flags passed to pi.
	ExtraArgs []string `mapstructure:"extra_args"`
	// ExtensionsDir is the path to the AgentLoop extensions directory.
	ExtensionsDir string `mapstructure:"extensions_dir"`
}

type VaultConfig struct {
	Path     string `mapstructure:"path"`
	AutoOpen bool   `mapstructure:"auto_open"`
}

type AgentConfig struct {
	MaxIterations  int `mapstructure:"max_iterations"`
	StuckThreshold int `mapstructure:"stuck_threshold"`
	MaxTokenBudget int `mapstructure:"max_token_budget"`
	MaxToolCalls   int `mapstructure:"max_tool_calls"`
}

type HITLConfig struct {
	ConfidenceThreshold float64  `mapstructure:"confidence_threshold"`
	AlwaysPauseTools    []string `mapstructure:"always_pause_tools"`
	TimeoutSeconds      int      `mapstructure:"timeout_seconds"`
	TimeoutAction       string   `mapstructure:"timeout_action"`
}

type SecurityConfig struct {
	// AllowedPaths: filesystem paths pi is allowed to access.
	AllowedPaths []string `mapstructure:"allowed_paths"`
	// BlockedEnvPrefixes: env var prefixes stripped from pi's environment.
	BlockedEnvPrefixes []string `mapstructure:"blocked_env_prefixes"`
	// BlockedCIDRs: IP ranges blocked for external API calls (SSRF protection).
	BlockedCIDRs []string `mapstructure:"blocked_cidrs"`
	// DockerAllowedSubcommands: allowed docker subcommands.
	DockerAllowedSubcommands []string `mapstructure:"docker_allowed_subcommands"`
	// DockerBlockedVolumePaths: volume mount prefixes blocked for docker.
	DockerBlockedVolumePaths []string `mapstructure:"docker_blocked_volume_paths"`
}

type ToolsConfig struct {
	WebSearch WebSearchConfig `mapstructure:"websearch"`
	N8N       N8NConfig       `mapstructure:"n8n"`
}

type WebSearchConfig struct {
	Provider   string `mapstructure:"provider"`
	MaxResults int    `mapstructure:"max_results"`
}

type N8NConfig struct {
	Webhooks map[string]N8NWebhook `mapstructure:"webhooks"`
}

type N8NWebhook struct {
	URL          string `mapstructure:"url"`
	AuthHeader   string `mapstructure:"auth_header"`
	SecretEnvVar string `mapstructure:"secret_env_var"`
}

type LoggingConfig struct {
	Level string `mapstructure:"level"`
	File  string `mapstructure:"file"`
}

func Defaults() *Config {
	return &Config{
		Vault: VaultConfig{Path: "~/.local/share/agentloop/vault"},
		Pi: PiConfig{
			Binary:        "pi",
			Provider:      "anthropic",
			Model:         "claude-sonnet-4-20250514",
			ExtensionsDir: "", // auto-detected relative to agentloop binary
		},
		Agents: AgentConfig{
			MaxIterations: 25, StuckThreshold: 3,
			MaxTokenBudget: 200000, MaxToolCalls: 100,
		},
		HITL: HITLConfig{
			ConfidenceThreshold: 0.75,
			AlwaysPauseTools:    []string{"docker", "n8n_webhook"},
			TimeoutSeconds:      300, TimeoutAction: "pause",
		},
		Security: SecurityConfig{
			AllowedPaths:             []string{"~/projects", "~/tmp"},
			BlockedEnvPrefixes:       []string{"ANTHROPIC_", "OPENAI_", "BRAVE_SEARCH_", "N8N_WEBHOOK_", "AWS_SECRET", "GITHUB_TOKEN", "GH_TOKEN", "SECRET_KEY", "PRIVATE_KEY"},
			BlockedCIDRs:             []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "127.0.0.0/8", "169.254.0.0/16", "::1/128", "fc00::/7", "fe80::/10"},
			DockerAllowedSubcommands: []string{"ps", "logs", "images", "build", "compose", "inspect", "stats", "top", "exec", "run", "stop", "start", "restart", "rm"},
			DockerBlockedVolumePaths: []string{"/etc", "/var", "/root", "/proc", "/sys", "/dev"},
		},
		Tools: ToolsConfig{
			WebSearch: WebSearchConfig{Provider: "brave", MaxResults: 10},
		},
		Logging: LoggingConfig{Level: "info"},
	}
}
