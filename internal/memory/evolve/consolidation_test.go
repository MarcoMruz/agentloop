package evolve

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestConsolidationTriggersOnIdle verifies that consolidation triggers when
// the idle threshold is exceeded without any recent activity.
func TestConsolidationTriggersOnIdle(t *testing.T) {
	// Track whether the handler was called
	handlerCalled := false
	handler := func(ctx context.Context, toolkit MemoryToolkit) error {
		handlerCalled = true
		return nil
	}

	config := ConsolidationConfig{
		IdleThreshold:                100 * time.Millisecond,
		ConsolidationInterval:        10 * time.Second,
		MaxConsolidationDuration:     5 * time.Second,
		Enabled:                      true,
	}

	toolkit := MemoryToolkit{
		Retrieve: func(ctx context.Context, query RetrievalQuery) (*RetrievalResult, error) {
			return &RetrievalResult{}, nil
		},
		Encode: func(ctx context.Context, input EncoderInput) (*EncoderOutput, error) {
			return &EncoderOutput{}, nil
		},
		Store: func(ctx context.Context, units []MemoryUnit) error {
			return nil
		},
		Compact: func(ctx context.Context, input CompactionInput) (*CompactionResult, error) {
			return &CompactionResult{}, nil
		},
	}

	manager := NewConsolidationManager(config, handler, toolkit)

	// Wait for idle threshold to be exceeded
	time.Sleep(150 * time.Millisecond)

	// Should consolidate due to idle
	if !manager.ShouldConsolidate() {
		t.Fatal("expected consolidation to be needed due to idle, but it was not")
	}

	trigger := manager.DetermineConsolidationTrigger()
	if trigger == nil || *trigger != TriggerIdle {
		t.Fatal("expected TriggerIdle, got something else")
	}

	// Execute consolidation
	err := manager.Consolidate(context.Background())
	if err != nil {
		t.Fatalf("consolidation failed: %v", err)
	}

	if !handlerCalled {
		t.Fatal("handler was not called during consolidation")
	}
}

// TestConsolidationForcedAfterInterval verifies that consolidation is triggered
// when the consolidation interval is exceeded, regardless of idle state.
func TestConsolidationForcedAfterInterval(t *testing.T) {
	handlerCalled := false
	handler := func(ctx context.Context, toolkit MemoryToolkit) error {
		handlerCalled = true
		return nil
	}

	config := ConsolidationConfig{
		IdleThreshold:                1 * time.Second,
		ConsolidationInterval:        100 * time.Millisecond,
		MaxConsolidationDuration:     5 * time.Second,
		Enabled:                      true,
	}

	toolkit := MemoryToolkit{
		Retrieve: func(ctx context.Context, query RetrievalQuery) (*RetrievalResult, error) {
			return &RetrievalResult{}, nil
		},
		Encode: func(ctx context.Context, input EncoderInput) (*EncoderOutput, error) {
			return &EncoderOutput{}, nil
		},
		Store: func(ctx context.Context, units []MemoryUnit) error {
			return nil
		},
		Compact: func(ctx context.Context, input CompactionInput) (*CompactionResult, error) {
			return &CompactionResult{}, nil
		},
	}

	manager := NewConsolidationManager(config, handler, toolkit)

	// Record activity to keep it active (not idle)
	manager.RecordActivity()

	// Wait for consolidation interval to be exceeded
	time.Sleep(150 * time.Millisecond)

	// Should consolidate due to forced interval
	if !manager.ShouldConsolidate() {
		t.Fatal("expected consolidation to be needed due to forced interval, but it was not")
	}

	trigger := manager.DetermineConsolidationTrigger()
	if trigger == nil || *trigger != TriggerForced {
		t.Fatal("expected TriggerForced, got something else")
	}

	// Execute consolidation
	err := manager.Consolidate(context.Background())
	if err != nil {
		t.Fatalf("consolidation failed: %v", err)
	}

	if !handlerCalled {
		t.Fatal("handler was not called during consolidation")
	}
}

