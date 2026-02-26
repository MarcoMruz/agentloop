package hitl

import (
	"fmt"
	"strings"
)

type Decision string

const (
	DecisionApprove Decision = "approve"
	DecisionSkip    Decision = "skip"
	DecisionAbort   Decision = "abort"
)

func FormatSummary(toolName string, args map[string]any, title string) string {
	var sb strings.Builder
	sb.WriteString("\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	sb.WriteString(fmt.Sprintf("⚠  HUMAN REVIEW REQUIRED\n"))
	sb.WriteString("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	if title != "" {
		sb.WriteString(title + "\n\n")
	}
	sb.WriteString(fmt.Sprintf("Tool: %s\n", toolName))
	if len(args) > 0 {
		sb.WriteString("Parameters:\n")
		for k, v := range args {
			sb.WriteString(fmt.Sprintf("  %-20s %v\n", k+":", v))
		}
	}
	sb.WriteString("\n[a] Approve  [s] Skip  [q] Abort\n> ")
	return sb.String()
}
