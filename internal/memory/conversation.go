package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type ConversationStore struct {
	vaultPath  string
	retainDays int
}

func NewConversationStore(vaultPath string, retainDays int) *ConversationStore {
	return &ConversationStore{vaultPath: vaultPath, retainDays: retainDays}
}

func (cs *ConversationStore) dir() string {
	return filepath.Join(cs.vaultPath, "memory", "contexts")
}

func (cs *ConversationStore) dayPath(userId string, date string) string {
	return filepath.Join(cs.dir(), fmt.Sprintf("%s-%s.md", userId, date))
}

// Append adds a conversation turn to today's log.
func (cs *ConversationStore) Append(userId string, role string, content string) error {
	date := time.Now().Format("2006-01-02")
	path := cs.dayPath(userId, date)
	os.MkdirAll(cs.dir(), 0755)

	entry := fmt.Sprintf("### %s [%s]\n%s\n\n",
		time.Now().Format("15:04:05"), role,
		truncateContent(content, 500))

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil { return err }
	defer f.Close()
	_, err = f.WriteString(entry)
	return err
}

// GetRecent returns conversation history for the last N entries.
func (cs *ConversationStore) GetRecent(userId string, maxEntries int) (string, error) {
	entries, err := os.ReadDir(cs.dir())
	if err != nil { return "", nil } // empty is fine

	prefix := userId + "-"
	var files []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), prefix) && strings.HasSuffix(e.Name(), ".md") {
			files = append(files, e.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(files)))

	var parts []string
	totalEntries := 0
	for _, file := range files {
		if totalEntries >= maxEntries { break }
		data, err := os.ReadFile(filepath.Join(cs.dir(), file))
		if err != nil { continue }
		content := string(data)
		parts = append(parts, content)
		totalEntries += strings.Count(content, "### ")
	}

	return strings.Join(parts, "\n---\n"), nil
}

func truncateContent(s string, n int) string {
	if len(s) <= n { return s }
	return s[:n] + "..."
}
