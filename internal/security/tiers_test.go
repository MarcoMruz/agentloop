package security

import (
	"path/filepath"
	"testing"

	"github.com/MarcoMruz/agentloop/internal/config"
)

func TestClassifyBashCommand(t *testing.T) {
	cfg := config.Defaults().Security

	tests := []struct {
		name     string
		command  string
		expected SecurityTier
	}{
		// Safe operations
		{"list files", "ls -la", TierAllow},
		{"show file content", "cat README.md", TierAllow},
		{"find files", "find . -name '*.go'", TierAllow},
		{"git status", "git status", TierAllow},
		{"grep search", "grep -r 'pattern' .", TierAllow},
		{"show current dir", "pwd", TierAllow},
		{"echo text", "echo 'hello world'", TierAllow},
		
		// Logged operations
		{"npm install", "npm install package", TierLog},
		{"create directory", "mkdir newdir", TierLog},
		{"git checkout", "git checkout branch", TierLog},
		{"copy file", "cp file1 file2", TierLog},
		{"move file", "mv old new", TierLog},
		{"go build", "go build .", TierLog},
		
		// HITL required operations
		{"sudo command", "sudo apt install", TierHITL},
		{"change permissions", "chmod 755 file", TierHITL},
		{"change owner", "chown user:group file", TierHITL},
		{"recursive remove", "rm -rf directory", TierHITL},
		{"systemctl", "systemctl restart nginx", TierHITL},
		{"curl POST", "curl -X POST http://api.com", TierHITL},
		
		// Always blocked operations
		{"dangerous rm", "rm -rf /", TierBlock},
		{"format disk", "mkfs /dev/sda1", TierBlock},
		{"fork bomb", ":(){ :|:& };:", TierBlock},
		{"dd command", "dd if=/dev/zero of=/dev/sda", TierBlock},
		{"shutdown", "shutdown -h now", TierBlock},
		
		// Unknown commands default to HITL
		{"unknown command", "someunknowncommand", TierHITL},
		{"complex pipeline", "cat file | awk '{print $1}' | sort", TierAllow}, // starts with cat, so classified as safe
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := classifyBashCommand(tt.command, &cfg)
			if result != tt.expected {
				t.Errorf("classifyBashCommand(%q) = %v, want %v", 
					tt.command, result, tt.expected)
			}
		})
	}
}

func TestClassifyDockerCommand(t *testing.T) {
	cfg := config.Defaults().Security

	tests := []struct {
		name     string
		command  string
		expected SecurityTier
	}{
		// Safe docker operations
		{"docker ps", "docker ps", TierAllow},
		{"docker logs", "docker logs container", TierAllow},
		{"docker images", "docker images", TierAllow},
		{"docker stats", "docker stats", TierAllow},
		{"docker inspect", "docker inspect container", TierAllow},
		
		// Logged docker operations
		{"docker build", "docker build -t image .", TierLog},
		{"docker run", "docker run image", TierLog},
		{"docker exec", "docker exec -it container bash", TierLog},
		
		// HITL required operations
		{"docker rm", "docker rm container", TierHITL},
		{"docker stop", "docker stop container", TierHITL},
		{"docker compose", "docker compose up", TierHITL},
		
		// Always blocked (dangerous volume mounts)
		{"mount /etc", "docker run -v /etc:/etc image", TierBlock},
		{"mount /var", "docker run -v /var:/var image", TierBlock},
		{"mount /root", "docker run -v /root:/root image", TierBlock},
		
		// Safe volume mounts are logged
		{"safe mount", "docker run -v ~/projects:/app image", TierLog},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := classifyDockerCommand(tt.command, &cfg)
			if result != tt.expected {
				t.Errorf("classifyDockerCommand(%q) = %v, want %v", 
					tt.command, result, tt.expected)
			}
		})
	}
}

