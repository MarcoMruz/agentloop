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
	if err := v.ReadInConfig(); err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
		return cfg, nil
	}
	if err := v.Unmarshal(cfg); err != nil {
		return nil, err
	}
	cfg.Server.SocketPath = expandHome(cfg.Server.SocketPath)
	cfg.Vault.Path = expandHome(cfg.Vault.Path)
	cfg.Logging.File = expandHome(cfg.Logging.File)
	for i, d := range cfg.Skills.SkillDirs {
		cfg.Skills.SkillDirs[i] = expandHome(d)
	}
	return cfg, nil
}

func expandHome(path string) string {
	if len(path) > 1 && path[:2] == "~/" {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, path[2:])
	}
	return path
}
