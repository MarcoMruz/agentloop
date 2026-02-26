package memory

import (
	"log/slog"
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
func (e *Engine) RecordInteraction(userId string, userMsg string, agentReply string, toolsUsed []string) {
	// Invalidate cache
	e.cache.Delete("ctx:" + userId)

	// Append to conversation log
	if err := e.conversations.Append(userId, "user", userMsg); err != nil {
		slog.Warn("failed to log user message", "error", err)
	}

	summary := agentReply
	if len(summary) > 500 { summary = summary[:500] + "..." }
	if err := e.conversations.Append(userId, "assistant", summary); err != nil {
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

func estimateTokens(s string) int { return len(s) / 4 }
