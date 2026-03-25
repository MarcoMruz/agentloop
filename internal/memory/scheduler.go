package memory

import "strings"

// ContextLevel controls how much memory context is retrieved for a task.
type ContextLevel int

const (
	Minimal  ContextLevel = iota // profile + top-3 notes only (~500 tokens)
	Standard                     // profile + top-8 notes + compacted recent history
	Detailed                     // full profile + top-16 notes + full history
)

// complexityKeywords trigger Detailed level regardless of word count.
var complexityKeywords = []string{
	"refactor", "architect", "redesign", "migrate", "integrate",
	"security", "performance", "debug", "analyse", "analyze",
}

// Schedule returns the appropriate ContextLevel for a task.
// Minimal: ≤3 words and no complexity keywords.
// Detailed: >25 words OR contains a complexity keyword.
// Standard: everything else.
func Schedule(task string) ContextLevel {
	words := strings.Fields(strings.ToLower(task))
	n := len(words)

	for _, kw := range complexityKeywords {
		for _, w := range words {
			if strings.HasPrefix(w, kw) {
				return Detailed
			}
		}
	}
	if n <= 3 {
		return Minimal
	}
	if n > 25 {
		return Detailed
	}
	return Standard
}
