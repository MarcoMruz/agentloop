package config

type Config struct {
	Server   ServerConfig   `mapstructure:"server"`
	Pi       PiConfig       `mapstructure:"pi"`
	Vault    VaultConfig    `mapstructure:"vault"`
	Memory   MemoryConfig   `mapstructure:"memory"`
	Sessions SessionConfig  `mapstructure:"sessions"`
	HITL     HITLConfig     `mapstructure:"hitl"`
	Security SecurityConfig `mapstructure:"security"`
	Skills   SkillsConfig   `mapstructure:"skills"`
	Logging  LoggingConfig  `mapstructure:"logging"`
}

type ServerConfig struct {
	SocketPath string `mapstructure:"socket_path"`
}

type PiConfig struct {
	Binary        string   `mapstructure:"binary"`
	Provider      string   `mapstructure:"provider"`
	Model         string   `mapstructure:"model"`
	ExtensionsDir string   `mapstructure:"extensions_dir"`
	ExtraArgs     []string `mapstructure:"extra_args"`
}

type VaultConfig struct {
	Path string `mapstructure:"path"`
}

type MemoryConfig struct {
	MaxProfileEntries      int    `mapstructure:"max_profile_entries"`
	ConversationRetainDays int    `mapstructure:"conversation_retain_days"`
	CompactionThreshold    int    `mapstructure:"compaction_threshold"`
	CompactionStrategy     string `mapstructure:"compaction_strategy"`
	MaxContextTokens       int    `mapstructure:"max_context_tokens"`
	PromptCacheTTLMinutes  int    `mapstructure:"prompt_cache_ttl_minutes"`
}

type SessionConfig struct {
	MaxConcurrent  int `mapstructure:"max_concurrent"`
	MaxPerUser     int `mapstructure:"max_per_user"`
	TimeoutMinutes int `mapstructure:"timeout_minutes"`
	MaxTokenBudget int `mapstructure:"max_token_budget"`
	MaxToolCalls   int `mapstructure:"max_tool_calls"`
	StuckThreshold int `mapstructure:"stuck_threshold"`
}

type HITLConfig struct {
	AlwaysPauseTools []string `mapstructure:"always_pause_tools"`
	TimeoutSeconds   int      `mapstructure:"timeout_seconds"`
	TimeoutAction    string   `mapstructure:"timeout_action"`
}

type SecurityConfig struct {
	AllowedPaths             []string `mapstructure:"allowed_paths"`
	BlockedEnvPrefixes       []string `mapstructure:"blocked_env_prefixes"`
	BlockedCIDRs             []string `mapstructure:"blocked_cidrs"`
	DockerAllowedSubcommands []string `mapstructure:"docker_allowed_subcommands"`
	DockerBlockedVolumePaths []string `mapstructure:"docker_blocked_volume_paths"`
}

type SkillsConfig struct {
	SkillDirs []string `mapstructure:"skill_dirs"`
}

type LoggingConfig struct {
	Level string `mapstructure:"level"`
	File  string `mapstructure:"file"`
}

func Defaults() *Config {
	return &Config{
		Server: ServerConfig{SocketPath: "~/.local/share/agentloop/agentloop.sock"},
		Pi: PiConfig{
			Binary:   "pi",
			Provider: "anthropic",
			Model:    "claude-sonnet-4-20250514",
		},
		Vault: VaultConfig{Path: "~/.local/share/agentloop/vault"},
		Memory: MemoryConfig{
			MaxProfileEntries:      50,
			ConversationRetainDays: 30,
			CompactionThreshold:    40,
			CompactionStrategy:     "rolling",
			MaxContextTokens:       3000,
			PromptCacheTTLMinutes:  60,
		},
		Sessions: SessionConfig{
			MaxConcurrent:  3,
			MaxPerUser:     1,
			TimeoutMinutes: 30,
			MaxTokenBudget: 200000,
			MaxToolCalls:   100,
			StuckThreshold: 3,
		},
		HITL: HITLConfig{
			AlwaysPauseTools: []string{"docker", "git push", "rm -r", "curl", "wget"},
			TimeoutSeconds:   300,
			TimeoutAction:    "deny",
		},
		Security: SecurityConfig{
			AllowedPaths:             []string{"~/projects", "~/tmp", "~/agentloop-sandbox"},
			BlockedEnvPrefixes:       []string{"ANTHROPIC_", "OPENAI_", "BRAVE_SEARCH_", "N8N_WEBHOOK_", "AWS_SECRET", "GITHUB_TOKEN", "SECRET_KEY", "PRIVATE_KEY"},
			BlockedCIDRs:             []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "127.0.0.0/8", "169.254.0.0/16", "::1/128", "fc00::/7", "fe80::/10"},
			DockerAllowedSubcommands: []string{"ps", "logs", "images", "build", "compose", "inspect", "stats", "top", "exec", "run", "stop", "start", "restart", "rm"},
			DockerBlockedVolumePaths: []string{"/etc", "/var", "/root", "/proc", "/sys", "/dev"},
		},
		Skills:  SkillsConfig{SkillDirs: []string{"~/.local/share/agentloop/vault/skills"}},
		Logging: LoggingConfig{Level: "info"},
	}
}
