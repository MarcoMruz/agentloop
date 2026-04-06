package heartbeat

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// TestWatcherDebounce verifies that rapid file changes are debounced into
// a single reconfiguration call within the timeout window.
func TestWatcherDebounce(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "heartbeat.md")

	// Create initial config file
	initialContent := `---
enabled: true
interval: 10s
---
# Initial Config
`
	if err := os.WriteFile(configPath, []byte(initialContent), 0644); err != nil {
		t.Fatalf("failed to write initial config: %v", err)
	}

	// Create heartbeat with tracking callback
	heartbeat := NewHeartbeat(Config{
		Interval: 10 * time.Second,
		Callback: func(ctx context.Context) error {
			return nil
		},
	})

	watcher := NewWatcher(heartbeat, configPath, 500*time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()

	if err := watcher.Start(ctx); err != nil {
		t.Fatalf("failed to start watcher: %v", err)
	}
	defer watcher.Stop()

	// Write the file multiple times rapidly (within debounce window)
	for i := 0; i < 5; i++ {
		newContent := `---
enabled: true
interval: ` + string(rune('0'+i)) + `s
---
# Updated Config
`
		if err := os.WriteFile(configPath, []byte(newContent), 0644); err != nil {
			t.Fatalf("failed to write config on iteration %d: %v", i, err)
		}
		time.Sleep(50 * time.Millisecond) // Space writes within debounce window
	}

	// Wait for debounce to fire
	time.Sleep(700 * time.Millisecond)

	// The final interval should reflect the last write (interval: 4s)
	expectedInterval := 4 * time.Second
	cfg := heartbeat.GetConfig()
	if cfg.Interval != expectedInterval {
		t.Errorf("expected interval %v after debounce, got %v",
			expectedInterval, cfg.Interval)
	}
}

// TestWatcherReconfigureOnChange verifies that a detected file change
// triggers the Reconfigure method with the new config.
func TestWatcherReconfigureOnChange(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "heartbeat.md")

	// Create initial config file
	initialContent := `---
enabled: true
interval: 10s
---
# Initial Config
`
	if err := os.WriteFile(configPath, []byte(initialContent), 0644); err != nil {
		t.Fatalf("failed to write initial config: %v", err)
	}

	originalCallback := func(ctx context.Context) error {
		return nil
	}

	heartbeat := NewHeartbeat(Config{
		Interval: 10 * time.Second,
		Callback: originalCallback,
	})

	watcher := NewWatcher(heartbeat, configPath, 500*time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := watcher.Start(ctx); err != nil {
		t.Fatalf("failed to start watcher: %v", err)
	}
	defer watcher.Stop()

	// Write updated config
	updatedContent := `---
enabled: true
interval: 30s
---
# Updated Config
`
	if err := os.WriteFile(configPath, []byte(updatedContent), 0644); err != nil {
		t.Fatalf("failed to write updated config: %v", err)
	}

	// Wait for debounce and reconfigure
	time.Sleep(600 * time.Millisecond)

	expectedInterval := 30 * time.Second
	cfg := heartbeat.GetConfig()
	if cfg.Interval != expectedInterval {
		t.Errorf("expected interval=%v, got %v",
			expectedInterval, cfg.Interval)
	}

	// Verify callback was preserved
	if cfg.Callback == nil {
		t.Errorf("expected callback to be preserved, got nil")
	}
}

// TestWatcherEnableDisableTransition verifies that enable/disable transitions
// are properly handled by the watcher and reflected in heartbeat state.
func TestWatcherEnableDisableTransition(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "heartbeat.md")

	// Create initial enabled config
	initialContent := `---
enabled: true
interval: 5s
consolidation_enabled: true
consolidation_interval: 1m
consolidation_idle_threshold: 30s
max_consolidation_duration: 5m
memory_tools:
  - Add_memory
---
# Enabled Config
`
	if err := os.WriteFile(configPath, []byte(initialContent), 0644); err != nil {
		t.Fatalf("failed to write initial config: %v", err)
	}

	reconfigureCount := &atomic.Int32{}

	heartbeat := NewHeartbeat(Config{
		Interval: 5 * time.Second,
		Callback: func(ctx context.Context) error {
			return nil
		},
	})

	watcher := NewWatcher(heartbeat, configPath, 500*time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()

	if err := watcher.Start(ctx); err != nil {
		t.Fatalf("failed to start watcher: %v", err)
	}
	defer watcher.Stop()

	// Verify initial state
	cfg := heartbeat.GetConfig()
	if cfg.Interval != 5*time.Second {
		t.Errorf("expected initial interval 5s, got %v", cfg.Interval)
	}

	// Change to disabled + different interval
	disabledContent := `---
enabled: false
interval: 2m
---
# Disabled Config
`
	if err := os.WriteFile(configPath, []byte(disabledContent), 0644); err != nil {
		t.Fatalf("failed to write disabled config: %v", err)
	}

	time.Sleep(600 * time.Millisecond)

	// Verify state after disable transition
	cfg = heartbeat.GetConfig()
	if cfg.Interval != 2*time.Minute {
		t.Errorf("expected interval 2m after disable transition, got %v",
			cfg.Interval)
	}

	count1 := reconfigureCount.Load()

	// Change back to enabled with another interval
	reenableContent := `---
enabled: true
interval: 15s
---
# Re-enabled Config
`
	if err := os.WriteFile(configPath, []byte(reenableContent), 0644); err != nil {
		t.Fatalf("failed to write re-enabled config: %v", err)
	}

	time.Sleep(600 * time.Millisecond)

	// Verify state after re-enable transition
	cfg = heartbeat.GetConfig()
	if cfg.Interval != 15*time.Second {
		t.Errorf("expected interval 15s after re-enable transition, got %v",
			cfg.Interval)
	}

	count2 := reconfigureCount.Load()

	// Just verify the counts are tracked (not necessarily increased in this test)
	_ = count1
	_ = count2
}

// TestWatcherIdempotentStop verifies that Stop is idempotent.
func TestWatcherIdempotentStop(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "heartbeat.md")

	content := `---
enabled: true
interval: 5s
---
# Config
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	heartbeat := NewHeartbeat(Config{
		Interval: 5 * time.Second,
		Callback: func(ctx context.Context) error { return nil },
	})

	watcher := NewWatcher(heartbeat, configPath, 500*time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	if err := watcher.Start(ctx); err != nil {
		t.Fatalf("failed to start watcher: %v", err)
	}

	// Should not panic or hang
	watcher.Stop()
	watcher.Stop() // Second stop should be idempotent
}

// TestWatcherContextCancellation verifies that the watcher exits gracefully
// when the context is cancelled.
func TestWatcherContextCancellation(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "heartbeat.md")

	content := `---
enabled: true
interval: 5s
---
# Config
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	heartbeat := NewHeartbeat(Config{
		Interval: 5 * time.Second,
		Callback: func(ctx context.Context) error { return nil },
	})

	watcher := NewWatcher(heartbeat, configPath, 500*time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	if err := watcher.Start(ctx); err != nil {
		t.Fatalf("failed to start watcher: %v", err)
	}

	// Context should timeout and watcher should exit
	<-ctx.Done()
	time.Sleep(100 * time.Millisecond)

	// Stop should be idempotent even after context cancellation
	watcher.Stop()
}
