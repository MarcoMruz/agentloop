package vault

import (
	"bufio"
	"strings"
	"time"
)

type SessionFrontmatter struct {
	ID       string    `yaml:"id"`
	Title    string    `yaml:"title"`
	Created  time.Time `yaml:"created"`
	Updated  time.Time `yaml:"updated"`
	Status   string    `yaml:"status"`
	Provider string    `yaml:"provider"`
	Model    string    `yaml:"model"`
	Source   string    `yaml:"source"`
	UserID   string    `yaml:"user_id"`
	Tags     []string  `yaml:"tags"`
	// Thread metadata for Slack-sourced sessions. Empty for CLI.
	ThreadID              string `yaml:"thread_id,omitempty"`
	ChannelID             string `yaml:"channel_id,omitempty"`
	ConversationContextID string `yaml:"conversation_context_id,omitempty"`
}

func ParseFrontmatter(content string) (yamlBlock string, body string) {
	scanner := bufio.NewScanner(strings.NewReader(content))
	in, closed := false, false
	var fm, bd []string
	for scanner.Scan() {
		line := scanner.Text()
		if !in && !closed && line == "---" {
			in = true
			continue
		}
		if in && !closed {
			if line == "---" {
				closed = true
				continue
			}
			fm = append(fm, line)
			continue
		}
		bd = append(bd, line)
	}
	return strings.Join(fm, "\n"), strings.Join(bd, "\n")
}
