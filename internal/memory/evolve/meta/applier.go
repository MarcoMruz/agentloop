package meta

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/MarcoMruz/agentloop/internal/memory/evolve"
	"github.com/MarcoMruz/agentloop/internal/memory/evolve/version"
)

const (
	evolvedStartMarker = "<!-- EVOLVED:START -->"
	evolvedEndMarker   = "<!-- EVOLVED:END -->"
)

type Applier struct {
	vaultPath  string
	agentsMD   string
	skillsPath string
}

func NewApplier(vaultPath, agentsMDPath, skillsPath string) *Applier {
	return &Applier{
		vaultPath:  vaultPath,
		agentsMD:   agentsMDPath,
		skillsPath: skillsPath,
	}
}

func (a *Applier) Apply(proposal *EvolutionProposal) error {
	_, err := a.Snapshot()
	if err != nil {
		slog.Warn("snapshot failed, continuing", "error", err)
	}

	if proposal.ConfigChanges != nil {
		if err := a.ApplyConfig(proposal.ConfigChanges); err != nil {
			return fmt.Errorf("apply config: %w", err)
		}
	}

	for _, sp := range proposal.SkillChanges {
		if err := a.ApplySkill(sp); err != nil {
			slog.Warn("skill apply failed", "name", sp.Name, "error", err)
		}
	}

	if proposal.AgentsMDPatch != "" && a.agentsMD != "" {
		if err := a.ApplyAgentsMD(proposal.AgentsMDPatch); err != nil {
			slog.Warn("agents.md apply failed", "error", err)
		}
	}

	a.gitCommit(proposal.Summary)
	a.logEvolution(proposal)

	return nil
}

func (a *Applier) ApplyConfig(cfg *evolve.PipelineConfig) error {
	dir := filepath.Join(a.vaultPath, "memory", "evolved")
	os.MkdirAll(dir, 0755)
	return evolve.SavePipelineConfig(filepath.Join(dir, "pipeline-config.yaml"), cfg)
}

func (a *Applier) ApplySkill(sp SkillProposal) error {
	if !strings.HasPrefix(sp.Name, "evolved-") {
		return fmt.Errorf("skill name must be prefixed with 'evolved-', got: %s", sp.Name)
	}

	skillDir := filepath.Join(a.skillsPath, sp.Name)

	switch sp.Action {
	case "delete":
		return os.RemoveAll(skillDir)
	case "create", "update":
		os.MkdirAll(skillDir, 0755)
		content := buildSkillMD(sp)
		return os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0644)
	default:
		return fmt.Errorf("unknown skill action: %s", sp.Action)
	}
}

func (a *Applier) ApplyAgentsMD(patch string) error {
	data, err := os.ReadFile(a.agentsMD)
	if err != nil {
		return err
	}

	content := string(data)
	evolvedBlock := fmt.Sprintf("%s\n%s\n%s", evolvedStartMarker, patch, evolvedEndMarker)

	startIdx := strings.Index(content, evolvedStartMarker)
	endIdx := strings.Index(content, evolvedEndMarker)

	if startIdx == -1 || endIdx == -1 {
		content = content + "\n" + evolvedBlock + "\n"
	} else {
		content = content[:startIdx] + evolvedBlock + content[endIdx+len(evolvedEndMarker):]
	}

	return os.WriteFile(a.agentsMD, []byte(content), 0644)
}

func (a *Applier) Snapshot() (string, error) {
	snap := version.NewSnapshotter(a.vaultPath, a.agentsMD)
	return snap.Take()
}

func (a *Applier) gitCommit(summary string) {
	vaultDir := a.vaultPath

	cmd := exec.Command("git", "rev-parse", "--git-dir")
	cmd.Dir = vaultDir
	if err := cmd.Run(); err != nil {
		init := exec.Command("git", "init")
		init.Dir = vaultDir
		if err := init.Run(); err != nil {
			slog.Warn("git init failed", "error", err)
			return
		}
		gitignore := "memory/evolved/metrics/*.jsonl\nmemory/cache/\n"
		os.WriteFile(filepath.Join(vaultDir, ".gitignore"), []byte(gitignore), 0644)
	}

	add := exec.Command("git", "add", "-A")
	add.Dir = vaultDir
	if err := add.Run(); err != nil {
		slog.Warn("git add failed", "error", err)
		return
	}

	msg := "evolve: " + summary
	commit := exec.Command("git", "commit", "-m", msg, "--allow-empty")
	commit.Dir = vaultDir
	if err := commit.Run(); err != nil {
		slog.Debug("git commit skipped", "error", err)
	}
}

func (a *Applier) logEvolution(proposal *EvolutionProposal) {
	logPath := filepath.Join(a.vaultPath, "memory", "evolved", "evolution-log.jsonl")
	entry := struct {
		Timestamp string `json:"timestamp"`
		Summary   string `json:"summary"`
		Reasoning string `json:"reasoning"`
	}{
		Timestamp: time.Now().Format(time.RFC3339),
		Summary:   proposal.Summary,
		Reasoning: proposal.Reasoning,
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return
	}
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	f.Write(data)
	f.Write([]byte("\n"))
}

func buildSkillMD(sp SkillProposal) string {
	var sb strings.Builder
	sb.WriteString("---\n")
	sb.WriteString(fmt.Sprintf("name: %s\n", sp.Name))
	sb.WriteString(fmt.Sprintf("description: %s\n", sp.Description))
	sb.WriteString("triggers:\n")
	for _, t := range sp.Triggers {
		sb.WriteString(fmt.Sprintf("  - %s\n", t))
	}
	sb.WriteString("---\n\n")
	sb.WriteString(sp.Content)
	return sb.String()
}
