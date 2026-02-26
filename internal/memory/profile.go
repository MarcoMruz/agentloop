package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

type UserProfile struct {
	UserID             string            `yaml:"user_id"`
	Name               string            `yaml:"name"`
	CommunicationStyle string            `yaml:"communication_style"`
	Preferences        map[string]string `yaml:"preferences"`
	FrequentProjects   []string          `yaml:"frequent_projects"`
	ToolPreferences    []string          `yaml:"tool_preferences"`
	FactSheet          map[string]string `yaml:"fact_sheet"`
	RecentTopics       []string          `yaml:"recent_topics"`
	LastUpdated        string            `yaml:"last_updated"`
}

func DefaultProfile(userId string) *UserProfile {
	return &UserProfile{
		UserID:             userId,
		CommunicationStyle: "concise, technical",
		Preferences:        map[string]string{},
		FactSheet:          map[string]string{},
	}
}

// Render produces the text representation injected into prompts.
// Designed for stability (changes rarely → maximizes prompt cache hits).
func (p *UserProfile) Render() string {
	var sb strings.Builder
	sb.WriteString("## User Profile\n")
	if p.Name != "" { sb.WriteString(fmt.Sprintf("Name: %s\n", p.Name)) }
	if p.CommunicationStyle != "" { sb.WriteString(fmt.Sprintf("Style: %s\n", p.CommunicationStyle)) }
	if len(p.FrequentProjects) > 0 {
		sb.WriteString(fmt.Sprintf("Projects: %s\n", strings.Join(p.FrequentProjects, ", ")))
	}
	for k, v := range p.Preferences {
		sb.WriteString(fmt.Sprintf("Pref: %s = %s\n", k, v))
	}
	for k, v := range p.FactSheet {
		sb.WriteString(fmt.Sprintf("Fact: %s: %s\n", k, v))
	}
	return sb.String()
}

type ProfileStore struct {
	vaultPath string
	mu        sync.Mutex
}

func NewProfileStore(vaultPath string) *ProfileStore {
	return &ProfileStore{vaultPath: vaultPath}
}

func (ps *ProfileStore) path(userId string) string {
	return filepath.Join(ps.vaultPath, "memory", "users", userId+".yaml")
}

func (ps *ProfileStore) Load(userId string) (*UserProfile, error) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	data, err := os.ReadFile(ps.path(userId))
	if err != nil { return nil, err }
	var p UserProfile
	if err := yaml.Unmarshal(data, &p); err != nil { return nil, err }
	return &p, nil
}

func (ps *ProfileStore) Save(p *UserProfile) error {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	p.LastUpdated = time.Now().Format(time.RFC3339)
	data, err := yaml.Marshal(p)
	if err != nil { return err }
	dir := filepath.Dir(ps.path(p.UserID))
	os.MkdirAll(dir, 0755)
	return os.WriteFile(ps.path(p.UserID), data, 0644)
}

func (ps *ProfileStore) SetFact(userId string, key string, value string) error {
	p, err := ps.Load(userId)
	if err != nil { p = DefaultProfile(userId) }
	if p.FactSheet == nil { p.FactSheet = map[string]string{} }
	p.FactSheet[key] = value
	return ps.Save(p)
}

func (ps *ProfileStore) DeleteFact(userId string, key string) error {
	p, err := ps.Load(userId)
	if err != nil { return err }
	delete(p.FactSheet, key)
	return ps.Save(p)
}

// UpdateFromInteraction extracts patterns from a conversation turn.
// Uses heuristics (no LLM call) — fast, free, runs after every task.
func (ps *ProfileStore) UpdateFromInteraction(userId string, userMsg string, toolsUsed []string) {
	p, err := ps.Load(userId)
	if err != nil { p = DefaultProfile(userId) }

	// Extract project paths
	for _, word := range strings.Fields(userMsg) {
		if strings.HasPrefix(word, "~/") && len(word) > 3 {
			if !contains(p.FrequentProjects, word) {
				p.FrequentProjects = append(p.FrequentProjects, word)
				if len(p.FrequentProjects) > 10 {
					p.FrequentProjects = p.FrequentProjects[1:]
				}
			}
		}
	}

	// Track topic
	words := strings.Fields(userMsg)
	topicLen := min(8, len(words))
	topic := strings.Join(words[:topicLen], " ")
	p.RecentTopics = append(p.RecentTopics, topic)
	if len(p.RecentTopics) > 20 { p.RecentTopics = p.RecentTopics[1:] }

	ps.Save(p)
}

func contains(ss []string, s string) bool {
	for _, v := range ss { if v == s { return true } }
	return false
}

func min(a, b int) int {
	if a < b { return a }
	return b
}
