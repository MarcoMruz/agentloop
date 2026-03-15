package vault

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type SessionNote struct {
	Frontmatter SessionFrontmatter
	TaskText    string
	Transcript  string   // raw conversation transcript from pi
	ToolCalls   []string // tool names used
	HITLLog     []HITLEntry
}

type HITLEntry struct {
	Timestamp time.Time
	ToolName  string
	Decision  string
}

func (v *Vault) Write(note SessionNote) error {
	filename := fmt.Sprintf("%s-%s.md", note.Frontmatter.Created.Format("2006-01-02"), note.Frontmatter.ID)
	return os.WriteFile(filepath.Join(v.SessionsDir(), filename), []byte(renderSession(note)), 0644)
}

// WriteSession is an alias for Write, used by the session manager.
func (v *Vault) WriteSession(note SessionNote) error {
	return v.Write(note)
}

func (v *Vault) Read(sessionID string) (*SessionNote, error) {
	entries, err := os.ReadDir(v.SessionsDir())
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), sessionID) {
			data, err := os.ReadFile(filepath.Join(v.SessionsDir(), e.Name()))
			if err != nil {
				return nil, err
			}
			yamlBlock, body := ParseFrontmatter(string(data))
			var fm SessionFrontmatter
			if err := yaml.Unmarshal([]byte(yamlBlock), &fm); err != nil {
				return nil, err
			}
			return &SessionNote{Frontmatter: fm, TaskText: strings.TrimSpace(body)}, nil
		}
	}
	return nil, fmt.Errorf("session %q not found", sessionID)
}

func renderSession(note SessionNote) string {
	fm := note.Frontmatter
	var sb strings.Builder
	sb.WriteString("---\n")
	sb.WriteString(fmt.Sprintf("id: %s\ntitle: %s\n", fm.ID, fm.Title))
	sb.WriteString(fmt.Sprintf("created: %s\nupdated: %s\n", fm.Created.Format(time.RFC3339), fm.Updated.Format(time.RFC3339)))
	sb.WriteString(fmt.Sprintf("status: %s\nprovider: %s\nmodel: %s\n", fm.Status, fm.Provider, fm.Model))
	if fm.Source != "" {
		sb.WriteString(fmt.Sprintf("source: %s\n", fm.Source))
	}
	if fm.UserID != "" {
		sb.WriteString(fmt.Sprintf("user_id: %s\n", fm.UserID))
	}
	if len(fm.Tags) > 0 {
		sb.WriteString("tags: [" + strings.Join(fm.Tags, ", ") + "]\n")
	}
	if fm.ThreadID != "" {
		sb.WriteString(fmt.Sprintf("thread_id: %s\n", fm.ThreadID))
	}
	if fm.ChannelID != "" {
		sb.WriteString(fmt.Sprintf("channel_id: %s\n", fm.ChannelID))
	}
	if fm.ConversationContextID != "" {
		sb.WriteString(fmt.Sprintf("conversation_context_id: %s\n", fm.ConversationContextID))
	}
	sb.WriteString("---\n\n## Task\n\n" + note.TaskText + "\n\n")
	if len(note.ToolCalls) > 0 {
		sb.WriteString("## Tools Used\n\n")
		for _, tc := range note.ToolCalls {
			sb.WriteString("- " + tc + "\n")
		}
		sb.WriteString("\n")
	}
	if len(note.HITLLog) > 0 {
		sb.WriteString("## HITL Log\n\n")
		for _, e := range note.HITLLog {
			sb.WriteString(fmt.Sprintf("- %s | %s | %s\n", e.Timestamp.Format("15:04:05"), e.ToolName, e.Decision))
		}
		sb.WriteString("\n")
	}
	if note.Transcript != "" {
		sb.WriteString("## Transcript\n\n```\n" + note.Transcript + "\n```\n")
	}
	return sb.String()
}
