package evolve

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
)

// Pipeline orchestrates the four memory interfaces.
type Pipeline struct {
	encoder   Encoder
	storer    Storer
	retriever Retriever
	manager   Manager
	config    *PipelineConfig
}

// PipelineHolder manages the active pipeline with atomic swap for hot-reloading.
type PipelineHolder struct {
	active    atomic.Pointer[Pipeline]
	vaultPath string
	defaults  *PipelineConfig
}

// NewPipelineHolder creates a holder, loads evolved config, and builds the initial pipeline.
func NewPipelineHolder(vaultPath string, defaults *PipelineConfig, encoder Encoder, storer Storer, retriever Retriever, manager Manager) *PipelineHolder {
	h := &PipelineHolder{
		vaultPath: vaultPath,
		defaults:  defaults,
	}

	configPath := filepath.Join(vaultPath, "memory", "evolved", "pipeline-config.yaml")
	evolved, err := LoadPipelineConfig(configPath)
	if err != nil {
		slog.Warn("failed to load evolved pipeline config", "error", err)
	}
	cfg := MergeWithDefaults(defaults, evolved)

	p := &Pipeline{
		encoder:   encoder,
		storer:    storer,
		retriever: retriever,
		manager:   manager,
		config:    cfg,
	}
	h.active.Store(p)
	return h
}

// Get returns the current active pipeline.
func (h *PipelineHolder) Get() *Pipeline {
	return h.active.Load()
}

// Reload reads the evolved config and swaps in a new pipeline.
func (h *PipelineHolder) Reload() error {
	configPath := filepath.Join(h.vaultPath, "memory", "evolved", "pipeline-config.yaml")
	evolved, err := LoadPipelineConfig(configPath)
	if err != nil {
		return err
	}
	cfg := MergeWithDefaults(h.defaults, evolved)

	old := h.active.Load()
	p := &Pipeline{
		encoder:   old.encoder,
		storer:    old.storer,
		retriever: old.retriever,
		manager:   old.manager,
		config:    cfg,
	}
	h.active.Store(p)
	slog.Info("pipeline reloaded", "version", cfg.Version)
	return nil
}

// ConfigVersion returns the active config version.
func (h *PipelineHolder) ConfigVersion() string {
	p := h.active.Load()
	if p == nil || p.config == nil {
		return "0"
	}
	return strconv.Itoa(p.config.Version)
}

// Config returns the active pipeline config.
func (p *Pipeline) Config() *PipelineConfig {
	return p.config
}

// Retrieve delegates to the active retriever.
func (p *Pipeline) Retrieve(ctx context.Context, query RetrievalQuery) (*RetrievalResult, error) {
	return p.retriever.Retrieve(ctx, query)
}

// Encode delegates to the active encoder.
func (p *Pipeline) Encode(ctx context.Context, input EncoderInput) (*EncoderOutput, error) {
	return p.encoder.Encode(ctx, input)
}

// Store delegates to the active storer.
func (p *Pipeline) Store(ctx context.Context, units []MemoryUnit) error {
	return p.storer.Store(ctx, units)
}

// Compact delegates to the active manager.
func (p *Pipeline) Compact(ctx context.Context, input CompactionInput) (*CompactionResult, error) {
	return p.manager.Compact(ctx, input)
}

// EnsureEvolvedDirs creates the vault/memory/evolved/ directory structure.
func EnsureEvolvedDirs(vaultPath string) error {
	dirs := []string{
		filepath.Join(vaultPath, "memory", "evolved"),
		filepath.Join(vaultPath, "memory", "evolved", "metrics"),
		filepath.Join(vaultPath, "memory", "evolved", "snapshots"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0755); err != nil {
			return err
		}
	}
	return nil
}
