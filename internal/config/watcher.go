package config

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// ConfigWatcher monitors configuration file changes and triggers synchronization
type ConfigWatcher struct {
	configPath      string
	multiConfigPath string
	syncService     *ConfigSyncService
	watcher         *fsnotify.Watcher
	
	// Event channels
	eventChan     chan ConfigEvent
	subscribers   map[string]chan ConfigEvent
	subscribersMu sync.RWMutex
	
	// Control
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
	
	// Configuration
	debounceTime time.Duration
	autoSync     bool
}

// ConfigEvent represents a configuration change event
type ConfigEvent struct {
	Type      EventType `json:"type"`
	File      string    `json:"file"`
	Timestamp time.Time `json:"timestamp"`
	Message   string    `json:"message"`
	Data      any       `json:"data,omitempty"`
}

// EventType represents the type of configuration event
type EventType string

const (
	EventFileChanged     EventType = "file_changed"
	EventSyncStarted     EventType = "sync_started" 
	EventSyncCompleted   EventType = "sync_completed"
	EventSyncFailed      EventType = "sync_failed"
	EventValidationError EventType = "validation_error"
	EventStatusUpdate    EventType = "status_update"
)

// NewConfigWatcher creates a new configuration file watcher
func NewConfigWatcher(configPath, multiConfigPath string, syncService *ConfigSyncService) (*ConfigWatcher, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("failed to create file watcher: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	
	cw := &ConfigWatcher{
		configPath:      configPath,
		multiConfigPath: multiConfigPath,
		syncService:     syncService,
		watcher:         watcher,
		eventChan:       make(chan ConfigEvent, 100),
		subscribers:     make(map[string]chan ConfigEvent),
		ctx:             ctx,
		cancel:          cancel,
		done:            make(chan struct{}),
		debounceTime:    2 * time.Second, // 2 second debounce
		autoSync:        true,
	}

	return cw, nil
}

// Start initiates configuration monitoring
func (cw *ConfigWatcher) Start() error {
	// Monitor configuration file directory
	configDir := filepath.Dir(cw.configPath)
	if err := cw.watcher.Add(configDir); err != nil {
		return fmt.Errorf("failed to watch config directory: %w", err)
	}

	multiConfigDir := filepath.Dir(cw.multiConfigPath)
	if multiConfigDir != configDir {
		if err := cw.watcher.Add(multiConfigDir); err != nil {
			return fmt.Errorf("failed to watch multi config directory: %w", err)
		}
	}

	log.Printf("ConfigWatcher: Started watching directories: %s, %s", configDir, multiConfigDir)

	// Start monitoring goroutines
	go cw.watchLoop()
	go cw.eventLoop()

	// Send initial status
	cw.publishStatusUpdate()

	return nil
}

// Stop terminates configuration monitoring
func (cw *ConfigWatcher) Stop() error {
	cw.cancel()
	
	if err := cw.watcher.Close(); err != nil {
		log.Printf("ConfigWatcher: Error closing file watcher: %v", err)
	}

	// Wait for goroutines to exit
	select {
	case <-cw.done:
	case <-time.After(5 * time.Second):
		log.Printf("ConfigWatcher: Timeout waiting for shutdown")
	}

	log.Printf("ConfigWatcher: Stopped")
	return nil
}

// Subscribe subscribes to configuration change events
func (cw *ConfigWatcher) Subscribe(id string) <-chan ConfigEvent {
	cw.subscribersMu.Lock()
	defer cw.subscribersMu.Unlock()

	ch := make(chan ConfigEvent, 50)
	cw.subscribers[id] = ch
	
	log.Printf("ConfigWatcher: Added subscriber: %s", id)
	return ch
}

// Unsubscribe cancels subscription to configuration events
func (cw *ConfigWatcher) Unsubscribe(id string) {
	cw.subscribersMu.Lock()
	defer cw.subscribersMu.Unlock()

	if ch, exists := cw.subscribers[id]; exists {
		close(ch)
		delete(cw.subscribers, id)
		log.Printf("ConfigWatcher: Removed subscriber: %s", id)
	}
}

// SetAutoSync enables or disables automatic synchronization
func (cw *ConfigWatcher) SetAutoSync(enabled bool) {
	cw.autoSync = enabled
	log.Printf("ConfigWatcher: Auto sync set to %v", enabled)
}

// GetStatus returns the current synchronization status
func (cw *ConfigWatcher) GetStatus() (*ConfigSyncStatus, error) {
	return cw.syncService.GetSyncStatus(cw.ctx)
}

// TriggerSync manually triggers synchronization
func (cw *ConfigWatcher) TriggerSync(direction string) error {
	event := ConfigEvent{
		Type:      EventSyncStarted,
		Timestamp: time.Now(),
		Message:   fmt.Sprintf("Manual sync triggered: %s", direction),
	}
	cw.publishEvent(event)

	var err error
	switch direction {
	case "config_to_multi":
		err = cw.syncService.SyncConfigToMulti(cw.ctx)
	case "multi_to_config":
		err = cw.syncService.SyncMultiToConfig(cw.ctx)
	case "bidirectional":
		err = cw.syncService.BiDirectionalSync(cw.ctx)
	default:
		err = fmt.Errorf("unknown sync direction: %s", direction)
	}

	if err != nil {
		event = ConfigEvent{
			Type:      EventSyncFailed,
			Timestamp: time.Now(),
			Message:   fmt.Sprintf("Sync failed: %v", err),
		}
	} else {
		event = ConfigEvent{
			Type:      EventSyncCompleted,
			Timestamp: time.Now(),
			Message:   fmt.Sprintf("Sync completed: %s", direction),
		}
		cw.publishStatusUpdate()
	}

	cw.publishEvent(event)
	return err
}

// Internal methods

func (cw *ConfigWatcher) watchLoop() {
	defer close(cw.done)
	
	debouncer := make(map[string]*time.Timer)
	
	for {
		select {
		case <-cw.ctx.Done():
			return
			
		case event, ok := <-cw.watcher.Events:
			if !ok {
				return
			}
			
			// Only process files we care about
			if !cw.isWatchedFile(event.Name) {
				continue
			}
			
			// Only process write and create events
			if event.Op&fsnotify.Write != fsnotify.Write && event.Op&fsnotify.Create != fsnotify.Create {
				continue
			}

			log.Printf("ConfigWatcher: File event: %s %s", event.Op, event.Name)
			
			// Debounce handling
			if timer, exists := debouncer[event.Name]; exists {
				timer.Stop()
			}
			
			debouncer[event.Name] = time.AfterFunc(cw.debounceTime, func() {
				cw.handleFileChange(event.Name)
				delete(debouncer, event.Name)
			})

		case err, ok := <-cw.watcher.Errors:
			if !ok {
				return
			}
			log.Printf("ConfigWatcher: Error: %v", err)
		}
	}
}

func (cw *ConfigWatcher) eventLoop() {
	ticker := time.NewTicker(30 * time.Second) // Send status updates every 30 seconds
	defer ticker.Stop()

	for {
		select {
		case <-cw.ctx.Done():
			return
			
		case <-ticker.C:
			cw.publishStatusUpdate()
			
		case event := <-cw.eventChan:
			cw.publishEvent(event)
		}
	}
}

func (cw *ConfigWatcher) isWatchedFile(path string) bool {
	base := filepath.Base(path)
	configBase := filepath.Base(cw.configPath)
	multiConfigBase := filepath.Base(cw.multiConfigPath)
	
	return base == configBase || base == multiConfigBase
}

func (cw *ConfigWatcher) handleFileChange(filePath string) {
	event := ConfigEvent{
		Type:      EventFileChanged,
		File:      filepath.Base(filePath),
		Timestamp: time.Now(),
		Message:   fmt.Sprintf("File changed: %s", filepath.Base(filePath)),
	}
	
	select {
	case cw.eventChan <- event:
	case <-cw.ctx.Done():
		return
	}

	// If auto-sync is enabled, perform bidirectional synchronization
	if cw.autoSync {
		go func() {
			time.Sleep(1 * time.Second) // Wait for file write completion
			
			syncEvent := ConfigEvent{
				Type:      EventSyncStarted,
				Timestamp: time.Now(),
				Message:   "Auto sync triggered by file change",
			}
			select {
			case cw.eventChan <- syncEvent:
			case <-cw.ctx.Done():
				return
			}

			err := cw.syncService.BiDirectionalSync(cw.ctx)
			
			if err != nil {
				syncEvent = ConfigEvent{
					Type:      EventSyncFailed,
					Timestamp: time.Now(),
					Message:   fmt.Sprintf("Auto sync failed: %v", err),
				}
			} else {
				syncEvent = ConfigEvent{
					Type:      EventSyncCompleted,
					Timestamp: time.Now(),
					Message:   "Auto sync completed successfully",
				}
			}
			
			select {
			case cw.eventChan <- syncEvent:
			case <-cw.ctx.Done():
				return
			}
			
			// Send status update
			cw.publishStatusUpdate()
		}()
	}
}

func (cw *ConfigWatcher) publishEvent(event ConfigEvent) {
	cw.subscribersMu.RLock()
	defer cw.subscribersMu.RUnlock()

	log.Printf("ConfigWatcher: Publishing event: %s - %s", event.Type, event.Message)

	for id, ch := range cw.subscribers {
		select {
		case ch <- event:
		default:
			log.Printf("ConfigWatcher: Subscriber %s channel full, dropping event", id)
		}
	}
}

func (cw *ConfigWatcher) publishStatusUpdate() {
	status, err := cw.syncService.GetSyncStatus(cw.ctx)
	if err != nil {
		log.Printf("ConfigWatcher: Failed to get sync status: %v", err)
		return
	}

	event := ConfigEvent{
		Type:      EventStatusUpdate,
		Timestamp: time.Now(),
		Message:   "Status update",
		Data:      status,
	}

	select {
	case cw.eventChan <- event:
	case <-cw.ctx.Done():
	}
}

// ConfigWatcherManager manages multiple configuration watchers
type ConfigWatcherManager struct {
	watchers map[string]*ConfigWatcher
	mu       sync.RWMutex
}

// NewConfigWatcherManager creates a new configuration watcher manager
func NewConfigWatcherManager() *ConfigWatcherManager {
	return &ConfigWatcherManager{
		watchers: make(map[string]*ConfigWatcher),
	}
}

// AddWatcher adds a configuration watcher
func (cwm *ConfigWatcherManager) AddWatcher(id string, watcher *ConfigWatcher) {
	cwm.mu.Lock()
	defer cwm.mu.Unlock()
	
	cwm.watchers[id] = watcher
}

// RemoveWatcher removes a configuration watcher
func (cwm *ConfigWatcherManager) RemoveWatcher(id string) error {
	cwm.mu.Lock()
	defer cwm.mu.Unlock()

	if watcher, exists := cwm.watchers[id]; exists {
		err := watcher.Stop()
		delete(cwm.watchers, id)
		return err
	}
	
	return fmt.Errorf("watcher not found: %s", id)
}

// GetWatcher retrieves a configuration watcher by ID
func (cwm *ConfigWatcherManager) GetWatcher(id string) (*ConfigWatcher, bool) {
	cwm.mu.RLock()
	defer cwm.mu.RUnlock()
	
	watcher, exists := cwm.watchers[id]
	return watcher, exists
}

// StopAll stops all configuration watchers
func (cwm *ConfigWatcherManager) StopAll() {
	cwm.mu.Lock()
	defer cwm.mu.Unlock()

	for id, watcher := range cwm.watchers {
		if err := watcher.Stop(); err != nil {
			log.Printf("ConfigWatcherManager: Error stopping watcher %s: %v", id, err)
		}
	}
	
	cwm.watchers = make(map[string]*ConfigWatcher)
}
