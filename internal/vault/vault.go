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
	} {
		if err := os.MkdirAll(d, 0755); err != nil {
			return nil, err
		}
	}
	return &Vault{RootPath: rootPath}, nil
}

func (v *Vault) SessionsDir() string { return filepath.Join(v.RootPath, "sessions") }

// Path returns the vault root directory path.
func (v *Vault) Path() string { return v.RootPath }
