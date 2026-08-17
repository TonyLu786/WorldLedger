// Package spool reads the directory the capture adapter writes into.
//
// The adapter names a directory by what state it is in, and those three names
// are a small protocol between it and whatever imports from it. Reading them in
// more than one place is how the two come to disagree about what "ready" means.
//
// The distinctions matter to what a person is told. A directory still being
// written is a client that is probably running, and importing it would take a
// half-written bundle. A quarantined one is something the adapter refused, and
// it is kept rather than deleted precisely so somebody can look at it. Reporting
// either as "nothing to import" would be true and useless.
package spool

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	readyPrefix      = "ready-"
	inProgressPrefix = ".tmp-"
	quarantinePrefix = "quarantine-"
	// ImportedPrefix marks a bundle that has been taken into an archive and is
	// being kept anyway.
	//
	// The command line deletes on success, which suits somebody watching output
	// scroll past. A window cannot delete a person's only copy of what they saw
	// on the strength of a button they pressed once, and it also cannot leave
	// the bundle looking like work that is still outstanding: "40 waiting to be
	// brought in" that never goes down is a broken application to anybody
	// reading it. Renaming keeps the bytes and tells the truth about the count.
	//
	// The adapter only ever writes into this directory, so a fourth name here
	// is invisible to it, and the command line ignores what it does not
	// recognise.
	ImportedPrefix = "imported-"
)

// Contents is what is sitting in a spool.
type Contents struct {
	// Ready are full paths, sorted, which is also the order they were captured
	// in: the adapter names them so that sorting by name sorts by sequence.
	Ready []string
	// InProgress is a count of bundles the adapter has not finished writing.
	InProgress int
	// Quarantined is a count of bundles the adapter itself rejected.
	Quarantined int
	// Imported is a count of bundles already taken into an archive and kept.
	Imported int
}

// Read reports what is in a spool directory.
//
// A directory that is not there is returned as an error rather than as empty
// contents, because "no spool" and "an empty spool" send a person to different
// places: the first means the mod has never run, the second means it ran and
// recorded nothing.
func Read(dir string) (Contents, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return Contents{}, err
	}

	var contents Contents
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		switch {
		case strings.HasPrefix(name, readyPrefix):
			contents.Ready = append(contents.Ready, filepath.Join(dir, name))
		case strings.HasPrefix(name, inProgressPrefix):
			contents.InProgress++
		case strings.HasPrefix(name, quarantinePrefix):
			contents.Quarantined++
		case strings.HasPrefix(name, ImportedPrefix):
			contents.Imported++
		}
	}
	sort.Strings(contents.Ready)
	return contents, nil
}

// MarkImported renames a bundle so it stops counting as outstanding.
//
// It is called only after the import returned, which is after the archive has
// forced the observation to disk. A rename that fails is reported rather than
// swallowed: the import did happen, and the caller has to be able to say that
// the bundle will be offered again.
func MarkImported(path string) error {
	dir, name := filepath.Split(path)
	if !strings.HasPrefix(name, readyPrefix) {
		// Nothing to do, and nothing worth failing over: a caller that hands
		// over something already marked has not done anything wrong.
		return nil
	}
	return os.Rename(path, filepath.Join(dir, ImportedPrefix+strings.TrimPrefix(name, readyPrefix)))
}
