package heartbeat

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestHeartbeatStartStop tests that Start and Stop work correctly.
func TestHeartbeatStartStop(t *testing.T) {
	cfg := Config{
		Interval: 10 * time.Millisecond,
		Callback: func(ctx context.Context) error {
			return nil
		},
	}

	h := NewHeartbeat(cfg)

	if h.IsRunning() {
		t.Fatal("heartbeat should not be running initially")
	}

	ctx := context.Background()
	h.Start(ctx)

	if !h.IsRunning() {
		t.Fatal("heartbeat should be running after Start")
	}

	h.Stop()

	if h.IsRunning() {
		t.Fatal("heartbeat should not be running after Stop")
	}
}

// TestHeartbeatTickFires verifies the callback fires at the configured interval.
func TestHeartbeatTickFires(t *testing.T) {
	callCount := atomic.Int32{}

	cfg := Config{
		Interval: 10 * time.Millisecond,
		Callback: func(ctx context.Context) error {
			callCount.Add(1)
			return nil
		},
	}

	h := NewHeartbeat(cfg)
	ctx := context.Background()
	h.Start(ctx)

	// Let it run for ~100ms, should fire roughly 10 times
	time.Sleep(100 * time.Millisecond)
	h.Stop()

	count := callCount.Load()
	if count < 7 || count > 15 {
		t.Fatalf("callback should have fired ~10 times, got %d", count)
	}
}

// testHandler is a custom handler that captures log records for testing.
type testHandler struct {
	mu      sync.Mutex
	records []struct {
		level   slog.Level
		message string
		attrs   map[string]interface{}
	}
}

func (h *testHandler) Handle(ctx context.Context, record slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	attrs := make(map[string]interface{})
	record.Attrs(func(a slog.Attr) bool {
		attrs[a.Key] = a.Value.String()
		return true
	})

	h.records = append(h.records, struct {
		level   slog.Level
		message string
		attrs   map[string]interface{}
	}{
		level:   record.Level,
		message: record.Message,
		attrs:   attrs,
	})
	return nil
}

func (h *testHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return h
}

func (h *testHandler) WithGroup(name string) slog.Handler {
	return h
}

func (h *testHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return true
}

// TestHeartbeatPanicRecovery verifies the heartbeat recovers from panics in the callback.
func TestHeartbeatPanicRecovery(t *testing.T) {
	callCount := atomic.Int32{}
	panicOnCall := int32(2) // Panic on the 2nd call

	// Create a custom handler to capture log records
	handler := &testHandler{}

	// Replace the default logger with our custom one
	oldLogger := slog.Default()
	slog.SetDefault(slog.New(handler))
	defer slog.SetDefault(oldLogger)

	cfg := Config{
		Interval: 10 * time.Millisecond,
		Callback: func(ctx context.Context) error {
			count := callCount.Add(1)
			if count == panicOnCall {
				panic("intentional panic")
			}
			return nil
		},
	}

	h := NewHeartbeat(cfg)
	ctx := context.Background()
	h.Start(ctx)

	// Let it run long enough to hit the panic and continue
	time.Sleep(100 * time.Millisecond)
	h.Stop()

	count := callCount.Load()
	if count < 7 {
		t.Fatalf("callback should have fired multiple times after panic, got %d", count)
	}

	// Verify panic was logged
	handler.mu.Lock()
	panicLogged := false
	for _, record := range handler.records {
		if record.message == "heartbeat tick panic recovered" {
			if _, hasPanic := record.attrs["panic"]; hasPanic {
				panicLogged = true
				break
			}
		}
	}
	handler.mu.Unlock()

	if !panicLogged {
		t.Fatal("panic should have been logged with 'panic' key")
	}
}

// TestHeartbeatReconfigure verifies that Reconfigure updates the config and restarts if needed.
func TestHeartbeatReconfigure(t *testing.T) {
	callCount1 := atomic.Int32{}
	callCount2 := atomic.Int32{}

	cfg1 := Config{
		Interval: 10 * time.Millisecond,
		Callback: func(ctx context.Context) error {
			callCount1.Add(1)
			return nil
		},
	}

	cfg2 := Config{
		Interval: 20 * time.Millisecond,
		Callback: func(ctx context.Context) error {
			callCount2.Add(1)
			return nil
		},
	}

	h := NewHeartbeat(cfg1)
	ctx := context.Background()

	// Start with first config
	h.Start(ctx)
	time.Sleep(50 * time.Millisecond)

	count1Before := callCount1.Load()

	// Reconfigure with second config and restart
	h.ReconfigureAndStart(ctx, cfg2)
	time.Sleep(50 * time.Millisecond)

	h.Stop()

	count1After := callCount1.Load()
	count2 := callCount2.Load()

	// First callback should mostly stop firing, second should start
	if count2 == 0 {
		t.Fatalf("second callback should have fired after reconfigure, got 0")
	}

	// First callback should have stopped or nearly stopped (allow 1 extra due to timing)
	if count1After > count1Before+1 {
		t.Fatalf("first callback should have stopped after reconfigure, got count increase from %d to %d", count1Before, count1After)
	}
}

