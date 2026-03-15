package memory

import (
	"fmt"
	"log/slog"
	"sort"
	"strings"
)

type Engine struct {
	profiles      *ProfileStore
	conversations *ConversationStore
	cache         *PromptCache
	compactor     *Compactor
	maxCtxTokens  int
}

func NewEngine(vaultPath string, maxContextTokens int, compactionStrategy string, retainDays int) *Engine {
	return &Engine{
		profiles:      NewProfileStore(vaultPath),
		conversations: NewConversationStore(vaultPath, retainDays),
		cache:         NewPromptCache(),
		compactor:     NewCompactor(compactionStrategy),
		maxCtxTokens:  maxContextTokens,
	}
}

// GetContextForUser builds the full memory context string injected into prompts.
// Structured for prompt cache efficiency: stable profile first, dynamic history last.
func (e *Engine) GetContextForUser(userId string) (string, error) {
	// Check cache first (profile + compacted history are relatively stable)
	if cached := e.cache.Get("ctx:" + userId); cached != "" {
		return cached, nil
	}

	profile, err := e.profiles.Load(userId)
	if err != nil {
		slog.Debug("no profile for user", "userId", userId)
		profile = DefaultProfile(userId)
	}

	rawHistory, err := e.conversations.GetRecent(userId, 30)
	if err != nil {
		rawHistory = ""
	}

	// Compact history to fit within token budget
	profileText := profile.Render()
	profileTokens := estimateTokens(profileText)
	historyBudget := e.maxCtxTokens - profileTokens
	if historyBudget < 200 { historyBudget = 200 }

	compacted := e.compactor.Compact(rawHistory, historyBudget)

	// Assemble
	var ctx string
	if compacted.Text != "" {
		ctx = profileText + "\n\n" + compacted.Text
	} else {
		ctx = profileText
	}

	// Cache for 5 minutes (profile is stable, history changes slowly)
	e.cache.Set("ctx:"+userId, ctx, 5*60)

	return ctx, nil
}

// RecordInteraction saves a conversation turn and updates the user profile.
// Called by the session manager after each completed task.
func (e *Engine) RecordInteraction(userId string, userMsg string, agentReply string, toolsUsed []string, conversationContextID string) {
	// Invalidate global user cache
	e.cache.Delete("ctx:" + userId)
	// Invalidate thread-specific cache
	if conversationContextID != "" {
		e.cache.Delete("ctx:" + userId + ":" + conversationContextID)
	}

	// Append to conversation log
	if err := e.conversations.Append(userId, "user", userMsg, conversationContextID); err != nil {
		slog.Warn("failed to log user message", "error", err)
	}

	summary := agentReply
	if len(summary) > 500 { summary = summary[:500] + "..." }
	if err := e.conversations.Append(userId, "assistant", summary, conversationContextID); err != nil {
		slog.Warn("failed to log assistant message", "error", err)
	}

	// Update profile with learned patterns (heuristic, no LLM)
	e.profiles.UpdateFromInteraction(userId, userMsg, toolsUsed)
}

// UpdateUserFact allows explicit profile updates (e.g. "remember I prefer TypeScript").
func (e *Engine) UpdateUserFact(userId string, key string, value string) error {
	e.cache.Delete("ctx:" + userId)
	return e.profiles.SetFact(userId, key, value)
}

// ForgetUserFact removes a fact from the profile.
func (e *Engine) ForgetUserFact(userId string, key string) error {
	e.cache.Delete("ctx:" + userId)
	return e.profiles.DeleteFact(userId, key)
}

