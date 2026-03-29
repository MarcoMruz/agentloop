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
	Logging   LoggingConfig   `mapstructure:"logging"`
	Evolution    EvolutionConfig    `mapstructure:"evolution"`
	Orchestrator OrchestratorConfig `mapstructure:"orchestrator"`
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
	// NoSkills passes --no-skills to pi, disabling auto-discovery from ~/.pi/agent/skills/.
	// AgentLoop exposes vault skills via resources_discover instead (vault-skills.ts).
	NoSkills bool `mapstructure:"no_skills"`
	// VaultPath is injected at runtime (not from YAML) so extensions can locate vault skills.
	VaultPath string `mapstructure:"-"`
}

type VaultConfig struct {
	Path string `mapstructure:"path"`
}

type MemoryAgentConfig struct {
	Enabled  bool   `mapstructure:"enabled"`
	Binary   string `mapstructure:"binary"`
	Provider string `mapstructure:"provider"`
	Model    string `mapstructure:"model"`
}

type MemoryConfig struct {
	MaxProfileEntries      int                `mapstructure:"max_profile_entries"`
	ConversationRetainDays int                `mapstructure:"conversation_retain_days"`
	CompactionThreshold    int                `mapstructure:"compaction_threshold"`
	CompactionStrategy     string             `mapstructure:"compaction_strategy"`
	MaxContextTokens       int                `mapstructure:"max_context_tokens"`
	PromptCacheTTLMinutes  int                `mapstructure:"prompt_cache_ttl_minutes"`
	EmbeddingDims          int                `mapstructure:"embedding_dims"`
	Agent                  MemoryAgentConfig  `mapstructure:"agent"`
}

type SessionConfig struct {
	MaxConcurrent  int  `mapstructure:"max_concurrent"`
	MaxPerUser     int  `mapstructure:"max_per_user"`
	TimeoutMinutes int  `mapstructure:"timeout_minutes"`
	MaxTokenBudget int  `mapstructure:"max_token_budget"`
	MaxToolCalls   int  `mapstructure:"max_tool_calls"`
	StuckThreshold int  `mapstructure:"stuck_threshold"`
	EvictLRU       bool `mapstructure:"evict_lru"`
}

type HITLConfig struct {
	AlwaysPauseTools   []string `mapstructure:"always_pause_tools"`
	TimeoutSeconds     int      `mapstructure:"timeout_seconds"`
	TimeoutAction      string   `mapstructure:"timeout_action"`
	ForceHITLKeywords  []string `mapstructure:"force_hitl_keywords"`
}

// ReadonlySessionConfig controls HITL auto-approval in internal read-only pi
// sessions (planner, judge, skill agent, meta agent). If the bash command in a
// HITL request matches any deny_pattern it is auto-denied; otherwise approved.
type ReadonlySessionConfig struct {
	DenyPatterns []string `mapstructure:"deny_patterns"`
}

type SecurityConfig struct {
	AllowedPaths             []string             `mapstructure:"allowed_paths"`
	BlockedEnvPrefixes       []string             `mapstructure:"blocked_env_prefixes"`
	BlockedCIDRs             []string             `mapstructure:"blocked_cidrs"`
	DockerAllowedSubcommands []string             `mapstructure:"docker_allowed_subcommands"`
	DockerBlockedVolumePaths []string             `mapstructure:"docker_blocked_volume_paths"`
	Injection                InjectionConfig      `mapstructure:"injection"`
	PolicyMode               string               `mapstructure:"policy_mode"`
	Tiers                    SecurityTiers        `mapstructure:"tiers"`
	AutoApproveNonHigh       bool                 `mapstructure:"auto_approve_non_high"`
	ReadonlySessions         ReadonlySessionConfig `mapstructure:"readonly_sessions"`
}

type SecurityTiers struct {
	SafeOperations   TierOperations `mapstructure:"safe_operations"`
	LoggedOperations TierOperations `mapstructure:"logged_operations"`
	HITLRequired     TierOperations `mapstructure:"hitl_required"`
	AlwaysBlocked    TierOperations `mapstructure:"always_blocked"`
}

type TierOperations struct {
	BashPatterns    []string `mapstructure:"bash_patterns"`
	Tools           []string `mapstructure:"tools"`
	DockerCommands  []string `mapstructure:"docker_commands"`
	VolumeMounts    []string `mapstructure:"volume_mounts"`
}

type InjectionConfig struct {
	EnableProtection   bool     `mapstructure:"enable_protection"`
	WhitelistSources   []string `mapstructure:"whitelist_sources"`
	BlockedKeywords    []string `mapstructure:"blocked_keywords"`
	SanitizeMemory     bool     `mapstructure:"sanitize_memory"`
	RequireApproval    []string `mapstructure:"require_approval"`
	ApprovalTier       string   `mapstructure:"approval_tier"`
	SensitivePatterns  []string `mapstructure:"sensitive_patterns"`
	MaxContentLength   int      `mapstructure:"max_content_length"`
	DetectionThreshold float64  `mapstructure:"detection_threshold"`
}

