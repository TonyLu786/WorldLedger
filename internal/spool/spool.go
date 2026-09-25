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
	"fmt"
	"io/fs"
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
	// Imported are full paths, sorted, of bundles already taken into an archive
	// and kept afterwards.
	//
	// They are paths rather than a count because keeping them is a default
	// rather than a rule. A player who captures every evening accumulates them
	// inside their Minecraft directory, and the only honest way to offer to
	// clear them is to be able to name exactly which ones qualify: a bundle is
	// on this list only because an import already returned for it, which is
	// after the archive forced the observation to disk.
	Imported []string
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
			contents.Imported = append(contents.Imported, filepath.Join(dir, name))
		}
	}
	sort.Strings(contents.Ready)
	sort.Strings(contents.Imported)
	return contents, nil
}

// Size adds up what a list of bundles occupies.
//
// Errors are ignored deliberately. The number exists to answer "is this worth
// clearing", and a bundle with one unreadable file should still be counted
// rather than making the whole answer disappear.
// Size totals the bytes a set of bundle directories occupies.
//
// It walks with WalkDir rather than Walk because Walk calls lstat on every path
// it reaches, while WalkDir takes the size out of the directory listing the
// operating system has already returned. Over the 608 bundles a few evenings of
// play left on the machine this was measured on, that is 4.4 seconds against
// 0.42, for a byte-identical answer. Walk also got slower on a second run with
// everything in cache, which is what a syscall-bound loop looks like.
func Size(paths []string) int64 {
	var total int64
	for _, path := range paths {
		filepath.WalkDir(path, func(_ string, entry fs.DirEntry, err error) error {
			if err != nil || entry.IsDir() {
				return nil
			}
			info, err := entry.Info()
			if err != nil {
				return nil
			}
			total += info.Size()
			return nil
		})
	}
	return total
}

// Discard removes bundles that have already been imported.
//
// It takes the whole path and checks the name itself rather than trusting the
// caller, because this is the one operation here that destroys the only copy of
// something outside the archive. A path that is not marked imported is refused
// rather than skipped: a caller asking to delete a ready bundle has a bug, and
// carrying on quietly would hide it behind a smaller number.
//
// It returns how many were removed and how many bytes went with them, so that
// whatever asked can say what happened rather than that something happened.
func Discard(paths []string) (removed int, freed int64, err error) {
	for _, path := range paths {
		if !strings.HasPrefix(filepath.Base(path), ImportedPrefix) {
			return removed, freed, fmt.Errorf(
				"refusing to remove %s: only bundles already taken into an archive can be cleared",
				filepath.Base(path))
		}
	}
	for _, path := range paths {
		size := Size([]string{path})
		if err := os.RemoveAll(path); err != nil {
			return removed, freed, err
		}
		removed++
		freed += size
	}
	return removed, freed, nil
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
