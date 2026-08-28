// Package atomicfile replaces a file in one step, so a reader never sees half
// of one and a crash never leaves half of one behind.
//
// The declarations an archive keeps beside its observations -- publication
// policies, redactions, identities, attestations, landmarks -- were each written
// with os.WriteFile, which truncates in place. A crash during one leaves a
// partial file, and every reader of it then fails to parse: a half-written
// redaction takes `redact list`, `redact purge`, coverage and export with it
// until somebody deletes the file by hand. Those all fail closed, which is the
// right direction, but "your archive is unusable until you find and delete a
// file nobody told you about" is not a recovery.
//
// The archive and the object store each grew their own version of this before
// there was a package for it. They are left alone: both work, both are covered
// by tests that would notice a change, and moving them is churn with a real
// downside and no user-visible upside.
package atomicfile

import (
	"os"
	"path/filepath"
)

// Write puts data at path, replacing whatever is there, or leaves the previous
// contents completely untouched.
//
// The temporary is created in the destination's own directory, because a rename
// is only atomic within one filesystem. It carries a name this project
// recognises as its own leftovers, so that a crash between creating it and
// renaming it leaves something identifiable rather than a stray file nobody can
// account for.
func Write(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	handle, err := os.CreateTemp(dir, ".worldledger-tmp-*")
	if err != nil {
		return err
	}
	name := handle.Name()
	// Removed on every path that does not rename it away, including the ones
	// that return an error part way through.
	defer os.Remove(name)

	if _, err := handle.Write(data); err != nil {
		handle.Close()
		return err
	}
	if err := handle.Chmod(perm); err != nil {
		handle.Close()
		return err
	}
	// Forced before the rename. A file that is a rename away from complete but
	// whose bytes never reached the disk is worse than one that was never
	// written: the name says it is there and the contents are not.
	if err := handle.Sync(); err != nil {
		handle.Close()
		return err
	}
	if err := handle.Close(); err != nil {
		return err
	}
	if err := replaceFile(name, path); err != nil {
		return err
	}
	return syncDirectory(dir)
}