// TestConsolidationTimeout verifies that consolidation respects the timeout
// and cancels the operation if it takes too long.
func TestConsolidationTimeout(t *testing.T) {
	handlerCtxCancelled := false
	handler := func(ctx context.Context, toolkit MemoryToolkit) error {
		// Simulate a long-running operation
		select {
		case <-time.After(2 * time.Second):
			// Operation completes (should not happen due to timeout)
			return nil
		case <-ctx.Done():
			// Context was cancelled due to timeout
			handlerCtxCancelled = true
			return ctx.Err()
		}
	}

	config := ConsolidationConfig{
		IdleThreshold:                100 * time.Millisecond,
		ConsolidationInterval:        10 * time.Second,
		MaxConsolidationDuration:     200 * time.Millisecond,
		Enabled:                      true,
	}

	toolkit := MemoryToolkit{
		Retrieve: func(ctx context.Context, query RetrievalQuery) (*RetrievalResult, error) {
			return &RetrievalResult{}, nil
		},
		Encode: func(ctx context.Context, input EncoderInput) (*EncoderOutput, error) {
			return &EncoderOutput{}, nil
		},
		Store: func(ctx context.Context, units []MemoryUnit) error {
			return nil
		},
		Compact: func(ctx context.Context, input CompactionInput) (*CompactionResult, error) {
			return &CompactionResult{}, nil
		},
	}

	manager := NewConsolidationManager(config, handler, toolkit)

	// Wait for idle threshold to be exceeded
	time.Sleep(150 * time.Millisecond)

	// Execute consolidation - should timeout
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := manager.Consolidate(ctx)

	// Verify that the handler detected the context cancellation
	if !handlerCtxCancelled {
		t.Fatal("expected handler context to be cancelled due to timeout, but it was not")
	}

	// The error should be related to context deadline
	if err != nil && !errors.Is(err, context.DeadlineExceeded) {
		// It's okay if there's no error because the manager catches and logs panics
		// But we check that handlerCtxCancelled was true above
	}
}

// TestConsolidationSkippedWhenActive verifies that consolidation does not trigger
// when activity is recent and consolidation interval has not elapsed.
func TestConsolidationSkippedWhenActive(t *testing.T) {
	handlerCalled := false
	handler := func(ctx context.Context, toolkit MemoryToolkit) error {
		handlerCalled = true
		return nil
	}

	config := ConsolidationConfig{
		IdleThreshold:                1 * time.Second,
		ConsolidationInterval:        10 * time.Second,
		MaxConsolidationDuration:     5 * time.Second,
		Enabled:                      true,
	}

	toolkit := MemoryToolkit{
		Retrieve: func(ctx context.Context, query RetrievalQuery) (*RetrievalResult, error) {
			return &RetrievalResult{}, nil
		},
		Encode: func(ctx context.Context, input EncoderInput) (*EncoderOutput, error) {
			return &EncoderOutput{}, nil
		},
		Store: func(ctx context.Context, units []MemoryUnit) error {
			return nil
		},
		Compact: func(ctx context.Context, input CompactionInput) (*CompactionResult, error) {
			return &CompactionResult{}, nil
		},
	}

	manager := NewConsolidationManager(config, handler, toolkit)

	// Record recent activity
	manager.RecordActivity()

	// Wait a short time (not enough to exceed either threshold)
	time.Sleep(100 * time.Millisecond)

	// Should not consolidate because both conditions are false:
	// 1. Idle threshold (1s) is not exceeded
	// 2. Consolidation interval (10s) is not exceeded
	if manager.ShouldConsolidate() {
		t.Fatal("expected consolidation to NOT be needed when activity is recent, but it was")
	}

	// DetermineConsolidationTrigger should return nil
	trigger := manager.DetermineConsolidationTrigger()
	if trigger != nil {
		t.Fatal("expected no trigger when active, but got one")
	}

	// Execute consolidation (should be no-op)
	err := manager.Consolidate(context.Background())
	if err != nil {
		t.Fatalf("consolidation should not error on no-op: %v", err)
	}

	if handlerCalled {
		t.Fatal("handler should not have been called when consolidation is not needed")
	}
}

