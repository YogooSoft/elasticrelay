package config

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"
)

// Snapshot is the raw configuration content of a source at a point in time.
type Snapshot struct {
	Main      []byte
	Transform []byte

	// HasTransform reports whether a transform configuration is configured for
	// this source at all; Transform may still be empty if it could not be read.
	HasTransform bool

	MainRevision      string
	TransformRevision string

	Origin   string
	LoadedAt time.Time
}

// NewSnapshot builds a Snapshot and computes content revisions.
func NewSnapshot(origin string, main, transform []byte, hasTransform bool) *Snapshot {
	return &Snapshot{
		Main:              main,
		Transform:         transform,
		HasTransform:      hasTransform,
		MainRevision:      contentRevision(main),
		TransformRevision: contentRevision(transform),
		Origin:            origin,
		LoadedAt:          time.Now(),
	}
}

func contentRevision(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])[:12]
}

// Source provides configuration content and notifies about changes.
type Source interface {
	// Load synchronously fetches the full configuration.
	Load(ctx context.Context) (*Snapshot, error)
	// Watch starts watching for changes and returns immediately. onChange is
	// called with the full latest Snapshot; it must not block. Watching stops
	// when ctx is cancelled or Close is called.
	Watch(ctx context.Context, onChange func(*Snapshot)) error
	// Close releases all resources held by the source.
	Close() error
	// Describe returns a human-readable, secret-free description.
	Describe() string
	// ReadOnly reports whether configuration must not be modified locally.
	ReadOnly() bool
}

// NewSource creates the configuration source selected by opts.
func NewSource(opts *Options) (Source, error) {
	switch opts.Source {
	case "", SourceFile:
		return NewFileSource(opts.ConfigFile, opts.TransformConfig), nil
	case SourceNacos:
		return NewNacosSource(opts.Nacos)
	default:
		return nil, fmt.Errorf("unsupported config source: %s", opts.Source)
	}
}
