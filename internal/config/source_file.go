package config

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/fsnotify/fsnotify"
)

// FileSource reads configuration from local files and optionally watches them.
type FileSource struct {
	mainPath      string
	transformPath string

	mu      sync.Mutex
	watcher *fsnotify.Watcher
}

// NewFileSource creates a file source. transformPath may be empty.
func NewFileSource(mainPath, transformPath string) *FileSource {
	return &FileSource{mainPath: mainPath, transformPath: transformPath}
}

// Load reads the main configuration and, if configured, the transform configuration.
// A transform file that cannot be read is reported but does not fail the load,
// matching the historical startup behavior (pass-through mode).
func (f *FileSource) Load(ctx context.Context) (*Snapshot, error) {
	main, err := os.ReadFile(f.mainPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file %s: %w", f.mainPath, err)
	}

	var transform []byte
	if f.transformPath != "" {
		transform, err = os.ReadFile(f.transformPath)
		if err != nil {
			log.Printf("⚠️  Warning: failed to read transform config %s: %v", f.transformPath, err)
			transform = nil
		}
	}

	return NewSnapshot(f.Describe(), main, transform, f.transformPath != ""), nil
}

// Watch watches the directories of the configured files. Events for unrelated
// files are ignored, except Kubernetes ConfigMap symlink swaps ("..data").
func (f *FileSource) Watch(ctx context.Context, onChange func(*Snapshot)) error {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("failed to create file watcher: %w", err)
	}

	targets := map[string]bool{}
	dirs := map[string]bool{}
	for _, p := range []string{f.mainPath, f.transformPath} {
		if p == "" {
			continue
		}
		abs, err := filepath.Abs(p)
		if err != nil {
			w.Close()
			return fmt.Errorf("failed to resolve path %s: %w", p, err)
		}
		targets[abs] = true
		dirs[filepath.Dir(abs)] = true
	}
	for dir := range dirs {
		if err := w.Add(dir); err != nil {
			w.Close()
			return fmt.Errorf("failed to watch directory %s: %w", dir, err)
		}
	}

	f.mu.Lock()
	f.watcher = w
	f.mu.Unlock()

	log.Printf("ConfigSource(file): watching %v", keys(targets))

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-w.Events:
				if !ok {
					return
				}
				if ev.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename) == 0 {
					continue
				}
				abs, _ := filepath.Abs(ev.Name)
				if !targets[abs] && !strings.HasPrefix(filepath.Base(ev.Name), "..") {
					continue
				}
				snap, err := f.Load(ctx)
				if err != nil {
					log.Printf("ConfigSource(file): failed to reload after %s: %v", ev, err)
					continue
				}
				onChange(snap)
			case err, ok := <-w.Errors:
				if !ok {
					return
				}
				log.Printf("ConfigSource(file): watcher error: %v", err)
			}
		}
	}()

	return nil
}

// Close stops watching.
func (f *FileSource) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.watcher != nil {
		err := f.watcher.Close()
		f.watcher = nil
		return err
	}
	return nil
}

// Describe returns the source description.
func (f *FileSource) Describe() string {
	return "file://" + f.mainPath
}

// ReadOnly reports false: local files may be edited.
func (f *FileSource) ReadOnly() bool { return false }

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
