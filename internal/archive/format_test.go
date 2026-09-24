package archive

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// An archive outlives every build that touches it, so a refusal that names only
// the number it found leaves somebody with nothing to act on. Which way the
// mismatch runs decides whether the answer is a newer WorldLedger or an older
// one, and this is the only place that can tell.

func archiveAtLayout(t *testing.T, layout string) string {
	t.Helper()
	dir := t.TempDir()
	if _, err := Init(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "VERSION"), []byte(layout+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestAnArchiveFromALaterBuildSaysToGoForward(t *testing.T) {
	_, err := Open(archiveAtLayout(t, "2"))
	if !errors.Is(err, ErrUnreadableFormat) {
		t.Fatalf("err = %v; want ErrUnreadableFormat", err)
	}
	for _, needed := range []string{"layout " + FormatVersion, "layout 2", "later WorldLedger"} {
		if !strings.Contains(err.Error(), needed) {
			t.Errorf("the refusal does not say %q: %v", needed, err)
		}
	}
}

func TestAnArchiveFromAnEarlierBuildSaysSo(t *testing.T) {
	_, err := Open(archiveAtLayout(t, "0"))
	if !errors.Is(err, ErrUnreadableFormat) {
		t.Fatalf("err = %v; want ErrUnreadableFormat", err)
	}
	if !strings.Contains(err.Error(), "earlier WorldLedger") {
		t.Errorf("the refusal does not say which way it runs: %v", err)
	}
}

// A layout this build cannot parse is treated as later, because a build that
// does not recognise one is more likely behind it than ahead, and being wrong
// that way sends somebody forwards rather than backwards.
func TestALayoutThatIsNotANumberSendsSomebodyForwards(t *testing.T) {
	_, err := Open(archiveAtLayout(t, "2-beta"))
	if !errors.Is(err, ErrUnreadableFormat) {
		t.Fatalf("err = %v; want ErrUnreadableFormat", err)
	}
	if !strings.Contains(err.Error(), "later WorldLedger") {
		t.Errorf("an unparseable layout did not send somebody forwards: %v", err)
	}
}

func TestAnEmptyVersionSaysThatRatherThanGuessing(t *testing.T) {
	_, err := Open(archiveAtLayout(t, ""))
	if !errors.Is(err, ErrUnreadableFormat) {
		t.Fatalf("err = %v; want ErrUnreadableFormat", err)
	}
	if !strings.Contains(err.Error(), "empty VERSION") {
		t.Errorf("an empty VERSION was given a direction it cannot have: %v", err)
	}
}

// Pointing at the wrong folder and holding the wrong program need opposite
// actions, so they stay different errors.
func TestAFolderThatIsNotAnArchiveIsStillItsOwnAnswer(t *testing.T) {
	_, err := Open(t.TempDir())
	if !errors.Is(err, ErrNotAnArchive) {
		t.Fatalf("err = %v; want ErrNotAnArchive", err)
	}
	if errors.Is(err, ErrUnreadableFormat) {
		t.Error("an empty folder was reported as an archive this build cannot read")
	}
}