// GetContextForUserWithTask builds a task-aware memory context.
// Instead of dumping all history, it scores each indexed entry against the task
// keywords and returns only the most relevant summaries. Falls back to the 5 most
// recent entries when no keyword overlap is found.
// Works across all domains: coding, email, calendar, reports, scheduling, etc.
func (e *Engine) GetContextForUserWithTask(userId string, task string) (string, error) {
	// Derive a short stable cache key from the task
	taskKey := taskCacheKey(task)
	cacheKey := "ctx:" + userId + ":" + taskKey
	if cached := e.cache.Get(cacheKey); cached != "" {
		return cached, nil
	}

	profile, err := e.profiles.Load(userId)
	if err != nil {
		slog.Debug("no profile for user", "userId", userId)
		profile = DefaultProfile(userId)
	}
	profileText := profile.Render()

	// Load indexed entries (lightweight JSON, not full markdown)
	indexed, err := e.conversations.GetRecentIndexed(userId, 60)
	if err != nil || len(indexed) == 0 {
		// No index yet — fall back to existing context builder
		return e.GetContextForUser(userId)
	}

	// Score entries against task
	taskKw := extractKeywords(task)
	taskTopics := extractTopics(task)

	type scored struct {
		entry IndexEntry
		score float64
	}
	var candidates []scored
	for _, entry := range indexed {
		s := scoreEntry(taskKw, taskTopics, entry)
		candidates = append(candidates, scored{entry, s})
	}

	// Sort descending by score
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})

	const maxRelevant = 8
	const maxFallback = 5

	var historyLines []string
	hasMatches := len(candidates) > 0 && candidates[0].score > 0

	if hasMatches {
		count := 0
		for _, c := range candidates {
			if c.score == 0 || count >= maxRelevant {
				break
			}
			historyLines = append(historyLines, fmt.Sprintf("- [%s %s] %s", c.entry.Timestamp[:5], c.entry.Role, c.entry.Summary))
			count++
		}
	} else {
		// No keyword match — use the most recent entries
		for i, c := range candidates {
			if i >= maxFallback {
				break
			}
			historyLines = append(historyLines, fmt.Sprintf("- [%s %s] %s", c.entry.Timestamp[:5], c.entry.Role, c.entry.Summary))
		}
	}

	var sb strings.Builder
	sb.WriteString(profileText)

	if len(historyLines) > 0 {
		if hasMatches {
			sb.WriteString(fmt.Sprintf("\n\n## Relevant context (%d matches)\n", len(historyLines)))
		} else {
			sb.WriteString("\n\n## Recent context\n")
		}
		sb.WriteString(strings.Join(historyLines, "\n"))
	}

	ctx := sb.String()
	e.cache.Set(cacheKey, ctx, 5*60)
	return ctx, nil
}

// GetContextForUserAndConversationContext builds a memory context scoped to a specific
// conversation thread (e.g., a Slack thread). Only entries tagged with contextID are
// included in history. Falls back to GetContextForUser when contextID is empty.
func (e *Engine) GetContextForUserAndConversationContext(userId, contextID string) (string, error) {
	if contextID == "" {
		return e.GetContextForUser(userId)
	}

	cacheKey := "ctx:" + userId + ":" + contextID
	if cached := e.cache.Get(cacheKey); cached != "" {
		return cached, nil
	}

	profile, err := e.profiles.Load(userId)
	if err != nil {
		profile = DefaultProfile(userId)
	}
	profileText := profile.Render()

	entries, err := e.conversations.GetRecentIndexedByContext(userId, contextID, 30)
	if err != nil {
		return profileText, nil // Return profile-only on error — don't fail the whole prompt
	}

	var historyLines []string
	for _, entry := range entries {
		historyLines = append(historyLines, fmt.Sprintf("- [%s %s] %s", entry.Timestamp[:5], entry.Role, entry.Summary))
	}

	var sb strings.Builder
	sb.WriteString(profileText)
	if len(historyLines) > 0 {
		sb.WriteString(fmt.Sprintf("\n\n## Thread context (%d entries)\n", len(historyLines)))
		sb.WriteString(strings.Join(historyLines, "\n"))
	}

	ctx := sb.String()
	e.cache.Set(cacheKey, ctx, 5*60)
	return ctx, nil
}

// taskCacheKey derives a short stable key from a task string.
// Same task text (including steered tasks) reuses the same cache entry.
func taskCacheKey(task string) string {
	normalized := strings.ToLower(strings.TrimSpace(task))
	normalized = strings.Join(strings.Fields(normalized), " ")
	if len(normalized) > 32 {
		normalized = normalized[:32]
	}
	// Replace characters unsafe for map keys
	normalized = strings.NewReplacer("/", "-", ":", "-", " ", "_").Replace(normalized)
	return normalized
}

func estimateTokens(s string) int { return len(s) / 4 }
