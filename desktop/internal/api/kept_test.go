package api

import (
	"os"
	"path/filepath"
	"testing"
)

func keptBundle(t *testing.T, dir, name string, size int) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Join(path, "components"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "components", "blocks.bin"), make([]byte, size), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestTheTotalIsWhatIsThere(t *testing.T) {
	dir := t.TempDir()
	first := keptBundle(t, dir, "imported-a", 300)
	second := keptBundle(t, dir, "imported-b", 700)

	k := keptSizes{bytes: map[string]int64{}}
	if got := k.total([]string{first, second}); got != 1000 {
		t.Fatalf("total %d, want 1000", got)
	}
	// Asking again is the case this exists for, and it has to give the same
	// answer rather than a remembered fragment of one.
	if got := k.total([]string{first, second}); got != 1000 {
		t.Fatalf("second total %d, want 1000", got)
	}
}

// A bundle that appears after the first total has to be counted. This is the
// ordinary case -- somebody plays, imports, and looks at the screen again --
// and a memo that only answered from what it had would report the old number
// forever.
func TestABundleThatArrivesLaterIsCounted(t *testing.T) {
	dir := t.TempDir()
	first := keptBundle(t, dir, "imported-a", 300)

	k := keptSizes{bytes: map[string]int64{}}
	if got := k.total([]string{first}); got != 300 {
		t.Fatalf("total %d, want 300", got)
	}

	second := keptBundle(t, dir, "imported-b", 700)
	if got := k.total([]string{first, second}); got != 1000 {
		t.Fatalf("after a new bundle: total %d, want 1000", got)
	}
}

// And one that is cleared has to stop being counted, and stop being held.
func TestClearedBundlesAreNeitherCountedNorHeld(t *testing.T) {
	dir := t.TempDir()
	first := keptBundle(t, dir, "imported-a", 300)
	second := keptBundle(t, dir, "imported-b", 700)

	k := keptSizes{bytes: map[string]int64{}}
	k.total([]string{first, second})

	if err := os.RemoveAll(second); err != nil {
		t.Fatal(err)
	}
	if got := k.total([]string{first}); got != 300 {
		t.Fatalf("after clearing: total %d, want 300", got)
	}
	if len(k.bytes) != 1 {
		t.Errorf("it is still holding %d measurements for 1 bundle", len(k.bytes))
	}
}

// Nothing is measured twice. This is the whole claim, and every other test here
// would pass against a memo that did nothing at all, so this one empties the
// bundles behind its back: a total that still says 300 is one that did not go
// back to the disk.
func TestEachBundleIsMeasuredOnce(t *testing.T) {
	dir := t.TempDir()
	paths := make([]string, 0, 3)
	for _, name := range []string{"imported-a", "imported-b", "imported-c"} {
		paths = append(paths, keptBundle(t, dir, name, 100))
	}

	k := keptSizes{bytes: map[string]int64{}}
	k.total(paths)

	// Emptying the directories would change every answer if they were being
	// re-measured. The remembered sizes are what keep the total at 300.
	for _, path := range paths {
		if err := os.Remove(filepath.Join(path, "components", "blocks.bin")); err != nil {
			t.Fatal(err)
		}
	}
	if got := k.total(paths); got != 300 {
		t.Fatalf("total %d after emptying the bundles, want the remembered 300", got)
	}
}
