package baseline

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/MarcoMruz/agentloop/internal/memory"
	"github.com/MarcoMruz/agentloop/internal/memory/evolve"
	"github.com/MarcoMruz/agentloop/internal/memory/notes"
)

// BaselineRetriever implements evolve.Retriever using dual-mode retrieval:
// keyword (Jaccard) search combined with vector search when embeddings are
// available. Results from both paths are merged, re-ranked by Jaccard score,
// and positioned using "edges" layout for optimal LLM attention.
type BaselineRetriever struct {
	profiles *memory.ProfileStore
	notes    *notes.SQLiteNoteStore
	config   *evolve.RetrieverConfig
}

// NewBaselineRetriever creates a retriever backed by SQLiteNoteStore.
func NewBaselineRetriever(profiles *memory.ProfileStore, store *notes.SQLiteNoteStore, config *evolve.RetrieverConfig) *BaselineRetriever {
	return &BaselineRetriever{
		profiles: profiles,
		notes:    store,
		config:   config,
	}
}

// scoredNote pairs a note with its relevance score.
type scoredNote struct {
	note  notes.AtomicNote
	score float64
}

// Retrieve builds a context string from the user profile and the most
// relevant atomic notes for the given task. Uses vector search when
// query.QueryEmbedding is provided, keyword search otherwise, merging
// both result sets when both paths are active.
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

	taskKeywords := memory.ExtractKeywords(query.Task)
	taskTopics := memory.ExtractTopics(query.Task)

	// Collect candidates from both search paths, deduplicated by ID.
	seen := make(map[string]struct{})
	var candidates []notes.AtomicNote

	addUnique := func(ns []notes.AtomicNote) {
		for _, n := range ns {
			if _, ok := seen[n.ID]; !ok {
				seen[n.ID] = struct{}{}
				candidates = append(candidates, n)
			}
		}
	}

	// Vector search when the caller has pre-computed a query embedding.
	if len(query.QueryEmbedding) > 0 {
		vecResults, _ := r.notes.SearchByVector(query.UserID, query.QueryEmbedding, maxResults*5)
		addUnique(vecResults)
	}

	// Keyword search.
	if len(taskKeywords) > 0 {
		kwResults, _ := r.notes.SearchByKeywords(query.UserID, taskKeywords, maxResults*5)
		addUnique(kwResults)
	}

	// Fallback to most-recent when both searches return nothing.
	if len(candidates) == 0 {
		all, _ := r.notes.ListByUser(query.UserID)
		limit := fallbackRecent
		if limit > len(all) {
			limit = len(all)
		}
		candidates = all[:limit]
	}

	if len(candidates) == 0 {
		return &evolve.RetrievalResult{Context: sb.String(), TokenEstimate: len(sb.String()) / 4}, nil
	}

	// Re-rank all candidates with Jaccard scoring (keyword overlap + topic bonus).
	scored := make([]scoredNote, len(candidates))
	for i, n := range candidates {
		scored[i] = scoredNote{note: n, score: scoreNote(n, taskKeywords, taskTopics, r.config.TopicBonus)}
	}

	sort.Slice(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})

	limit := maxResults
	if limit > len(scored) {
		limit = len(scored)
	}
	selected := scored[:limit]

	if len(selected) > 2 {
		selected = positionEdgesNotes(selected)
	}

	sb.WriteString("## Relevant Notes\n")
	for _, s := range selected {
		label := strings.Join(s.note.Tags, ", ")
		if label == "" {
			label = "note"
		}
		sb.WriteString(fmt.Sprintf("- [%s] %s\n", label, s.note.Description))
	}

	text := sb.String()
	return &evolve.RetrievalResult{
		Context:       text,
		TokenEstimate: len(text) / 4,
		UnitsUsed:     len(selected),
	}, nil
}

// scoreNote computes a Jaccard-style relevance score for a note against
// task keywords and topics. Tags on the note are treated as topics.
func scoreNote(n notes.AtomicNote, taskKeywords, taskTopics []string, topicBonus float64) float64 {
	if len(taskKeywords) == 0 {
		return 0
	}
	overlap := 0
	for _, tk := range taskKeywords {
		for _, nk := range n.Keywords {
			if strings.EqualFold(tk, nk) {
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
		for _, nt := range n.Tags {
			if tt == nt {
				score += topicBonus
				goto topicDone
			}
		}
	}
topicDone:
	return score
}

// positionEdgesNotes interleaves items so the highest-scored notes land at the
// edges (start and end) of the list. This exploits the "lost in the middle"
// phenomenon where LLMs attend more to the beginning and end of context.
func positionEdgesNotes(items []scoredNote) []scoredNote {
	if len(items) <= 2 {
		return items
	}
	result := make([]scoredNote, len(items))
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
