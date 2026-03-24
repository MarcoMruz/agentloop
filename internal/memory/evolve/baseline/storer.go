package baseline

import (
	"context"

	"github.com/MarcoMruz/agentloop/internal/memory"
	"github.com/MarcoMruz/agentloop/internal/memory/evolve"
	"github.com/MarcoMruz/agentloop/internal/memory/notes"
)

// BaselineStorer wraps ProfileStore and Engine into the evolve.Storer interface.
// Writes go through Engine.AddNote() to trigger bidirectional auto-linking.
type BaselineStorer struct {
	profiles *memory.ProfileStore
	engine   *memory.Engine
}

// NewBaselineStorer creates a storer that routes note writes through Engine.AddNote.
func NewBaselineStorer(profiles *memory.ProfileStore, engine *memory.Engine) *BaselineStorer {
	return &BaselineStorer{profiles: profiles, engine: engine}
}

// Store persists memory units. Profile-type units are skipped (profile updates
// happen as a side-effect of Encode). Conversation units are stored via
// Engine.AddNote so bidirectional auto-linking runs on every write.
func (s *BaselineStorer) Store(ctx context.Context, units []evolve.MemoryUnit) error {
	for _, u := range units {
		if u.Metadata["type"] == "profile" {
			continue
		}
		desc := u.Content
		if len(desc) > 120 {
			desc = desc[:117] + "..."
		}
		if _, err := s.engine.AddNote(notes.AtomicNote{
			UserID:      u.Metadata["userId"],
			Content:     u.Content,
			Keywords:    u.Keywords,
			Tags:        u.Topics,
			Description: desc,
		}); err != nil {
			return err
		}
	}
	return nil
}

// Load retrieves memory units for a user from the profile and NoteStore.
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
		ns := s.engine.NoteStore()
		if ns == nil {
			return units, nil
		}
		noteList, err := ns.ListByUser(userId)
		if err != nil {
			return units, err
		}
		for i, n := range noteList {
			if i >= maxItems {
				break
			}
			units = append(units, evolve.MemoryUnit{
				ID:        n.ID,
				Timestamp: n.CreatedAt,
				Role:      "user",
				Content:   n.Content,
				Keywords:  n.Keywords,
				Topics:    n.Tags,
				Metadata: map[string]string{
					"type": "conversation",
				},
			})
		}
	}

	return units, nil
}