func TestClassifyFileOperation(t *testing.T) {
	cfg := config.Defaults().Security
	
	// Create temp dir for testing with absolute paths
	tempDir := t.TempDir()
	
	// Override allowed paths to use absolute paths for consistent testing
	cfg.AllowedPaths = []string{tempDir}

	tests := []struct {
		name     string
		toolName string
		filePath string
		expected SecurityTier
	}{
		// Operations in allowed paths
		{"write to project", "write", filepath.Join(tempDir, "test.txt"), TierLog},
		{"edit allowed file", "edit", filepath.Join(tempDir, "allowed.yaml"), TierLog},
		
		// Operations in sensitive locations require HITL
		{"write to .git", "write", ".git/config", TierHITL},
		{"edit node_modules", "edit", "node_modules/package/index.js", TierHITL},
		{"write .env file", "write", ".env.local", TierHITL},
		{"edit credentials", "edit", "credentials.json", TierHITL},
		
		// Operations outside allowed paths require HITL  
		{"write to /etc", "write", "/etc/hosts", TierHITL},
		{"edit system file", "edit", "/var/log/system.log", TierHITL},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := classifyFileOperation(tt.toolName, tt.filePath, &cfg)
			if result != tt.expected {
				t.Errorf("classifyFileOperation(%q, %q) = %v, want %v", 
					tt.toolName, tt.filePath, result, tt.expected)
			}
		})
	}
}

func TestClassifyReadOperation(t *testing.T) {
	cfg := config.Defaults().Security

	tests := []struct {
		name     string
		filePath string
		expected SecurityTier
	}{
		// Regular file reads are safe
		{"read source code", "main.go", TierAllow},
		{"read README", "README.md", TierAllow},
		{"read regular text", "document.txt", TierAllow},
		
		// Sensitive file reads are logged
		{"read .env", ".env", TierLog},
		{"read credentials", "credentials.json", TierLog},
		{"read config", "config.yaml", TierLog},
		{"read private key", "id_rsa", TierLog},
		{"read passwd", "/etc/passwd", TierLog},
		{"read secret", "secret.txt", TierLog},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := classifyReadOperation(tt.filePath, &cfg)
			if result != tt.expected {
				t.Errorf("classifyReadOperation(%q) = %v, want %v", 
					tt.filePath, result, tt.expected)
			}
		})
	}
}

func TestClassifyOperation(t *testing.T) {
	cfg := config.Defaults().Security

	tests := []struct {
		name     string
		toolName string
		command  string
		filePath string
		expected SecurityTier
	}{
		{"bash ls", "bash", "ls -la", "", TierAllow},
		{"bash sudo", "bash", "sudo command", "", TierHITL},
		{"write safe", "write", "", "~/projects/file.txt", TierLog},
		{"read safe", "read", "", "main.go", TierAllow},
		{"read sensitive", "read", "", ".env", TierLog},
		{"unknown tool", "unknown", "", "", TierLog},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ClassifyOperation(tt.toolName, tt.command, tt.filePath, &cfg)
			if result != tt.expected {
				t.Errorf("ClassifyOperation(%q, %q, %q) = %v, want %v", 
					tt.toolName, tt.command, tt.filePath, result, tt.expected)
			}
		})
	}
}

func TestShouldRequireApproval(t *testing.T) {
	tests := []struct {
		tier     SecurityTier
		expected bool
	}{
		{TierAllow, false},
		{TierLog, false},
		{TierHITL, true},
		{TierBlock, false}, // Block is handled differently
	}

	for _, tt := range tests {
		result := ShouldRequireApproval(tt.tier)
		if result != tt.expected {
			t.Errorf("ShouldRequireApproval(%v) = %v, want %v", 
				tt.tier, result, tt.expected)
		}
	}
}

func TestShouldBlock(t *testing.T) {
	tests := []struct {
		tier     SecurityTier
		expected bool
	}{
		{TierAllow, false},
		{TierLog, false},
		{TierHITL, false},
		{TierBlock, true},
	}

	for _, tt := range tests {
		result := ShouldBlock(tt.tier)
		if result != tt.expected {
			t.Errorf("ShouldBlock(%v) = %v, want %v", 
				tt.tier, result, tt.expected)
		}
	}
}

func TestSecurityTierFallback(t *testing.T) {
	// Test that when policy_mode is not "selective", it falls back to HITL
	cfg := config.Defaults().Security
	cfg.PolicyMode = "strict"

	result := ClassifyOperation("bash", "ls -la", "", &cfg)
	if result != TierHITL {
		t.Errorf("Expected fallback to HITL for non-selective policy mode, got %v", result)
	}
}