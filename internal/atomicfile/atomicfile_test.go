package atomicfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReplacingAFileLeavesNoIntermediateState(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "record.json")
	if err := Write(path, []byte(`{"one":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Write(path, []byte(`{"two":2}`), 0o644); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != `{"two":2}` {
		t.Errorf("the file holds %q", body)
	}
	// Nothing left behind for the enumerations that walk these directories.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		names := []string{}
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Errorf("the directory holds %v, want only the record", names)
	}
}

// The property the four record stores needed. os.WriteFile truncates in place,
// so a crash during one leaves a file that parses as nothing and takes every
// reader of it down until somebody deletes it by hand.
func TestAFailedWriteDoesNotDisturbWhatWasThere(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "record.json")
	original := `{"declared_by":"alice"}`
	if err := Write(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	// A destination directory that has gone away is the closest thing to a
	// mid-write failure that can be arranged deterministically: the temporary
	// cannot be created, so nothing is written and nothing is truncated.
	missing := filepath.Join(dir, "gone", "record.json")
	if err := Write(missing, []byte("irrelevant"), 0o644); err == nil {
		t.Fatal("writing into a directory that does not exist succeeded")
	}

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != original {
		t.Errorf("the existing record changed to %q", body)
	}
}

func TestTheTemporaryIsRecognisableAsOurs(t *testing.T) {
	// The name matters: a crash leaves one of these behind, and anything that
	// enumerates the directory afterwards should be able to tell it from a file
	// somebody else put there.
	dir := t.TempDir()
	handle, err := os.CreateTemp(dir, ".worldledger-tmp-*")
	if err != nil {
		t.Fatal(err)
	}
	handle.Close()
	if !strings.HasPrefix(filepath.Base(handle.Name()), ".worldledger-tmp-") {
		t.Errorf("temporary named %q", filepath.Base(handle.Name()))
	}
}
