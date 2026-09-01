package archive

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/worldledger/worldledger-mc/internal/model"
)

// A purge journal is read off disk and replayed by every command, because every
// command opens the archive. Its ids were validated on exactly this reasoning
// and its object references were not, and a reference is what reaches os.Remove.
func TestAJournalNamingSomethingThatIsNotAnObjectIsRefused(t *testing.T) {
	dir := t.TempDir()
	if _, err := Init(dir); err != nil {
		t.Fatal(err)
	}
	// Somewhere outside the archive, which is the whole point.
	outside := filepath.Join(filepath.Dir(dir), "not-part-of-any-archive.txt")
	if err := os.WriteFile(outside, []byte("somebody else's file"), 0o644); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(outside)

	escape := strings.Repeat("../", 6) + "not-part-of-any-archive.txt"
	journalDir := filepath.Join(dir, purgeDirectory)
	if err := os.MkdirAll(journalDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(purgeJournal{
		IDs:  []string{},
		Refs: map[string]model.BlobRef{escape: {Algorithm: "sha256", Digest: escape, Size: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(journalDir, "pending.json"), body, 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := Open(dir); err == nil {
		t.Error("a journal naming a path instead of a digest was replayed")
	}
	if _, err := os.Stat(outside); err != nil {
		t.Errorf("opening the archive deleted a file outside it: %v", err)
	}
}
