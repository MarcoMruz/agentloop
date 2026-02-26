package memory

import "strings"

type CompactionResult struct {
	Text           string
	TokenEstimate  int
	EntriesRemoved int
}

type Compactor struct {
	strategy string // "rolling", "facts", "topics"
}

func NewCompactor(strategy string) *Compactor {
	return &Compactor{strategy: strategy}
}

// Compact reduces conversation history to fit within the token budget.
func (c *Compactor) Compact(rawHistory string, maxTokens int) CompactionResult {
	estimated := len(rawHistory) / 4
	if estimated <= maxTokens {
		return CompactionResult{Text: rawHistory, TokenEstimate: estimated}
	}
	switch c.strategy {
	case "facts":
		return compactFacts(rawHistory, maxTokens)
	case "topics":
		return compactTopics(rawHistory, maxTokens)
	default:
		return compactRolling(rawHistory, maxTokens)
	}
}

// compactRolling: keep last N turns verbatim, summarize older as bullets.
func compactRolling(history string, maxTokens int) CompactionResult {
	turns := splitTurns(history)
	maxChars := maxTokens * 4

	recentCount := 5
	if recentCount > len(turns) { recentCount = len(turns) }
	recent := turns[len(turns)-recentCount:]
	older := turns[:len(turns)-recentCount]

	if len(older) == 0 {
		text := strings.Join(recent, "\n")
		if len(text) > maxChars { text = text[len(text)-maxChars:] }
		return CompactionResult{Text: text, TokenEstimate: len(text) / 4}
	}

	// Summarize older: first non-header line of each turn → bullet
	var bullets []string
	for _, turn := range older {
		lines := strings.Split(turn, "\n")
		for _, l := range lines {
			t := strings.TrimSpace(l)
			if t != "" && !strings.HasPrefix(t, "###") {
				bullets = append(bullets, "- "+truncateContent(t, 100))
				break
			}
		}
	}

	summary := "**Earlier (summarized):**\n" + strings.Join(bullets, "\n")
	recentText := "**Recent:**\n" + strings.Join(recent, "\n")
	combined := summary + "\n\n" + recentText

	if len(combined) > maxChars { combined = combined[len(combined)-maxChars:] }
	return CompactionResult{
		Text: combined, TokenEstimate: len(combined) / 4,
		EntriesRemoved: len(older),
	}
}

// compactFacts: extract action/decision lines only.
func compactFacts(history string, maxTokens int) CompactionResult {
	maxChars := maxTokens * 4
	signals := []string{"decided", "confirmed", "created", "fixed", "deployed",
		"merged", "installed", "configured", "updated", "deleted", "error", "failed"}

	var facts []string
	for _, line := range strings.Split(history, "\n") {
		t := strings.TrimSpace(line)
		if t == "" { continue }
		lower := strings.ToLower(t)
		for _, sig := range signals {
			if strings.Contains(lower, sig) || strings.HasPrefix(t, "- ") || strings.HasPrefix(t, "Tools:") {
				facts = append(facts, truncateContent(t, 150))
				break
			}
		}
	}

	text := "**Key facts:**\n" + strings.Join(last(facts, 30), "\n")
	if len(text) > maxChars { text = text[len(text)-maxChars:] }
	return CompactionResult{Text: text, TokenEstimate: len(text) / 4, EntriesRemoved: len(strings.Split(history, "\n")) - len(facts)}
}

// compactTopics: keep only the most recent exchange per topic.
func compactTopics(history string, maxTokens int) CompactionResult {
	maxChars := maxTokens * 4
	turns := splitTurns(history)
	topicMap := make(map[string]string)

	for _, turn := range turns {
		words := strings.Fields(strings.TrimLeft(turn, "# \n"))
		if len(words) > 3 { words = words[:3] }
		topic := strings.ToLower(strings.Join(words, " "))
		topicMap[topic] = turn
	}

	var parts []string
	for _, v := range topicMap { parts = append(parts, v) }
	text := strings.Join(parts, "\n")
	if len(text) > maxChars { text = text[len(text)-maxChars:] }
	return CompactionResult{Text: text, TokenEstimate: len(text) / 4, EntriesRemoved: len(turns) - len(topicMap)}
}

func splitTurns(history string) []string {
	parts := strings.Split(history, "### ")
	var out []string
	for _, p := range parts {
		t := strings.TrimSpace(p)
		if t != "" { out = append(out, "### "+t) }
	}
	return out
}

func last(ss []string, n int) []string {
	if len(ss) <= n { return ss }
	return ss[len(ss)-n:]
}
