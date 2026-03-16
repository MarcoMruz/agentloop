package baseline

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/MarcoMruz/agentloop/internal/memory"
	"github.com/MarcoMruz/agentloop/internal/memory/evolve"
)

// BaselineRetriever implements evolve.Retriever using Jaccard-style keyword
// overlap scoring with topic bonus, falling back to most-recent entries when
// no keywords match. Results are positioned using "edges" layout (strongest
// items at start and end of the list) for optimal LLM attention.
type BaselineRetriever struct {
	profiles      *memory.ProfileStore
	conversations *memory.ConversationStore
	config        *evolve.RetrieverConfig
}

// NewBaselineRetriever creates a retriever backed by the legacy stores.
func NewBaselineRetriever(profiles *memory.ProfileStore, conversations *memory.ConversationStore, config *evolve.RetrieverConfig) *BaselineRetriever {
	return &BaselineRetriever{
		profiles:      profiles,
		conversations: conversations,
		config:        config,
	}
}

// scoredEntry pairs an index entry with its relevance score.
type scoredEntry struct {
	entry memory.IndexEntry
	score float64
}

// Retrieve builds a context string containing the user profile and the most
// relevant conversation entries for the given task.
func (r *BaselineRetriever) Retrieve(ctx context.Context, query evolve.RetrievalQuery) (*evolve.RetrievalResult, error) {
	var sb strings.Builder

	profile, err := r.profiles.Load(query.UserID)
	if err == nil && profile != nil {
		sb.WriteString(profile.Render())
		sb.WriteString("\n\n")
	}

	maxResults := r.config.MaxResults
	if maxResults <= 0 {
		maxResults = 8
	}
	fallbackRecent := r.config.FallbackRecent
	if fallbackRecent <= 0 {
		fallbackRecent = 5
	}

	var entries []memory.IndexEntry
	if query.ContextID != "" {
		entries, err = r.conversations.GetRecentIndexedByContext(query.UserID, query.ContextID, 60)
	} else {
		entries, err = r.conversations.GetRecentIndexed(query.UserID, 60)
	}
	if err != nil {
		return &evolve.RetrievalResult{Context: sb.String(), TokenEstimate: len(sb.String()) / 4}, nil
	}

	if len(entries) == 0 {
		return &evolve.RetrievalResult{Context: sb.String(), TokenEstimate: len(sb.String()) / 4}, nil
	}

	taskKeywords := memory.ExtractKeywords(query.Task)
	taskTopics := memory.ExtractTopics(query.Task)

	var scoredEntries []scoredEntry
	for _, e := range entries {
		s := scoreEntryBaseline(e, taskKeywords, taskTopics, r.config.TopicBonus)
		scoredEntries = append(scoredEntries, scoredEntry{entry: e, score: s})
	}

	sort.Slice(scoredEntries, func(i, j int) bool {
		return scoredEntries[i].score > scoredEntries[j].score
	})

	var selected []scoredEntry
	if scoredEntries[0].score > 0 {
		limit := maxResults
		if limit > len(scoredEntries) {
			limit = len(scoredEntries)
		}
		selected = scoredEntries[:limit]
	} else {
		limit := fallbackRecent
		if limit > len(scoredEntries) {
			limit = len(scoredEntries)
		}
		selected = scoredEntries[:limit]
	}

	if len(selected) > 2 {
		selected = positionEdges(selected)
	}

	sb.WriteString("## Relevant History\n")
	for _, s := range selected {
		sb.WriteString(fmt.Sprintf("- [%s] %s\n", s.entry.Role, s.entry.Summary))
	}

	text := sb.String()
	return &evolve.RetrievalResult{
		Context:       text,
		TokenEstimate: len(text) / 4,
		UnitsUsed:     len(selected),
	}, nil
}

// scoreEntryBaseline computes a relevance score for an index entry against
// the task keywords and topics. The score is the fraction of task keywords
// found in the entry's keywords, plus an optional topic bonus.
func scoreEntryBaseline(entry memory.IndexEntry, taskKeywords, taskTopics []string, topicBonus float64) float64 {
	if len(taskKeywords) == 0 {
		return 0
	}
	overlap := 0
	for _, tk := range taskKeywords {
		for _, ek := range entry.Keywords {
			if strings.EqualFold(tk, ek) {
				overlap++
				break
			}
		}
	}
	score := float64(overlap) / float64(len(taskKeywords))

	if topicBonus <= 0 {
		topicBonus = 0.2
	}
	for _, tt := range taskTopics {
		for _, et := range entry.Topics {
			if tt == et {
				score += topicBonus
				goto topicDone
			}
		}
	}
topicDone:
	return score
}

// positionEdges interleaves items so the highest-scored entries land at the
// edges (start and end) of the list. This exploits the "lost in the middle"
// phenomenon where LLMs attend more to the beginning and end of context.
func positionEdges(items []scoredEntry) []scoredEntry {
	if len(items) <= 2 {
		return items
	}
	result := make([]scoredEntry, len(items))
	left, right := 0, len(items)-1
	for i, item := range items {
		if i%2 == 0 {
			result[left] = item
			left++
		} else {
			result[right] = item
			right--
		}
	}
	return result
}
