package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/worldledger/worldledger-mc/internal/archive"
	"github.com/worldledger/worldledger-mc/internal/redact"
)

// A fingerprint and a manifest are written to be given to somebody, and neither
// applies the archive's redactions. That is deliberate and it is a disclosure,
// and the only place it was written down was a design document nobody exporting
// a fingerprint has open.

func TestAnArchiveWithNothingWithheldSaysNothing(t *testing.T) {
	if note := handOffDisclosure("fingerprint", 0); note != "" {
		t.Errorf("an archive with no redactions was given a warning: %q", note)
	}
}

func TestTheDisclosureNamesWhatWasDeclaredAndWhatItAffects(t *testing.T) {
	note := handOffDisclosure("manifest", 2)
	for _, wanted := range []string{"2 declared redaction", "manifest", "send"} {
		if !strings.Contains(note, wanted) {
			t.Errorf("the note does not mention %q: %q", wanted, note)
		}
	}
	if strings.Contains(note, "fingerprint") {
		t.Errorf("the note names the wrong kind of file: %q", note)
	}
}

func TestTheDisclosureCountsTheArchivesOwnRedactions(t *testing.T) {
	root := t.TempDir()
	if _, err := archive.Init(root); err != nil {
		t.Fatal(err)
	}
	a, err := archive.Open(root)
	if err != nil {
		t.Fatal(err)
	}

	store := redact.NewStore(a.Root)
	for _, contributor := range []string{"alice", "bob"} {
		if _, err := store.Declare(redact.Redaction{
			Schema:      redact.Schema,
			Server:      "example",
			Contributor: contributor,
			Reason:      "consent withdrawn",
			DeclaredBy:  "operator",
			DeclaredAt:  time.Now().UTC(),
		}); err != nil {
			t.Fatal(err)
		}
	}

	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	previous := os.Stderr
	os.Stderr = write
	err = noteWhatAHandOffDiscloses(a, "fingerprint")
	os.Stderr = previous
	write.Close()
	if err != nil {
		t.Fatal(err)
	}

	printed := make([]byte, 1024)
	n, _ := read.Read(printed)
	if !strings.Contains(string(printed[:n]), "2 declared redaction") {
		t.Errorf("the two declared redactions were not reported: %q", string(printed[:n]))
	}
}

// A redaction store that cannot be read is not a reason to carry on. The whole
// point of the note is to say what is not being applied, and a program that
// cannot find that out is about to hand somebody a file it cannot describe.
func TestAnUnreadableRedactionStopsTheHandOff(t *testing.T) {
	root := t.TempDir()
	if _, err := archive.Init(root); err != nil {
		t.Fatal(err)
	}
	a, err := archive.Open(root)
	if err != nil {
		t.Fatal(err)
	}

	store := redact.NewStore(a.Root)
	if _, err := store.Declare(redact.Redaction{
		Schema:     redact.Schema,
		Server:     "example",
		Reason:     "the whole server",
		DeclaredBy: "operator",
		DeclaredAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(store.Root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Fatal("nothing was declared, so this test would prove nothing")
	}
	if err := os.WriteFile(filepath.Join(store.Root, entries[0].Name()), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := noteWhatAHandOffDiscloses(a, "fingerprint"); err == nil {
		t.Fatal("a hand-off went ahead over redactions the program could not read")
	}
}
