// Package evolve defines the interfaces and types for AgentLoop's
// self-evolving memory architecture (MemEvolve).
package evolve

import (
	"context"
	"time"
)

// Encoder transforms raw interactions into structured memory units.
type Encoder interface {
	Encode(ctx context.Context, input EncoderInput) (*EncoderOutput, error)
}

// Storer persists and loads memory units.
type Storer interface {
	Store(ctx context.Context, units []MemoryUnit) error
	Load(ctx context.Context, userId string, filter StoreFilter) ([]MemoryUnit, error)
}

// Retriever selects relevant memory for a given task context.
type Retriever interface {
	Retrieve(ctx context.Context, query RetrievalQuery) (*RetrievalResult, error)
}

// Manager consolidates and prunes stored memory under a token budget.
type Manager interface {
	Compact(ctx context.Context, input CompactionInput) (*CompactionResult, error)
}

// MemoryUnit is the universal exchange type between interfaces.
type MemoryUnit struct {
	ID        string
	Timestamp time.Time
	Role      string            // "user", "assistant", "system"
	Content   string
	Keywords  []string
	Topics    []string
	Metadata  map[string]string // Extensible: contextID, source, toolsUsed, type
	Score     float64           // Set by Retriever during scoring
}

// EncoderInput holds raw interaction data for encoding.
type EncoderInput struct {
	UserID                string
	UserMessage           string
	AgentReply            string
	ToolsUsed             []string
	ConversationContextID string
}

// EncoderOutput holds the encoded memory units.
type EncoderOutput struct {
	Units []MemoryUnit
}

// StoreFilter controls what Load() returns.
type StoreFilter struct {
	Type      string // "profile", "conversation", "all"
	ContextID string // Filter by conversation context ID (empty = no filter)
	MaxItems  int
}

// RetrievalQuery describes what memory to retrieve.
type RetrievalQuery struct {
	UserID         string
	Task           string
	ContextID      string    // For thread-scoped retrieval
	MaxTokens      int
	QueryEmbedding []float32 // Optional: when non-nil, enables vector search in SQLiteNoteStore
}

// RetrievalResult holds retrieved memory formatted for prompt injection.
type RetrievalResult struct {
	Context       string // Assembled text for <memory> block
	TokenEstimate int
	UnitsUsed     int
}

// CompactionInput holds data for compaction.
type CompactionInput struct {
	Text      string
	MaxTokens int
	Strategy  string
}

// CompactionResult from the Manager.
type CompactionResult struct {
	Text           string
	TokenEstimate  int
	EntriesRemoved int
}
