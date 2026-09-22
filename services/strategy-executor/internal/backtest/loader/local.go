package loader

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// LocalObjectStore serves archive objects from a local directory laid out with
// the same key structure as the bucket (e.g. a target of `aws s3 sync`).
//
// It exists so that a multi-day analysis run can pay the S3 download cost once
// and then iterate offline. Reading 26k+ small objects over the network on
// every experiment is slow enough to discourage re-running an analysis, and an
// analysis that is painful to re-run is one that silently goes stale.
type LocalObjectStore struct {
	root string
}

// NewLocalObjectStore roots a store at dir. Keys are resolved relative to it.
func NewLocalObjectStore(dir string) *LocalObjectStore {
	return &LocalObjectStore{root: dir}
}

// ListObjects walks the directory and returns keys (relative, slash-separated)
// that start with prefix.
func (l *LocalObjectStore) ListObjects(_ context.Context, prefix string) ([]string, error) {
	base := filepath.Join(l.root, filepath.FromSlash(prefix))

	// A missing day directory is a normal, expected condition (the collector
	// may have been down, or the window may extend past the archive), not an
	// error. Returning an error here would abort a whole multi-day load.
	if _, err := os.Stat(base); os.IsNotExist(err) {
		return nil, nil
	}

	var keys []string
	err := filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(l.root, path)
		if relErr != nil {
			return relErr
		}
		keys = append(keys, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk %s: %w", base, err)
	}
	return keys, nil
}

// GetObject reads one object from disk.
func (l *LocalObjectStore) GetObject(_ context.Context, key string) ([]byte, error) {
	// Reject traversal outside the root; keys come from listings, but this
	// store may also be pointed at caller-supplied keys.
	clean := filepath.Clean(filepath.FromSlash(key))
	if strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) {
		return nil, fmt.Errorf("invalid key %q", key)
	}
	return os.ReadFile(filepath.Join(l.root, clean))
}