type SkillAgentConfig struct {
	Binary   string `mapstructure:"binary"`
	Provider string `mapstructure:"provider"`
	Model    string `mapstructure:"model"`
}

type SkillsConfig struct {
	SkillDirs []string        `mapstructure:"skill_dirs"`
	Agent     SkillAgentConfig `mapstructure:"agent"`
}

type LoggingConfig struct {
	Level string `mapstructure:"level"`
	File  string `mapstructure:"file"`
}

type EvolutionConfig struct {
	Enabled            bool    `mapstructure:"enabled"`
	ScoreThreshold     float64 `mapstructure:"score_threshold"`
	MetaTokenBudget    int     `mapstructure:"meta_token_budget"`
	MaxOutcomesPerRun  int     `mapstructure:"max_outcomes_per_run"`
	SnapshotRetainMax  int     `mapstructure:"snapshot_retain_max"`
	PipelineConfigPath string  `mapstructure:"pipeline_config_path"`
	MinCooldownSeconds int     `mapstructure:"min_cooldown_seconds"`
	MaxDailyRuns       int     `mapstructure:"max_daily_runs"`
}

type OrchestratorConfig struct {
	Planner          PiConfig `mapstructure:"planner"`
	Worker           PiConfig `mapstructure:"worker"`
	Judge            PiConfig `mapstructure:"judge"`
	WorkerPoolSize   int      `mapstructure:"worker_pool_size"`
	MaxIterations    int      `mapstructure:"max_iterations"`
	SummaryMaxTokens int      `mapstructure:"summary_max_tokens"`
}

