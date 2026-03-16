package version

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Snapshotter struct {
	vaultPath string
	agentsMD  string
}

func NewSnapshotter(vaultPath, agentsMDPath string) *Snapshotter {
	return &Snapshotter{vaultPath: vaultPath, agentsMD: agentsMDPath}
}

func (s *Snapshotter) Take() (string, error) {
	ts := time.Now().Format("20060102-150405")
	evolvedDir := filepath.Join(s.vaultPath, "memory", "evolved")
	snapshotDir := filepath.Join(evolvedDir, "snapshots", ts)

	if err := os.MkdirAll(snapshotDir, 0755); err != nil {
		return "", err
	}

	src := filepath.Join(evolvedDir, "pipeline-config.yaml")
	if data, err := os.ReadFile(src); err == nil {
		os.WriteFile(filepath.Join(snapshotDir, "pipeline-config.yaml"), data, 0644)
	}

	if s.agentsMD != "" {
		if data, err := os.ReadFile(s.agentsMD); err == nil {
			content := string(data)
			start := strings.Index(content, "<!-- EVOLVED:START -->")
			end := strings.Index(content, "<!-- EVOLVED:END -->")
			if start != -1 && end != -1 {
				section := content[start : end+len("<!-- EVOLVED:END -->")]
				os.WriteFile(filepath.Join(snapshotDir, "agents-md-section.md"), []byte(section), 0644)
			}
		}
	}

	return ts, nil
}
