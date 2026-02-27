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

func (cs *ConversationStore) idxPath(userId string, date string) string {
	return filepath.Join(cs.dir(), fmt.Sprintf("%s-%s.idx.json", userId, date))
}

// Append adds a conversation turn to today's log and updates the index sidecar.
func (cs *ConversationStore) Append(userId string, role string, content string) error {
	date := time.Now().Format("2006-01-02")
	ts := time.Now().Format("15:04:05")
	path := cs.dayPath(userId, date)
	os.MkdirAll(cs.dir(), 0755)

	entry := fmt.Sprintf("### %s [%s]\n%s\n\n",
		ts, role,
		truncateContent(content, 500))

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil { return err }
	if _, err = f.WriteString(entry); err != nil {
		f.Close()
		return err
	}
	f.Close()

	// Update lightweight index sidecar for relevance-based retrieval
	idxEntry := buildIndexEntry(ts, role, content)
	_ = appendIndexEntry(cs.idxPath(userId, date), idxEntry)

	return nil
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

// GetRecentIndexed returns up to maxEntries IndexEntry objects from the most recent
// daily index files, in reverse-chronological order (newest file first).
func (cs *ConversationStore) GetRecentIndexed(userId string, maxEntries int) ([]IndexEntry, error) {
	entries, err := os.ReadDir(cs.dir())
	if err != nil { return nil, nil }

	prefix := userId + "-"
	var files []string
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, prefix) && strings.HasSuffix(name, ".idx.json") {
			files = append(files, name)
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(files)))

	var out []IndexEntry
	for _, file := range files {
		if len(out) >= maxEntries { break }
		idx := loadIndex(filepath.Join(cs.dir(), file))
		// Append in reverse so newest entries within the file come first
		for i := len(idx.Entries) - 1; i >= 0 && len(out) < maxEntries; i-- {
			out = append(out, idx.Entries[i])
		}
	}
	return out, nil
}

func truncateContent(s string, n int) string {
	if len(s) <= n { return s }
	return s[:n] + "..."
}
