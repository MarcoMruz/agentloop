package heartbeat

import (
	"context"
	"log/slog"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// Config holds configuration for a Heartbeat.
type Config struct {
	Interval time.Duration
	Callback func(context.Context) error
}

// Heartbeat is a time.Ticker-based periodic task runner with panic recovery,
// idle tracking, and mutex-protected state management.
type Heartbeat struct {
	mu           sync.Mutex
	config       Config
	running      bool
	stopCh       chan struct{}
	wg           sync.WaitGroup      // Tracks the running goroutine
	ctx          context.Context     // Stored context for Reconfigure
	cancel       context.CancelFunc  // Cancel func for stored context
	lastActivity atomic.Int64        // Unix nanoseconds
}

// NewHeartbeat creates a new Heartbeat with the given config.
func NewHeartbeat(cfg Config) *Heartbeat {
	h := &Heartbeat{
		config: cfg,
	}
	// Initialize lastActivity to now
	h.lastActivity.Store(time.Now().UnixNano())
	return h
}

// Start begins the heartbeat ticker goroutine. If already running, it's a no-op.
func (h *Heartbeat) Start(ctx context.Context) {
	h.mu.Lock()

	if h.running {
		h.mu.Unlock()
		return // Already running
	}

	h.running = true
	h.stopCh = make(chan struct{})
	// Capture config under lock before spawning goroutine
	config := h.config
	// Store context for Reconfigure to use
	h.ctx = ctx
	if h.cancel != nil {
		h.cancel() // Cancel old context if any
	}

	h.wg.Add(1)
	h.mu.Unlock()

	go h.run(ctx, config)
}

// run is the main loop that ticks at the configured interval.
// config is captured at start time, so no synchronization needed.
func (h *Heartbeat) run(ctx context.Context, config Config) {
	defer h.wg.Done()

	ticker := time.NewTicker(config.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// Execute callback with panic recovery
			h.executeWithRecovery(ctx, config)
		case <-h.stopCh:
			// Graceful shutdown
			return
		case <-ctx.Done():
			// Context cancelled
			return
		}
	}
}

// executeWithRecovery runs the callback with panic recovery.
func (h *Heartbeat) executeWithRecovery(ctx context.Context, config Config) {
	defer func() {
		if r := recover(); r != nil {
			// Panic occurred but we continue; the heartbeat is resilient.
			// Capture stack trace and log it.
			buf := make([]byte, 4096)
			n := runtime.Stack(buf, false)
			slog.Error("heartbeat tick panic recovered", "panic", r, "stack", string(buf[:n]))
		}
	}()

	if config.Callback != nil {
		_ = config.Callback(ctx)
	}
}

// Stop gracefully stops the heartbeat ticker. Idempotent.
func (h *Heartbeat) Stop() {
	h.mu.Lock()

	if !h.running {
		h.mu.Unlock()
		return // Already stopped
	}

	h.running = false
	close(h.stopCh)
	h.mu.Unlock()

	// Wait for the goroutine to fully exit
	h.wg.Wait()
}

// Reconfigure updates the heartbeat config and restarts if it was running.
// Uses the stored context from Start.
func (h *Heartbeat) Reconfigure(newConfig Config) {
	h.mu.Lock()
	wasRunning := h.running
	storedCtx := h.ctx

	// Stop current heartbeat if running
	if h.running {
		h.running = false
		close(h.stopCh)
	}

	// Update config
	h.config = newConfig

	h.mu.Unlock()

	// Wait for the goroutine to fully exit before starting a new one
	if wasRunning {
		h.wg.Wait()
		// Restart with stored context
		if storedCtx != nil {
			h.Start(storedCtx)
		}
	}
}

// ReconfigureAndStart updates the config and restarts with an explicit context.
// Use this when you want to provide a fresh context, overriding the stored one.
func (h *Heartbeat) ReconfigureAndStart(ctx context.Context, newConfig Config) {
	h.mu.Lock()
	wasRunning := h.running

	// Stop current heartbeat
	if h.running {
		h.running = false
		close(h.stopCh)
	}

	// Update config
	h.config = newConfig

	// Store new context
	h.ctx = ctx
	if h.cancel != nil {
		h.cancel()
	}

	h.mu.Unlock()

	// Wait for the goroutine to fully exit before starting a new one
	if wasRunning {
		h.wg.Wait()
		h.Start(ctx)
	}
}

// IsRunning returns whether the heartbeat is currently running.
func (h *Heartbeat) IsRunning() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.running
}

// RecordActivity updates the last activity timestamp to now.
func (h *Heartbeat) RecordActivity() {
	h.lastActivity.Store(time.Now().UnixNano())
}

// LastActivity returns the timestamp of the last recorded activity.
func (h *Heartbeat) LastActivity() time.Time {
	nanos := h.lastActivity.Load()
	return time.Unix(0, nanos)
}

// IdleDuration returns the duration since the last activity.
func (h *Heartbeat) IdleDuration() time.Duration {
	return time.Since(h.LastActivity())
}

// GetConfig returns the current config (for testing/diagnostics).
func (h *Heartbeat) GetConfig() Config {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.config
}
