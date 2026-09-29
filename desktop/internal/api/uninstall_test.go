package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/worldledger/worldledger-mc/desktop/internal/installer"
)

// Remove is the other half of the one thing this application writes into
// somebody's game, and it is only as good as what it says afterwards.
//
// It used to report every file it did not undo as "left alone because it had
// been changed since it was installed", under a banner saying it had removed
// everything. Some of those had been changed, which is the right reason to
// leave a file. Others were files whose original could not be put back, still
// holding what had been installed -- and then the record of what to undo was
// deleted, so pressing Remove again did nothing.

// installedFile writes a file as the installer would have, and returns the
// record it would have kept of it.
func installedFile(t *testing.T, path, content, backup string) installer.Record {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(content))
	return installer.Record{Path: path, Backup: backup, Digest: hex.EncodeToString(digest[:])}
}

func (m machine) record() string { return filepath.Join(m.root, "worldledger", "installed.json") }

func (m machine) keepRecord(t *testing.T, records ...installer.Record) {
	t.Helper()
	body, err := json.Marshal(installer.Manifest{Schema: "test", Root: m.minecraft.Root, Records: records})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(m.record()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(m.record(), body, 0o644); err != nil {
		t.Fatal(err)
	}
}

func (m machine) heldRecords(t *testing.T) []installer.Record {
	t.Helper()
	body, err := os.ReadFile(m.record())
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var manifest installer.Manifest
	if err := json.Unmarshal(body, &manifest); err != nil {
		t.Fatal(err)
	}
	return manifest.Records
}

func TestRemovingWithNothingInstalledChangesNothing(t *testing.T) {
	api := newAPI(t)
	api.machine.installMinecraft(t)

	got := api.ask(http.MethodPost, "/api/uninstall", nil)
	if got.status != http.StatusNotFound {
		t.Fatalf("status %d, want %d: %s", got.status, http.StatusNotFound, got.raw)
	}
	if got.problem() == "" || got.text("next") == "" {
		t.Errorf("it does not say what happened and what that means: %s", got.raw)
	}
}

// A file that could not be put back is reported as that, the record keeps
// exactly it, and a second Remove finishes the job.
func TestAFileThatCouldNotBePutBackIsSaidAndCanBeTriedAgain(t *testing.T) {
	api := newAPI(t)
	api.machine.installMinecraft(t)
	mods := api.machine.minecraft.Mods()

	undoable := installedFile(t, filepath.Join(mods, "worldledger.jar"), "installed mod", "")
	// This one replaced a file somebody already had, and the copy kept of theirs
	// is missing, so it cannot be put back yet.
	kept := filepath.Join(api.machine.root, "worldledger", "backups", "fabric-api.jar")
	replaced := installedFile(t, filepath.Join(mods, "fabric-api.jar"), "installed api", kept)
	api.machine.keepRecord(t, undoable, replaced)

	got := api.ask(http.MethodPost, "/api/uninstall", nil)
	if got.status != http.StatusOK {
		t.Fatalf("status %d: %s", got.status, got.raw)
	}
	if complete, _ := got.body["complete"].(bool); complete {
		t.Errorf("it says it finished while a file was not put back: %s", got.raw)
	}
	skipped, _ := got.body["skipped"].([]any)
	if len(skipped) != 1 {
		t.Fatalf("skipped %v, want the one file", skipped)
	}
	entry, _ := skipped[0].(map[string]any)
	if entry["changed"] != false || entry["path"] != replaced.Path || entry["reason"] == "" {
		t.Errorf("the file that could not be put back is not reported as that: %v", entry)
	}
	if _, err := os.Stat(undoable.Path); !os.IsNotExist(err) {
		t.Error("the file that could be removed was not")
	}

	held := api.machine.heldRecords(t)
	if len(held) != 1 || held[0].Path != replaced.Path {
		t.Fatalf("the record keeps %v, want only what is still to undo", held)
	}

	// The copy turns up -- security software let go of it, say -- and Remove
	// is pressed again.
	if err := os.MkdirAll(filepath.Dir(kept), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(kept, []byte("their own api"), 0o644); err != nil {
		t.Fatal(err)
	}
	again := api.ask(http.MethodPost, "/api/uninstall", nil)
	if complete, _ := again.body["complete"].(bool); !complete {
		t.Fatalf("the second attempt did not finish: %s", again.raw)
	}
	if skipped, _ := again.body["skipped"].([]any); len(skipped) != 0 {
		t.Errorf("the second attempt reported %v; everything it was asked to do it did", skipped)
	}
	if current, _ := os.ReadFile(replaced.Path); string(current) != "their own api" {
		t.Errorf("their file was not put back: %q", current)
	}
	if held := api.machine.heldRecords(t); held != nil {
		t.Errorf("the record outlived a finished uninstall: %v", held)
	}
}

// A file somebody changed after it was installed is theirs, and leaving it is
// the job done, not a job half done.
func TestAFileSomebodyChangedIsLeftAndTheJobIsDone(t *testing.T) {
	api := newAPI(t)
	api.machine.installMinecraft(t)

	properties := installedFile(t, api.machine.minecraft.CaptureProperties(), "contributor=\n", "")
	api.machine.keepRecord(t, properties)
	if err := os.WriteFile(properties.Path, []byte("contributor=someone\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := api.ask(http.MethodPost, "/api/uninstall", nil)
	if complete, _ := got.body["complete"].(bool); !complete {
		t.Fatalf("leaving somebody's own file was treated as unfinished: %s", got.raw)
	}
	skipped, _ := got.body["skipped"].([]any)
	if len(skipped) != 1 {
		t.Fatalf("skipped %v, want the one changed file", skipped)
	}
	if entry, _ := skipped[0].(map[string]any); entry["changed"] != true {
		t.Errorf("the changed file is not reported as changed: %s", got.raw)
	}
	if current, _ := os.ReadFile(properties.Path); string(current) != "contributor=someone\n" {
		t.Errorf("their edit was not left alone: %q", current)
	}
	if held := api.machine.heldRecords(t); held != nil {
		t.Errorf("a finished uninstall kept a record: %v", held)
	}
}
