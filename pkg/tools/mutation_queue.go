package tools

import "sync"

// Package-level state. One mutex per path, guarded by a registry mutex.
var (
	fileLocks   = make(map[string]*sync.Mutex)
	fileLocksMu sync.Mutex
)

// WithFileMutationQueue serialises concurrent mutations to the same path.
// fn is invoked while holding the per-path lock. The registry lock is only
// held while looking up or creating the per-path mutex.
func WithFileMutationQueue(path string, fn func() error) error {
	fileLocksMu.Lock()
	lock, ok := fileLocks[path]
	if !ok {
		lock = &sync.Mutex{}
		fileLocks[path] = lock
	}
	fileLocksMu.Unlock()

	lock.Lock()
	defer lock.Unlock()
	return fn()
}