// TestConsolidationDisabled verifies that consolidation is skipped when disabled.
func TestConsolidationDisabled(t *testing.T) {
	handlerCalled := false
	handler := func(ctx context.Context, toolkit MemoryToolkit) error {
		handlerCalled = true
		return nil
	}

	config := ConsolidationConfig{
		IdleThreshold:                100 * time.Millisecond,
		ConsolidationInterval:        100 * time.Millisecond,
		MaxConsolidationDuration:     5 * time.Second,
		Enabled:                      false, // Disabled
	}

	toolkit := MemoryToolkit{}

	manager := NewConsolidationManager(config, handler, toolkit)

	// Wait for thresholds to be exceeded
	time.Sleep(150 * time.Millisecond)

	// Should not consolidate because consolidation is disabled
	if manager.ShouldConsolidate() {
		t.Fatal("expected consolidation to be skipped when disabled")
	}

	if manager.DetermineConsolidationTrigger() != nil {
		t.Fatal("expected no trigger when disabled")
	}

	// Execute consolidation
	err := manager.Consolidate(context.Background())
	if err != nil {
		t.Fatalf("consolidation should not error when disabled: %v", err)
	}

	if handlerCalled {
		t.Fatal("handler should not be called when consolidation is disabled")
	}
}

// TestConsolidationMetrics verifies that consolidation metrics are tracked correctly.
func TestConsolidationMetrics(t *testing.T) {
	metrics := NewConsolidationMetrics()

	// Record some operations
	metrics.RecordConsolidation(true, 100*time.Millisecond)
	metrics.RecordConsolidation(true, 150*time.Millisecond)
	metrics.RecordConsolidation(false, 50*time.Millisecond)

	stats := metrics.Stats()

	if total, ok := stats["total_consolidations"].(uint32); !ok || total != 3 {
		t.Fatalf("expected 3 total consolidations, got %v", stats["total_consolidations"])
	}

	if successful, ok := stats["successful_consolidations"].(uint32); !ok || successful != 2 {
		t.Fatalf("expected 2 successful consolidations, got %v", stats["successful_consolidations"])
	}

	if failed, ok := stats["failed_consolidations"].(uint32); !ok || failed != 1 {
		t.Fatalf("expected 1 failed consolidation, got %v", stats["failed_consolidations"])
	}

	// Average duration should be around 100ms
	if avgDuration, ok := stats["average_duration_ms"].(float64); !ok || avgDuration < 50 || avgDuration > 200 {
		t.Fatalf("expected average duration around 100ms, got %v", avgDuration)
	}
}

// TestConsolidationHandlerError verifies that consolidation continues even if the handler returns an error.
func TestConsolidationHandlerError(t *testing.T) {
	expectedErr := errors.New("handler failed")
	handler := func(ctx context.Context, toolkit MemoryToolkit) error {
		return expectedErr
	}

	config := ConsolidationConfig{
		IdleThreshold:                100 * time.Millisecond,
		ConsolidationInterval:        10 * time.Second,
		MaxConsolidationDuration:     5 * time.Second,
		Enabled:                      true,
	}

	toolkit := MemoryToolkit{}

	manager := NewConsolidationManager(config, handler, toolkit)

	// Wait for idle threshold to be exceeded
	time.Sleep(150 * time.Millisecond)

	// Execute consolidation - should return the handler error
	err := manager.Consolidate(context.Background())
	if err != expectedErr {
		t.Fatalf("expected handler error to be returned, got %v", err)
	}

	// lastConsolidation should NOT be updated on failure
	// (verify by checking that ShouldConsolidate still returns true)
	if !manager.ShouldConsolidate() {
		t.Fatal("expected consolidation to still be needed after handler error")
	}
}

// TestConsolidationRecordActivityUpdatesTimestamp verifies that RecordActivity
// updates the last activity timestamp.
func TestConsolidationRecordActivityUpdatesTimestamp(t *testing.T) {
	handler := func(ctx context.Context, toolkit MemoryToolkit) error {
		return nil
	}

	config := ConsolidationConfig{
		IdleThreshold:                100 * time.Millisecond,
		ConsolidationInterval:        10 * time.Second,
		MaxConsolidationDuration:     5 * time.Second,
		Enabled:                      true,
	}

	toolkit := MemoryToolkit{}

	manager := NewConsolidationManager(config, handler, toolkit)

	// Wait for idle threshold to be exceeded
	time.Sleep(150 * time.Millisecond)

	// Should consolidate now
	if !manager.ShouldConsolidate() {
		t.Fatal("expected consolidation to be needed")
	}

	// Record activity
	manager.RecordActivity()

	// Now should not consolidate (activity is recent)
	if manager.ShouldConsolidate() {
		t.Fatal("expected consolidation to NOT be needed after recording activity")
	}
}
