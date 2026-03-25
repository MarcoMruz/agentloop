package skills

import (
	"os"
	"path/filepath"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

// SkillFile represents a non-SKILL.md file bundled with a skill.
type SkillFile struct {
	Name        string `yaml:"name"                  json:"name"`
	Path        string `yaml:"-"                     json:"path"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
	Type        string `yaml:"-"                     json:"type"` // file extension without dot; "" for files with no extension
}

// Skill represents a loaded skill with its metadata and associated files.
type Skill struct {
	Name         string      `yaml:"name"`
	Description  string      `yaml:"description"`
	Tags         []string    `yaml:"tags"`
	Files        []SkillFile `yaml:"files,omitempty"`
	Instructions string      `yaml:"-" json:"instructions"`
	Dir          string      `yaml:"-" json:"dir"`
}

// SkillCatalogEntry is a compact representation used by the SkillAgent for skill selection.
type SkillCatalogEntry struct {
	Name        string   `json:"name"        yaml:"name"`
	Description string   `json:"description" yaml:"description"`
	Tags        []string `json:"tags"        yaml:"tags"`
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
// Format: YAML frontmatter (name, description, tags) + markdown body (instructions).
func (r *Registry) LoadAll() {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, dir := range r.dirs {
		expanded := expandHome(dir)
		entries, err := os.ReadDir(expanded)
		if err != nil {
			continue
		}

		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			skillDir := filepath.Join(expanded, e.Name())
			skillFile := filepath.Join(skillDir, "SKILL.md")
			data, err := os.ReadFile(skillFile)
			if err != nil {
				continue
			}

			skill := parseSkillFile(string(data), e.Name())
			if skill != nil {
				skill.Dir = skillDir
				skill.Files = scanSkillFiles(skillDir, skill.Files)
				r.skills[skill.Name] = skill
			}
		}
	}
}

func (r *Registry) Get(name string) (*Skill, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.skills[name]
	if !ok {
		return nil, os.ErrNotExist
	}
	return s, nil
}

func (r *Registry) List() []*Skill {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []*Skill
	for _, s := range r.skills {
		out = append(out, s)
	}
	return out
}

// Catalog returns a compact catalog of all loaded skills (name + description + tags only).
func (r *Registry) Catalog() []SkillCatalogEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	entries := make([]SkillCatalogEntry, 0, len(r.skills))
	for _, s := range r.skills {
		entries = append(entries, SkillCatalogEntry{
			Name:        s.Name,
			Description: s.Description,
			Tags:        s.Tags,
		})
	}
	return entries
}

// scanSkillFiles reads all non-SKILL.md, non-hidden files in skillDir and returns SkillFile entries.
// It merges descriptions from the manifest (frontmatter files list) when names match.
func scanSkillFiles(skillDir string, manifest []SkillFile) []SkillFile {
	// Build a lookup map from the frontmatter manifest by name.
	manifestDesc := make(map[string]string, len(manifest))
	for _, f := range manifest {
		manifestDesc[f.Name] = f.Description
	}

	dirEntries, err := os.ReadDir(skillDir)
	if err != nil {
		return nil
	}

	var files []SkillFile
	for _, entry := range dirEntries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		// Skip SKILL.md itself and hidden files.
		if name == "SKILL.md" || strings.HasPrefix(name, ".") {
			continue
		}
		ext := filepath.Ext(name)
		fileType := strings.TrimPrefix(ext, ".")

		sf := SkillFile{
			Name: name,
			Path: filepath.Join(skillDir, name),
			Type: fileType,
		}
		if desc, ok := manifestDesc[name]; ok {
			sf.Description = desc
		}
		files = append(files, sf)
	}
	return files
}

func parseSkillFile(content string, dirName string) *Skill {
	// Split YAML frontmatter from body
	if !strings.HasPrefix(content, "---") {
		return nil
	}
	parts := strings.SplitN(content[3:], "---", 2)
	if len(parts) < 2 {
		return nil
	}

	var skill Skill
	if err := yaml.Unmarshal([]byte(parts[0]), &skill); err != nil {
		return nil
	}
	skill.Instructions = strings.TrimSpace(parts[1])
	if skill.Name == "" {
		skill.Name = dirName
	}
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
