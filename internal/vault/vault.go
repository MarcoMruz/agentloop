package vault

import (
	"os"
	"path/filepath"
)

type Vault struct{ RootPath string }

func New(rootPath string) (*Vault, error) {
	for _, d := range []string{
		filepath.Join(rootPath, "sessions"),
		filepath.Join(rootPath, "memory", "users"),
		filepath.Join(rootPath, "memory", "contexts"),
		filepath.Join(rootPath, "memory", "cache"),
		filepath.Join(rootPath, "skills"),
		filepath.Join(rootPath, "agents"),
	} {
		if err := os.MkdirAll(d, 0755); err != nil {
			return nil, err
		}
	}
	return &Vault{RootPath: rootPath}, nil
}

func (v *Vault) SessionsDir() string { return filepath.Join(v.RootPath, "sessions") }

// AgentsDir returns the path to the agents directory within the vault.
func (v *Vault) AgentsDir() string { return filepath.Join(v.RootPath, "agents") }

// Path returns the vault root directory path.
func (v *Vault) Path() string { return v.RootPath }

// InstallDefaultAgents copies agent markdown files from configAgentsDir into the
// vault agents directory. Existing files are not overwritten. If configAgentsDir
// does not exist, the function returns nil silently.
func (v *Vault) InstallDefaultAgents(configAgentsDir string) error {
	entries, err := os.ReadDir(configAgentsDir)
	if err != nil {
		return nil // no config agents dir — skip silently
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		dest := filepath.Join(v.RootPath, "agents", e.Name())
		if _, err := os.Stat(dest); err == nil {
			continue // already exists — don't overwrite
		}
		data, err := os.ReadFile(filepath.Join(configAgentsDir, e.Name()))
		if err != nil {
			continue
		}
		os.WriteFile(dest, data, 0644)
	}
	return nil
}
