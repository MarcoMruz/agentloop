package evolve

import (
	"os"

	"gopkg.in/yaml.v3"
)

// PipelineConfig is the declarative configuration for the memory pipeline.
type PipelineConfig struct {
	Version     int             `yaml:"version"`
	Updated     string          `yaml:"updated"`
	EvolvedFrom string          `yaml:"evolved_from"`
	Encoder     EncoderConfig   `yaml:"encoder"`
	Retriever   RetrieverConfig `yaml:"retriever"`
	Manager     ManagerConfig   `yaml:"manager"`
	Storer      StorerConfig    `yaml:"storer"`
}

type EncoderConfig struct {
	Strategy                string   `yaml:"strategy"`
	KeywordLimit            int      `yaml:"keyword_limit"`
	TopicTaxonomyExtensions []string `yaml:"topic_taxonomy_extensions"`
	ExtractToolPatterns     bool     `yaml:"extract_tool_patterns"`
}

type RetrieverConfig struct {
	Strategy       string  `yaml:"strategy"`
	MaxResults     int     `yaml:"max_results"`
	FallbackRecent int     `yaml:"fallback_recent"`
	TopicBonus     float64 `yaml:"topic_bonus"`
	RecencyWeight  float64 `yaml:"recency_weight"`
	Position       string  `yaml:"position"`
}

type ManagerConfig struct {
	Strategy          string   `yaml:"strategy"`
	RollingKeepRecent int      `yaml:"rolling_keep_recent"`
	FactsMax          int      `yaml:"facts_max"`
	FactsKeywords     []string `yaml:"facts_keywords"`
	AggressiveFilter  bool     `yaml:"aggressive_filter"`
}

type StorerConfig struct {
	Format           string `yaml:"format"`
	IndexSidecar     bool   `yaml:"index_sidecar"`
	ContextIsolation bool   `yaml:"context_isolation"`
}

// DefaultPipelineConfig returns baseline configuration matching existing behavior.
func DefaultPipelineConfig() *PipelineConfig {
	return &PipelineConfig{
		Version: 1,
		Encoder: EncoderConfig{
			Strategy:     "baseline",
			KeywordLimit: 15,
		},
		Retriever: RetrieverConfig{
			Strategy:       "jaccard",
			MaxResults:     8,
			FallbackRecent: 5,
			TopicBonus:     0.2,
			Position:       "edges",
		},
		Manager: ManagerConfig{
			Strategy:          "rolling",
			RollingKeepRecent: 5,
			FactsMax:          30,
			FactsKeywords: []string{
				"decided", "confirmed", "created", "fixed", "deployed",
				"merged", "installed", "configured", "updated", "deleted",
				"error", "failed",
			},
		},
		Storer: StorerConfig{
			Format:           "markdown",
			IndexSidecar:     true,
			ContextIsolation: true,
		},
	}
}

// LoadPipelineConfig reads a pipeline config from YAML. Returns nil, nil if file missing.
func LoadPipelineConfig(path string) (*PipelineConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var cfg PipelineConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// SavePipelineConfig writes pipeline config to YAML.
func SavePipelineConfig(path string, cfg *PipelineConfig) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// MergeWithDefaults overlays evolved config onto defaults.
// Non-zero evolved values override defaults. Position is always forced to "edges".
func MergeWithDefaults(defaults, evolved *PipelineConfig) *PipelineConfig {
	merged := *defaults

	if evolved == nil {
		return &merged
	}

	// Encoder
	if evolved.Encoder.Strategy != "" {
		merged.Encoder.Strategy = evolved.Encoder.Strategy
	}
	if evolved.Encoder.KeywordLimit != 0 {
		merged.Encoder.KeywordLimit = evolved.Encoder.KeywordLimit
	}
	if len(evolved.Encoder.TopicTaxonomyExtensions) > 0 {
		merged.Encoder.TopicTaxonomyExtensions = evolved.Encoder.TopicTaxonomyExtensions
	}
	if evolved.Encoder.ExtractToolPatterns {
		merged.Encoder.ExtractToolPatterns = true
	}

	// Retriever
	if evolved.Retriever.Strategy != "" {
		merged.Retriever.Strategy = evolved.Retriever.Strategy
	}
	if evolved.Retriever.MaxResults != 0 {
		merged.Retriever.MaxResults = evolved.Retriever.MaxResults
	}
	if evolved.Retriever.FallbackRecent != 0 {
		merged.Retriever.FallbackRecent = evolved.Retriever.FallbackRecent
	}
	if evolved.Retriever.TopicBonus != 0 {
		merged.Retriever.TopicBonus = evolved.Retriever.TopicBonus
	}
	if evolved.Retriever.RecencyWeight != 0 {
		merged.Retriever.RecencyWeight = evolved.Retriever.RecencyWeight
	}
	// Position is always "edges" — hard constraint, not overridable
	merged.Retriever.Position = "edges"

	// Manager
	if evolved.Manager.Strategy != "" {
		merged.Manager.Strategy = evolved.Manager.Strategy
	}
	if evolved.Manager.RollingKeepRecent != 0 {
		merged.Manager.RollingKeepRecent = evolved.Manager.RollingKeepRecent
	}
	if evolved.Manager.FactsMax != 0 {
		merged.Manager.FactsMax = evolved.Manager.FactsMax
	}
	if len(evolved.Manager.FactsKeywords) > 0 {
		merged.Manager.FactsKeywords = evolved.Manager.FactsKeywords
	}
	merged.Manager.AggressiveFilter = evolved.Manager.AggressiveFilter

	// Storer
	if evolved.Storer.Format != "" {
		merged.Storer.Format = evolved.Storer.Format
	}
	merged.Storer.IndexSidecar = evolved.Storer.IndexSidecar
	merged.Storer.ContextIsolation = evolved.Storer.ContextIsolation

	// Metadata
	if evolved.Version != 0 {
		merged.Version = evolved.Version
	}
	if evolved.Updated != "" {
		merged.Updated = evolved.Updated
	}
	if evolved.EvolvedFrom != "" {
		merged.EvolvedFrom = evolved.EvolvedFrom
	}

	return &merged
}