func Defaults() *Config {
	return &Config{
		Server: ServerConfig{SocketPath: "~/.local/share/agentloop/agentloop.sock"},
		Pi: PiConfig{
			Binary:    "pi",
			Provider:  "anthropic",
			Model:     "claude-sonnet-4-20250514",
			NoSkills:  true,
		},
		Vault: VaultConfig{Path: "~/.local/share/agentloop/vault"},
		Memory: MemoryConfig{
			MaxProfileEntries:      50,
			ConversationRetainDays: 30,
			CompactionThreshold:    40,
			CompactionStrategy:     "rolling",
			MaxContextTokens:       3000,
			PromptCacheTTLMinutes:  60,
			EmbeddingDims:          0,
			Agent: MemoryAgentConfig{
				Enabled:  false,
				Binary:   "pi",
				Provider: "anthropic",
				Model:    "claude-haiku-4-5-20251001",
			},
		},
		Sessions: SessionConfig{
			MaxConcurrent:  5,
			MaxPerUser:     5,
			TimeoutMinutes: 30,
			MaxTokenBudget: 200000,
			MaxToolCalls:   100,
			StuckThreshold: 3,
			EvictLRU:       true,
		},
		HITL: HITLConfig{
			AlwaysPauseTools:  []string{"docker", "git push", "rm -r", "curl", "wget"},
			TimeoutSeconds:    300,
			TimeoutAction:     "deny",
			ForceHITLKeywords: []string{"sudo", "chmod", "chown", "systemctl", "npm install", "pip install", "yarn add"},
		},
		Security: SecurityConfig{
			AutoApproveNonHigh:       true,
			AllowedPaths:             []string{"~/projects", "~/tmp", "~/agentloop-sandbox"},
			BlockedEnvPrefixes:       []string{"ANTHROPIC_", "OPENAI_", "BRAVE_SEARCH_", "N8N_WEBHOOK_", "AWS_SECRET", "GITHUB_TOKEN", "SECRET_KEY", "PRIVATE_KEY"},
			BlockedCIDRs:             []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "127.0.0.0/8", "169.254.0.0/16", "::1/128", "fc00::/7", "fe80::/10"},
			DockerAllowedSubcommands: []string{"ps", "logs", "images", "build", "compose", "inspect", "stats", "top", "exec", "run", "stop", "start", "restart", "rm"},
			DockerBlockedVolumePaths: []string{"/etc", "/var", "/root", "/proc", "/sys", "/dev"},
			PolicyMode:               "selective",
			Injection: InjectionConfig{
				EnableProtection: true,
				WhitelistSources: []string{"~/projects", "~/agentloop-sandbox", "~/.config/agentloop"},
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
					`\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Z|a-z]{2,}\b`,
					`\b(?:[0-9]{1,3}\.){3}[0-9]{1,3}\b`,
					`\b[A-Fa-f0-9]{32,}\b`,
					`sk-[a-zA-Z0-9]{48}`,
					`xoxb-[0-9]+-[0-9]+-[0-9]+-[a-zA-Z0-9]+`,
				},
				MaxContentLength:   50000,
				DetectionThreshold: 0.7,
			},
			Tiers: SecurityTiers{
				SafeOperations: TierOperations{
					BashPatterns: []string{
						"^ls\\b",
						"^cat\\b",
						"^grep\\b",
						"^find\\b",
						"^pwd$",
						"^echo\\b",
						"^which\\b",
						"^git log",
						"^git status",
						"^git diff",
						"^npm list",
						"^yarn list",
						"^head\\b",
						"^tail\\b",
						"^wc\\b",
						"^sort\\b",
						"^uniq\\b",
					},
					Tools:          []string{"read"},
					DockerCommands: []string{"ps", "logs", "images", "inspect", "stats", "top"},
				},
				LoggedOperations: TierOperations{
					BashPatterns: []string{
						"^git checkout",
						"^git branch",
						"^npm install",
						"^yarn install",
						"^mkdir\\b",
						"^touch\\b",
						"^cp\\b",
						"^mv\\b",
						"^go build",
						"^go test",
						"^cargo build",
						"^cargo test",
					},
					Tools:          []string{"write", "edit"},
					DockerCommands: []string{"build", "run", "exec", "start"},
				},
				HITLRequired: TierOperations{
					BashPatterns: []string{
						"\\bsudo\\b",
						"\\bchmod\\b",
						"\\bchown\\b",
						"\\brm\\b.*-r",
						"\\bsystemctl\\b",
						"\\bservice\\b",
						"\\busermod\\b",
						"\\buserdel\\b",
						"\\buseradd\\b",
						"curl.*-X.*POST",
						"wget.*--post",
					},
					DockerCommands: []string{"rm", "stop", "restart", "compose"},
				},
				AlwaysBlocked: TierOperations{
					BashPatterns: []string{
						"rm -rf /",
						"mkfs",
						"> /dev/sd",
						"dd if=",
						":(){ :|:& };:",
						"\\bformat\\b.*C:",
						"shutdown",
						"reboot",
						"halt",
					},
					VolumeMounts: []string{"/etc", "/var", "/root", "/proc", "/sys", "/dev"},
				},
			},
			ReadonlySessions: ReadonlySessionConfig{
				DenyPatterns: []string{
					// Shell output redirects
					" > ", " >> ",
					`\btee\b`,
					// Destructive file ops
					`\brm\b`, `\brmdir\b`, `\bmv\b`,
					`\bmkdir\b`, `\btouch\b`,
					// Permission / ownership changes
					`\bchmod\b`, `\bchown\b`, `\bchattr\b`,
					// In-place edits
					`\bsed\b.*-i`, `\bawk\b.*-i`,
					// Git write ops (commit, push, reset, rebase, merge, branch deletes)
					`\bgit\b.*(commit|push|reset|rebase|merge|stash pop|tag\b|branch -[dD])`,
					// curl: deny non-GET methods and body flags; plain GET is fine
					`curl\b.*(-X\s*(POST|PUT|DELETE|PATCH)|--data\b|-d\s|--upload-file|--data-raw|--data-binary)`,
					// wget: deny POST variants; plain GET is fine
					`wget\b.*(--post-data|--post-file|--method=(POST|PUT|DELETE|PATCH))`,
					// Package managers (write ops)
					`\bnpm\b.*(install|i\b|add|rm\b|uninstall|update|upgrade|publish)`,
					`\bpip[23]?\b.*(install|uninstall|download)`,
					`\byarn\b.*(add|remove|install|upgrade)`,
					`\bbrew\b.*(install|uninstall|upgrade|reinstall)`,
					`\bapt(-get)?\b.*(install|remove|purge|upgrade)`,
					`\byum\b.*(install|remove|update)`,
					`\bdnf\b.*(install|remove|update)`,
					// Privilege escalation
					`\bsudo\b`, `\bsu\b\s`,
					// Process termination
					`\bkill\b`, `\bpkill\b`, `\bkillall\b`,
					// System control
					`\bsystemctl\b`,
					// Disk / low-level
					`\btruncate\b`, `\bdd\b`, `\bmkfs\b`,
				},
			},
		},
		Skills: SkillsConfig{
			SkillDirs: []string{"~/.local/share/agentloop/vault/skills"},
			Agent:     SkillAgentConfig{}, // empty = inherit from pi.binary / pi.provider / pi.model at runtime
		},
		Logging: LoggingConfig{Level: "info"},
		Evolution: EvolutionConfig{
			Enabled:            true,
			ScoreThreshold:     0.7,
			MetaTokenBudget:    10000,
			MaxOutcomesPerRun:  10,
			SnapshotRetainMax:  50,
			PipelineConfigPath: "",
			MinCooldownSeconds: 300,
			MaxDailyRuns:       10,
		},
		Orchestrator: OrchestratorConfig{
			Planner: PiConfig{
				Binary:   "pi",
				Provider: "anthropic",
				Model:    "claude-opus-4-6",
				NoSkills: true,
			},
			Worker: PiConfig{
				Binary:   "pi",
				Provider: "anthropic",
				Model:    "claude-sonnet-4-6",
				NoSkills: true,
			},
			Judge: PiConfig{
				Binary:   "pi",
				Provider: "anthropic",
				Model:    "claude-opus-4-6",
				NoSkills: true,
			},
			WorkerPoolSize:   4,
			MaxIterations:    3,
			SummaryMaxTokens: 1500,
		},
	}
}