// TestHeartbeatIdempotentStop verifies that calling Stop multiple times is safe.
func TestHeartbeatIdempotentStop(t *testing.T) {
	cfg := Config{
		Interval: 10 * time.Millisecond,
		Callback: func(ctx context.Context) error {
			return nil
		},
	}

	h := NewHeartbeat(cfg)
	ctx := context.Background()
	h.Start(ctx)

	// Stop multiple times
	h.Stop()
	h.Stop()
	h.Stop() // Should not panic or deadlock

	if h.IsRunning() {
		t.Fatal("heartbeat should be stopped after Stop calls")
	}
}

// TestHeartbeatContextCancellation verifies the heartbeat stops when context is cancelled.
func TestHeartbeatContextCancellation(t *testing.T) {
	callCount := atomic.Int32{}

	cfg := Config{
		Interval: 10 * time.Millisecond,
		Callback: func(ctx context.Context) error {
			callCount.Add(1)
			return nil
		},
	}

	h := NewHeartbeat(cfg)
	ctx, cancel := context.WithCancel(context.Background())

	h.Start(ctx)
	time.Sleep(50 * time.Millisecond)

	// Cancel the context
	cancel()

	// Wait a bit for goroutine to finish
	time.Sleep(50 * time.Millisecond)

	count := callCount.Load()

	// Context cancellation should stop the heartbeat
	// The goroutine should exit cleanly
	if count == 0 {
		t.Fatal("callback should have fired before context cancellation")
	}

	// After context cancellation, no more calls should happen
	countBefore := count
	time.Sleep(50 * time.Millisecond)
	countAfter := callCount.Load()

	if countAfter > countBefore {
		t.Fatalf("callback should not fire after context cancellation, got increase from %d to %d", countBefore, countAfter)
	}
}

// TestHeartbeatIdleTracking verifies RecordActivity and IdleDuration work correctly.
func TestHeartbeatIdleTracking(t *testing.T) {
	cfg := Config{
		Interval: 10 * time.Millisecond,
		Callback: func(ctx context.Context) error {
			return nil
		},
	}

	h := NewHeartbeat(cfg)

	// Initially, idle duration should be very small (just created)
	idle1 := h.IdleDuration()
	if idle1 > 10 * time.Millisecond {
		t.Fatalf("initial idle duration should be near zero, got %v", idle1)
	}

	// Wait and check idle duration increases
	time.Sleep(50 * time.Millisecond)
	idle2 := h.IdleDuration()
	if idle2 < 40 * time.Millisecond {
		t.Fatalf("idle duration should be at least 40ms, got %v", idle2)
	}

	// RecordActivity should reset idle to near zero
	h.RecordActivity()
	idle3 := h.IdleDuration()
	if idle3 > 10 * time.Millisecond {
		t.Fatalf("idle duration after RecordActivity should be near zero, got %v", idle3)
	}
}

// TestHeartbeatDoubleStart is idempotent (no double-goroutine).
func TestHeartbeatDoubleStart(t *testing.T) {
	callCount := atomic.Int32{}

	cfg := Config{
		Interval: 10 * time.Millisecond,
		Callback: func(ctx context.Context) error {
			callCount.Add(1)
			return nil
		},
	}

	h := NewHeartbeat(cfg)
	ctx := context.Background()

	h.Start(ctx)
	h.Start(ctx) // Should be a no-op

	time.Sleep(50 * time.Millisecond)
	h.Stop()

	count := callCount.Load()

	// Should fire at normal rate, not double (would be ~10, not ~20)
	if count > 15 {
		t.Fatalf("callback should not fire at double rate due to double Start, got %d", count)
	}
}

// TestHeartbeatStopBeforeStart is safe.
func TestHeartbeatStopBeforeStart(t *testing.T) {
	cfg := Config{
		Interval: 10 * time.Millisecond,
		Callback: func(ctx context.Context) error {
			return nil
		},
	}

	h := NewHeartbeat(cfg)

	// Stop before ever starting should be safe
	h.Stop()

	if h.IsRunning() {
		t.Fatal("heartbeat should not be running after Stop on a never-started heartbeat")
	}
}

// TestHeartbeatConcurrentRecordActivity verifies RecordActivity is thread-safe.
func TestHeartbeatConcurrentRecordActivity(t *testing.T) {
	cfg := Config{
		Interval: 10 * time.Millisecond,
		Callback: func(ctx context.Context) error {
			return nil
		},
	}

	h := NewHeartbeat(cfg)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			h.RecordActivity()
		}()
	}

	wg.Wait()

	// Should not panic or have race conditions
	idle := h.IdleDuration()
	if idle > 100 * time.Millisecond {
		t.Fatalf("concurrent RecordActivity should work correctly, idle=%v", idle)
	}
}
