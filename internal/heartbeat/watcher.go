package heartbeat

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Watcher monitors a heartbeat config file for changes and reconfigures
// the associated Heartbeat instance when changes are detected.
type Watcher struct {
	mu              sync.Mutex
	heartbeat       *Heartbeat
	configPath      string
	debounceTimeout time.Duration
	debounceTimer   *time.Timer
	stopCh          chan struct{}
	readyCh         chan struct{}
	wg              sync.WaitGroup
	running         bool
}

// NewWatcher creates a new Watcher for the given heartbeat config file.
// debounceTimeout should typically be 500ms to group rapid file changes.
func NewWatcher(heartbeat *Heartbeat, configPath string, debounceTimeout time.Duration) *Watcher {
	return &Watcher{
		heartbeat:       heartbeat,
		configPath:      configPath,
		debounceTimeout: debounceTimeout,
		stopCh:          make(chan struct{}),
		readyCh:         make(chan struct{}),
	}
}

// Start begins monitoring the config file. If already running, it's a no-op.
// Starts a goroutine that manages the fsnotify watcher and debounce timer.
// Blocks until the watcher goroutine is ready to receive events.
func (w *Watcher) Start(ctx context.Context) error {
	w.mu.Lock()

	// If already running, just return
	if w.running {
		w.mu.Unlock()
		return nil
	}

	// Mark as running and reset channels if needed
	w.running = true
	w.stopCh = make(chan struct{})
	w.readyCh = make(chan struct{})

	// Create fsnotify watcher
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		w.running = false
		w.mu.Unlock()
		return err
	}

	// Watch the config file's directory (fsnotify watches dirs, not individual files)
	dir := filepath.Dir(w.configPath)
	if err := watcher.Add(dir); err != nil {
		watcher.Close()
		w.running = false
		w.mu.Unlock()
		return err
	}

	w.wg.Add(1)
	w.mu.Unlock()

	go w.run(ctx, watcher)

	// Wait for watcher to be ready
	<-w.readyCh

	return nil
}

// run is the main watch loop that processes fsnotify events.
func (w *Watcher) run(ctx context.Context, watcher *fsnotify.Watcher) {
	defer w.wg.Done()
	defer watcher.Close()
	defer w.resetDebounceTimer()

	// Signal that we're ready to receive events
	close(w.readyCh)

	for {
		select {
		case <-w.stopCh:
			return
		case <-ctx.Done():
			return
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			// Only react to writes/creates on the config file itself
			if event.Name == w.configPath && (event.Op&fsnotify.Write != 0 || event.Op&fsnotify.Create != 0) {
				w.scheduleReconfigure()
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			// Log watch errors
			slog.Warn("watcher fsnotify error", "error", err)
		}
	}
}

// scheduleReconfigure resets the debounce timer, collapsing rapid file changes
// into a single reconfiguration call.
func (w *Watcher) scheduleReconfigure() {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Cancel existing timer if any
	if w.debounceTimer != nil {
		w.debounceTimer.Stop()
	}

	// Schedule new timer
	w.debounceTimer = time.AfterFunc(w.debounceTimeout, func() {
		w.performReconfigure()
	})
}

// performReconfigure reads the config file, parses it, and calls Reconfigure
// on the Heartbeat instance.
func (w *Watcher) performReconfigure() {
	content, err := os.ReadFile(w.configPath)
	if err != nil {
		// Log file read errors
		slog.Warn("watcher failed to read config file", "path", w.configPath, "error", err)
		return
	}

	parsedCfg, err := ParseHeartbeatConfig(string(content))
	if err != nil {
		// Log parse errors
		slog.Warn("watcher failed to parse config file", "path", w.configPath, "error", err)
		return
	}

	// Read the old callback from the heartbeat's current config
	// The Heartbeat struct internally locks when accessing config
	// We need to preserve the callback when updating interval
	oldCallback := w.heartbeat.config.Callback

	newConfig := Config{
		Interval: parsedCfg.Interval,
		Callback: oldCallback,
	}

	w.heartbeat.Reconfigure(newConfig)
}

// Stop gracefully stops the watcher. Idempotent.
func (w *Watcher) Stop() {
	w.mu.Lock()

	// If not running, return immediately
	if !w.running {
		w.mu.Unlock()
		return
	}

	w.running = false

	close(w.stopCh)

	if w.debounceTimer != nil {
		w.debounceTimer.Stop()
		w.debounceTimer = nil
	}

	w.mu.Unlock()

	// Wait for goroutine to exit
	w.wg.Wait()
}

// resetDebounceTimer is a helper to clean up the timer reference.
func (w *Watcher) resetDebounceTimer() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.debounceTimer != nil {
		w.debounceTimer.Stop()
		w.debounceTimer = nil
	}
}


