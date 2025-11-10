package migrations

import (
	"io/fs"
	"sync"
)

var (
	mu          sync.RWMutex
	filesystems []fs.FS
)

// Register records a filesystem that contains go-users migrations. Callers can
// then feed all registered filesystems into go-persistence-bun (or any other
// runner) via Filesystems().
func Register(fsys fs.FS) {
	if fsys == nil {
		return
	}
	mu.Lock()
	filesystems = append(filesystems, fsys)
	mu.Unlock()
}

// Filesystems returns a copy of all registered migration filesystems.
func Filesystems() []fs.FS {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]fs.FS, len(filesystems))
	copy(out, filesystems)
	return out
}
