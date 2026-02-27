package memory

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"
)

// IndexEntry is a lightweight, searchable summary of a single conversation turn.
// Stored as a JSON sidecar alongside each daily markdown conversation file.
type IndexEntry struct {
	Timestamp string   `json:"ts"`
	Role      string   `json:"role"`
	Keywords  []string `json:"keywords"`
	Topics    []string `json:"topics"`
	Summary   string   `json:"summary"`
}

// ConversationIndex holds all indexed entries for one user-day file.
type ConversationIndex struct {
	Entries []IndexEntry `json:"entries"`
}

// loadIndex reads a sidecar index file. Returns empty index if missing or corrupt.
func loadIndex(path string) ConversationIndex {
	data, err := os.ReadFile(path)
	if err != nil {
		return ConversationIndex{}
	}
	var idx ConversationIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		return ConversationIndex{}
	}
	return idx
}

// saveIndex writes the index to disk.
func saveIndex(path string, idx ConversationIndex) error {
	data, err := json.Marshal(idx)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// appendIndexEntry appends a single entry to the sidecar index at path.
func appendIndexEntry(path string, entry IndexEntry) error {
	idx := loadIndex(path)
	idx.Entries = append(idx.Entries, entry)
	return saveIndex(path, idx)
}

// buildIndexEntry extracts keywords, topics, and a summary from a raw message.
func buildIndexEntry(ts, role, content string) IndexEntry {
	return IndexEntry{
		Timestamp: ts,
		Role:      role,
		Keywords:  extractKeywords(content),
		Topics:    extractTopics(content),
		Summary:   summarizeEntry(content),
	}
}

// scoreEntry returns a relevance score [0.0, 1.2] for an entry against task keywords.
// Base score: keyword overlap (Jaccard-like). Topic bonus: +0.2 if topics overlap.
func scoreEntry(taskKeywords []string, taskTopics []string, entry IndexEntry) float64 {
	if len(taskKeywords) == 0 {
		return 0
	}

	taskKwSet := make(map[string]bool, len(taskKeywords))
	for _, k := range taskKeywords {
		taskKwSet[k] = true
	}

	overlap := 0
	for _, k := range entry.Keywords {
		if taskKwSet[k] {
			overlap++
		}
	}

	score := float64(overlap) / float64(len(taskKeywords))

	// Topic bonus
	taskTopicSet := make(map[string]bool, len(taskTopics))
	for _, t := range taskTopics {
		taskTopicSet[t] = true
	}
	for _, t := range entry.Topics {
		if taskTopicSet[t] {
			score += 0.2
			break
		}
	}

	return score
}

// extractKeywords returns up to 15 meaningful keywords from content.
// Filters stopwords and short words. Domain-agnostic.
func extractKeywords(content string) []string {
	lower := strings.ToLower(content)

	// Remove markdown headers, punctuation — keep -, /, . for paths/domains
	re := regexp.MustCompile(`[^\w\s\-\/\.]`)
	lower = re.ReplaceAllString(lower, " ")

	words := strings.Fields(lower)
	seen := make(map[string]bool)
	var out []string

	for _, w := range words {
		w = strings.Trim(w, ".-/")
		if len(w) < 4 {
			continue
		}
		if stopwords[w] {
			continue
		}
		if seen[w] {
			continue
		}
		seen[w] = true
		out = append(out, w)
		if len(out) == 15 {
			break
		}
	}
	return out
}

// extractTopics matches content keywords against a predefined domain taxonomy.
// Covers both coding/infra and personal-assistant domains.
// Matching: keyword and topic share a common stem of at least 4 chars
// (handles plurals, verb forms: "scheduling"→"schedule", "emails"→"email").
func extractTopics(content string) []string {
	keywords := extractKeywords(content)
	seen := make(map[string]bool)
	var matched []string

	for _, kw := range keywords {
		for _, topic := range topicTaxonomy {
			if !seen[topic] && stemMatch(kw, topic) {
				seen[topic] = true
				matched = append(matched, topic)
			}
		}
	}
	return matched
}

// stemMatch returns true if kw and topic share a common stem of at least 4 chars.
// The stem is the shorter of the two strings minus 1 trailing char (to absorb
// common suffixes: -s, -ed, -er, -ing → "emails"/"email", "scheduling"/"schedule").
func stemMatch(kw, topic string) bool {
	stemLen := len(topic) - 1
	if len(kw) < len(topic) {
		stemLen = len(kw) - 1
	}
	if stemLen < 4 {
		return false
	}
	return strings.HasPrefix(kw, topic[:stemLen]) && strings.HasPrefix(topic, topic[:stemLen])
}

// summarizeEntry produces a short summary (≤120 chars) from a message.
// Takes the first meaningful non-header sentence.
func summarizeEntry(content string) string {
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "---") {
			continue
		}
		// Strip markdown formatting
		line = strings.NewReplacer("**", "", "*", "", "`", "", "_", "").Replace(line)
		line = strings.TrimSpace(line)
		if len(line) < 4 {
			continue
		}
		if len(line) > 120 {
			line = line[:117] + "..."
		}
		return line
	}
	// fallback: truncate raw content
	content = strings.TrimSpace(content)
	if len(content) > 120 {
		content = content[:117] + "..."
	}
	return content
}

// stopwords is the set of common words to ignore during keyword extraction.
var stopwords = map[string]bool{
	"the": true, "and": true, "that": true, "this": true, "with": true,
	"have": true, "from": true, "they": true, "will": true, "been": true,
	"were": true, "their": true, "about": true, "would": true, "there": true,
	"what": true, "when": true, "your": true, "which": true, "then": true,
	"them": true, "some": true, "also": true, "just": true, "like": true,
	"make": true, "want": true, "need": true, "could": true, "should": true,
	"shall": true, "might": true, "must": true, "please": true, "very": true,
	"into": true, "more": true, "than": true, "such": true, "each": true,
	"does": true, "done": true, "over": true, "here": true, "these": true,
	"those": true, "only": true, "even": true, "both": true, "back": true,
	"good": true, "know": true, "take": true, "year": true, "most": true,
	"much": true, "same": true, "come": true, "time": true, "look": true,
	"think": true, "well": true, "away": true,
	"used": true, "find": true, "many": true, "long": true, "down": true,
	"work": true, "tell": true, "give": true, "show": true, "keep": true,
	"last": true, "next": true, "left": true, "hand": true, "move": true,
	"live": true, "feel": true, "high": true, "open": true, "seem": true,
	"hard": true, "help": true, "talk": true, "turn": true, "case": true,
	"file": true, "line": true, "code": true, "data": true,
	"user": true, "said": true, "sure": true, "okay": true,
}

// topicTaxonomy covers coding/infra and personal-assistant domains.
// Substring match: "emails" matches "email", "scheduling" matches "schedule".
var topicTaxonomy = []string{
	// Code & infrastructure
	"auth", "database", "deploy", "frontend", "backend", "test",
	"config", "docker", "migration", "cache", "security", "build",
	"kubernetes", "monitor", "logging", "debug", "refactor", "pipeline",
	// Personal assistant
	"email", "calendar", "meeting", "schedule", "report", "reminder",
	"invoice", "budget", "travel", "contact", "document", "slack",
	"ticket", "review", "interview", "presentation", "deadline",
	"shopping", "finance", "payment",
}
