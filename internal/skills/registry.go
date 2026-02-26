package skills

import (
	"os"
	"path/filepath"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

type Skill struct {
	Name         string   `yaml:"name"`
	Description  string   `yaml:"description"`
	Triggers     []string `yaml:"triggers"` // keywords that activate this skill
	Instructions string   `yaml:"-"`        // body content (loaded separately)
}

type Registry struct {
	skills map[string]*Skill
	dirs   []string
	mu     sync.RWMutex
}

func NewRegistry(skillDirs []string) *Registry {
	r := &Registry{skills: make(map[string]*Skill), dirs: skillDirs}
	r.LoadAll()
	return r
}

// LoadAll scans skill directories for SKILL.md files.
// Format: YAML frontmatter (name, description, triggers) + markdown body (instructions).
func (r *Registry) LoadAll() {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, dir := range r.dirs {
		expanded := expandHome(dir)
		entries, err := os.ReadDir(expanded)
		if err != nil { continue }

		for _, e := range entries {
			if !e.IsDir() { continue }
			skillFile := filepath.Join(expanded, e.Name(), "SKILL.md")
			data, err := os.ReadFile(skillFile)
			if err != nil { continue }

			skill := parseSkillFile(string(data), e.Name())
			if skill != nil {
				r.skills[skill.Name] = skill
			}
		}
	}
}

func (r *Registry) Get(name string) (*Skill, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.skills[name]
	if !ok { return nil, os.ErrNotExist }
	return s, nil
}

func (r *Registry) List() []*Skill {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []*Skill
	for _, s := range r.skills { out = append(out, s) }
	return out
}

func parseSkillFile(content string, dirName string) *Skill {
	// Split YAML frontmatter from body
	if !strings.HasPrefix(content, "---") { return nil }
	parts := strings.SplitN(content[3:], "---", 2)
	if len(parts) < 2 { return nil }

	var skill Skill
	if err := yaml.Unmarshal([]byte(parts[0]), &skill); err != nil { return nil }
	skill.Instructions = strings.TrimSpace(parts[1])
	if skill.Name == "" { skill.Name = dirName }
	return &skill
}

func expandHome(p string) string {
	if strings.HasPrefix(p, "~/") {
		if h, err := os.UserHomeDir(); err == nil {
			return filepath.Join(h, p[2:])
		}
	}
	return p
}
