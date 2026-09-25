package transfer

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The guard that stops a bundle being built inside the archive it is read from
// compares two path strings, and string comparison is case sensitive where
// Windows is not.
//
// It holds, because both sides go through resolveExisting first and
// EvalSymlinks hands back the casing the filesystem actually uses. That is a
// property of the fix for the short-name problem rather than something it was
// written for, so it is worth a test of its own: someone simplifying
// resolveExisting away would take this with it and nothing else would notice.
func TestOnWindowsADifferentlyCasedPathIsStillInsideTheArchive(t *testing.T) {
	if runtime.GOOS != "windows" {
		// Elsewhere these are two directories and accepting the second is the
		// right answer, so there is nothing here to check.
		t.Skip("path casing only names the same directory on Windows")
	}

	root := t.TempDir()
	archive := filepath.Join(root, "Archive")
	if err := os.MkdirAll(filepath.Join(archive, "objects"), 0o755); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(strings.ToLower(archive), "bundle")
	if err := requireSeparateFromArchive(out, archive); err == nil {
		t.Errorf("an output directory inside the archive was accepted because it was spelled in a different case:\n  archive %s\n  out     %s",
			archive, out)
	}
}

// And the ordinary answer is still no refusal, so the test above is not passing
// because everything is refused.
func TestADirectoryBesideTheArchiveIsAccepted(t *testing.T) {
	root := t.TempDir()
	archive := filepath.Join(root, "Archive")
	if err := os.MkdirAll(filepath.Join(archive, "objects"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := requireSeparateFromArchive(filepath.Join(root, "bundle"), archive); err != nil {
		t.Errorf("a directory beside the archive was refused: %v", err)
	}
}
