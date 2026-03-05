package config

import (
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

func DefaultConfigPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "agentloop", "agentloop.yaml")
}

func Load(path string) (*Config, error) {
	cfg := Defaults()
	v := viper.New()
	v.SetConfigFile(path)
	v.SetEnvPrefix("AGENTLOOP")
	v.AutomaticEnv()

	// configDir is used to resolve relative paths (e.g. "./vault") anchored to
	// the config file's location rather than the process working directory.
	configDir := filepath.Dir(path)
	if absDir, err := filepath.Abs(configDir); err == nil {
		configDir = absDir
	}

	if err := v.ReadInConfig(); err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
		return cfg, nil
	}
	if err := v.Unmarshal(cfg); err != nil {
		return nil, err
	}
	cfg.Server.SocketPath = resolvePath(cfg.Server.SocketPath, configDir)
	cfg.Vault.Path = resolvePath(cfg.Vault.Path, configDir)
	cfg.Logging.File = resolvePath(cfg.Logging.File, configDir)
	for i, d := range cfg.Skills.SkillDirs {
		cfg.Skills.SkillDirs[i] = resolvePath(d, configDir)
	}
	return cfg, nil
}

// resolvePath expands ~ home-relative paths and resolves ./ relative paths
// against configDir so they are anchored to the config file's location.
func resolvePath(path, configDir string) string {
	if path == "" {
		return path
	}
	// Expand ~/
	if len(path) >= 2 && path[:2] == "~/" {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, path[2:])
	}
	// Resolve relative paths against the config file's directory
	if !filepath.IsAbs(path) {
		return filepath.Join(configDir, path)
	}
	return path
}
