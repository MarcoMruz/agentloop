package security

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidatePathTraversal(t *testing.T) {
	tmp := t.TempDir()
	allowed := filepath.Join(tmp, "safe")
	os.MkdirAll(allowed, 0755)
	err := ValidatePath(allowed+"/../../../etc/passwd", []string{allowed})
	if err == nil {
		t.Fatal("SECURITY: path traversal not blocked")
	}
}

func TestValidatePathAllowed(t *testing.T) {
	tmp := t.TempDir()
	err := ValidatePath(filepath.Join(tmp, "file.txt"), []string{tmp})
	if err != nil {
		t.Fatalf("expected allowed path: %v", err)
	}
}

func TestValidateDockerBlocked(t *testing.T) {
	err := ValidateDockerCommand("docker run -v /etc:/host ubuntu", []string{"run"}, []string{"/etc"})
	if err == nil {
		t.Fatal("SECURITY: blocked volume mount not detected")
	}
}

func TestValidateDockerSubcommand(t *testing.T) {
	err := ValidateDockerCommand("docker system prune", []string{"ps", "logs"}, nil)
	if err == nil {
		t.Fatal("SECURITY: disallowed subcommand not blocked")
	}
}
