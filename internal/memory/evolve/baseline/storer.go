package baseline

import (
	"context"

	"github.com/MarcoMruz/agentloop/internal/memory"
	"github.com/MarcoMruz/agentloop/internal/memory/evolve"
)

// BaselineStorer wraps ProfileStore and ConversationStore into the
// evolve.Storer interface, preserving the existing storage format.
type BaselineStorer struct {
	profiles      *memory.ProfileStore
	conversations *memory.ConversationStore
}

// NewBaselineStorer creates a storer backed by the legacy stores.
func NewBaselineStorer(profiles *memory.ProfileStore, conversations *memory.ConversationStore) *BaselineStorer {
	return &BaselineStorer{profiles: profiles, conversations: conversations}
}

// Store persists memory units. Profile-type units are skipped (profile updates
// happen as a side-effect of Encode). Conversation units are appended via
// ConversationStore.Append.
func (s *BaselineStorer) Store(ctx context.Context, units []evolve.MemoryUnit) error {
	for _, u := range units {
		if u.Metadata["type"] == "profile" {
			continue
		}
		contextID := u.Metadata["contextID"]
		if err := s.conversations.Append(u.Metadata["userId"], u.Role, u.Content, contextID); err != nil {
			return err
		}
	}
	return nil
}

// Load retrieves memory units for a user. It assembles units from profiles
// and/or conversation index entries depending on the filter.
func (s *BaselineStorer) Load(ctx context.Context, userId string, filter evolve.StoreFilter) ([]evolve.MemoryUnit, error) {
	maxItems := filter.MaxItems
	if maxItems <= 0 {
		maxItems = 60
	}

	var units []evolve.MemoryUnit

	if filter.Type == "profile" || filter.Type == "all" || filter.Type == "" {
		profile, err := s.profiles.Load(userId)
		if err == nil && profile != nil {
			units = append(units, evolve.MemoryUnit{
				Role:    "system",
				Content: profile.Render(),
				Metadata: map[string]string{
					"type": "profile",
				},
			})
		}
	}

	if filter.Type == "conversation" || filter.Type == "all" || filter.Type == "" {
		var entries []memory.IndexEntry
		var err error

		if filter.ContextID != "" {
			entries, err = s.conversations.GetRecentIndexedByContext(userId, filter.ContextID, maxItems)
		} else {
			entries, err = s.conversations.GetRecentIndexed(userId, maxItems)
		}
		if err != nil {
			return units, err
		}

		for _, e := range entries {
			units = append(units, evolve.MemoryUnit{
				Role:     e.Role,
				Content:  e.Summary,
				Keywords: e.Keywords,
				Topics:   e.Topics,
				Metadata: map[string]string{
					"type":      "conversation",
					"contextID": e.ConversationContextID,
				},
			})
		}
	}

	return units, nil
}
