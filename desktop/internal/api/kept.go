package api

import (
	"sync"

	"github.com/worldledger/worldledger-mc/internal/spool"
)

// keptSizes totals the bundles being kept after import, measuring each one
// once.
//
// The status route is loaded by every screen in the window, and it was adding
// those bundles up from scratch each time. On the machine this was found on
// that was 608 directories and 4.5 seconds of a window sitting still while
// somebody clicked a tab, and it grows for as long as they keep playing:
// nothing removes an imported bundle until they press Clear. A tenth of that
// was the walk itself being written badly, which is fixed in spool.Size. The
// rest was the work being repeated.
//
// Remembering it is exact rather than approximate, which is the only reason to
// do it here at all. A bundle is named ready-<session uuid>-<sequence> by the
// adapter and keeps that name when it is renamed to imported-, so a path is
// never reused. Nothing in this project writes inside a bundle after it is
// complete; it is renamed once and then either read or deleted whole. A size
// measured against a path is therefore still that path's size, however long
// ago it was taken.
//
// Nothing is remembered between runs, deliberately. The first status load of a
// session measures everything, because a spool is an ordinary folder that
// anything can edit while this is not running.
type keptSizes struct {
	mu    sync.Mutex
	bytes map[string]int64
}

var kept = keptSizes{bytes: map[string]int64{}}

// total measures the paths it has not seen and forgets the ones that are gone.
// Forgetting matters because Clear removes every imported bundle at once, and a
// map that only grew would hold entries for directories that no longer exist
// for the life of the process.
func (k *keptSizes) total(paths []string) int64 {
	k.mu.Lock()
	defer k.mu.Unlock()

	var sum int64
	fresh := make(map[string]int64, len(paths))
	for _, path := range paths {
		size, known := k.bytes[path]
		if !known {
			size = spool.Size([]string{path})
		}
		fresh[path] = size
		sum += size
	}
	k.bytes = fresh
	return sum
}
