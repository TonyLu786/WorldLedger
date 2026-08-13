package archive

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/worldledger/worldledger-mc/internal/model"
)

func TestSafeIndexComponentsDoNotCollapseCommonSeparators(t *testing.T) {
	cases := [][2]string{
		{"a/b", "a_b"},
		{"a:b", "a_b"},
		{"a\\b", "a_b"},
	}
	for _, pair := range cases {
		if safe(pair[0]) == safe(pair[1]) {
			t.Fatalf("index encoding collision between %q and %q", pair[0], pair[1])
		}
	}
}

func TestAddObservationRejectsMisfiledExistingObservation(t *testing.T) {
	a, err := Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	original := testObservation(t, "alice")
	if err := a.AddObservation(original); err != nil {
		t.Fatal(err)
	}
	other := testObservation(t, "bob")
	data, err := json.Marshal(other)
	if err != nil {
		t.Fatal(err)
	}
	originalPath := filepath.Join(a.Root, "observations", original.ID[:2], original.ID+".json")
	if err := os.WriteFile(originalPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := a.AddObservation(original); err == nil || !strings.Contains(err.Error(), "contains id") {
		t.Fatalf("expected misfiled observation rejection, got %v", err)
	}
}

func TestInitNeverRewritesAnExistingArchiveVersion(t *testing.T) {
	root := t.TempDir()
	a, err := Init(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Init(root); err != nil {
		t.Fatalf("initializing the current archive twice should be idempotent: %v", err)
	}
	if a.Root != root {
		t.Fatalf("archive root = %q; want %q", a.Root, root)
	}

	versionPath := filepath.Join(root, "VERSION")
	const incompatible = "worldledger-future-format\n"
	if err := os.WriteFile(versionPath, []byte(incompatible), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Init(root); err == nil {
		t.Fatal("expected incompatible existing archive to be rejected")
	}
	data, err := os.ReadFile(versionPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != incompatible {
		t.Fatalf("incompatible VERSION was rewritten: %q", data)
	}
}

func TestOpenRecoversObservationTransactions(t *testing.T) {
	phases := []string{"journal-only", "observation-written", "index-written"}
	for _, phase := range phases {
		t.Run(phase, func(t *testing.T) {
			root := t.TempDir()
			a, err := Init(root)
			if err != nil {
				t.Fatal(err)
			}
			ref, err := a.CAS.Put(strings.NewReader("transaction fixture"))
			if err != nil {
				t.Fatal(err)
			}
			o := testObservation(t, "alice")
			o.Components = map[string]model.BlobRef{"chunk": ref}
			if err := o.Finalize(); err != nil {
				t.Fatal(err)
			}
			data, err := json.MarshalIndent(o, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			data = append(data, '\n')
			if err := writeAtomic(a.transactionPath(o.ID), data); err != nil {
				t.Fatal(err)
			}
			if phase == "observation-written" || phase == "index-written" {
				if err := a.commitObservation(o, data); err != nil {
					t.Fatal(err)
				}
			}
			if phase == "index-written" {
				if err := a.commitIndex(o); err != nil {
					t.Fatal(err)
				}
			}

			reopened, err := Open(root)
			if err != nil {
				t.Fatal(err)
			}
			observations, err := reopened.Observations(o.Chunk)
			if err != nil {
				t.Fatal(err)
			}
			if len(observations) != 1 || observations[0].ID != o.ID {
				t.Fatalf("transaction was not recovered: %#v", observations)
			}
			if _, err := os.Stat(reopened.transactionPath(o.ID)); !os.IsNotExist(err) {
				t.Fatalf("completed transaction was not removed: %v", err)
			}
			report := reopened.Check()
			if len(report.Errors) != 0 || report.Observations != 1 || report.Objects != 1 {
				t.Fatalf("recovered archive is not clean: %#v", report)
			}
		})
	}
}

func TestAtomicIndexReplacementPreservesExistingEntries(t *testing.T) {
	a, err := Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ref, err := a.CAS.Put(strings.NewReader("shared state"))
	if err != nil {
		t.Fatal(err)
	}
	first := testObservation(t, "alice")
	first.Components = map[string]model.BlobRef{"chunk": ref}
	if err := first.Finalize(); err != nil {
		t.Fatal(err)
	}
	second := testObservation(t, "bob")
	second.Components = map[string]model.BlobRef{"chunk": ref}
	if err := second.Finalize(); err != nil {
		t.Fatal(err)
	}
	if err := a.AddObservation(first); err != nil {
		t.Fatal(err)
	}
	if err := a.AddObservation(second); err != nil {
		t.Fatal(err)
	}
	ids, err := readIndexIDs(a.chunkIndexPath(first.Chunk))
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 || ids[0] != first.ID || ids[1] != second.ID {
		t.Fatalf("unexpected index entries: %#v", ids)
	}
	report := a.Check()
	if len(report.Errors) != 0 || report.Observations != 2 || report.Objects != 1 {
		t.Fatalf("archive is not clean: %#v", report)
	}
}

func TestConcurrentObservationCommitsPreserveEveryIndexEntry(t *testing.T) {
	a, err := Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ref, err := a.CAS.Put(strings.NewReader("concurrent shared state"))
	if err != nil {
		t.Fatal(err)
	}
	const count = 32
	observations := make([]model.Observation, count)
	for index := range observations {
		o := testObservation(t, fmt.Sprintf("contributor-%02d", index))
		o.Components = map[string]model.BlobRef{"chunk": ref}
		if err := o.Finalize(); err != nil {
			t.Fatal(err)
		}
		observations[index] = o
	}

	start := make(chan struct{})
	errors := make(chan error, count)
	var workers sync.WaitGroup
	for _, observation := range observations {
		observation := observation
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			errors <- a.AddObservation(observation)
		}()
	}
	close(start)
	workers.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}

	stored, err := a.Observations(observations[0].Chunk)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != count {
		t.Fatalf("concurrent commits produced %d indexed observations; want %d", len(stored), count)
	}
	report := a.Check()
	if len(report.Errors) != 0 || report.Observations != count || report.Objects != 1 {
		t.Fatalf("concurrent archive is not clean: %#v", report)
	}
}

func TestConcurrentProcessesPreserveEveryIndexEntry(t *testing.T) {
	a, err := Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	const count = 12
	type childProcess struct {
		command *exec.Cmd
		output  bytes.Buffer
	}
	children := make([]childProcess, count)
	for index := range children {
		command := exec.Command(os.Args[0], "-test.run=^TestArchiveLockSubprocessHelper$")
		command.Env = append(os.Environ(),
			"WORLDLEDGER_ARCHIVE_LOCK_HELPER=1",
			"WORLDLEDGER_ARCHIVE_LOCK_ROOT="+a.Root,
			"WORLDLEDGER_ARCHIVE_LOCK_INDEX="+strconv.Itoa(index),
		)
		children[index].command = command
		command.Stdout = &children[index].output
		command.Stderr = &children[index].output
		if err := command.Start(); err != nil {
			t.Fatal(err)
		}
	}
	for index := range children {
		if err := children[index].command.Wait(); err != nil {
			t.Fatalf("child %d failed: %v\n%s", index, err, children[index].output.String())
		}
	}

	stored, err := a.Observations(model.ChunkRef{
		ServerID: "example.org", Dimension: "minecraft:overworld", X: 3, Z: -7,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != count {
		t.Fatalf("concurrent processes produced %d indexed observations; want %d", len(stored), count)
	}
	report := a.Check()
	if len(report.Errors) != 0 || report.Observations != count || report.Objects != 1 {
		t.Fatalf("cross-process archive is not clean: %#v", report)
	}
}

func TestArchiveLockSubprocessHelper(t *testing.T) {
	if os.Getenv("WORLDLEDGER_ARCHIVE_LOCK_HELPER") != "1" {
		return
	}
	index, err := strconv.Atoi(os.Getenv("WORLDLEDGER_ARCHIVE_LOCK_INDEX"))
	if err != nil {
		t.Fatal(err)
	}
	a, err := Open(os.Getenv("WORLDLEDGER_ARCHIVE_LOCK_ROOT"))
	if err != nil {
		t.Fatal(err)
	}
	ref, err := a.CAS.Put(strings.NewReader("cross-process shared state"))
	if err != nil {
		t.Fatal(err)
	}
	o := model.Observation{
		Chunk: model.ChunkRef{
			ServerID: "example.org", Dimension: "minecraft:overworld", X: 3, Z: -7,
		},
		ObservedAt: time.Date(2026, 8, 9, 12, 0, index, 0, time.UTC),
		Protocol:   "test/v1",
		Source:     model.Source{Contributor: fmt.Sprintf("process-%02d", index)},
		Components: map[string]model.BlobRef{"chunk": ref},
	}
	if err := a.AddObservation(o); err != nil {
		t.Fatal(err)
	}
}

func TestCheckDetectsUnindexedObservation(t *testing.T) {
	a, err := Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	o := testObservation(t, "alice")
	data, err := json.Marshal(o)
	if err != nil {
		t.Fatal(err)
	}
	obsDir := filepath.Join(a.Root, "observations", o.ID[:2])
	if err := os.MkdirAll(obsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(obsDir, o.ID+".json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	report := a.Check()
	if len(report.Errors) == 0 || !strings.Contains(strings.Join(report.Errors, "\n"), "missing from its chunk index") {
		t.Fatalf("fsck did not detect unindexed observation: %#v", report)
	}
}

func testObservation(t *testing.T, contributor string) model.Observation {
	t.Helper()
	o := model.Observation{
		Chunk:      model.ChunkRef{ServerID: "example.org", Dimension: "overworld", X: 1, Z: 2},
		ObservedAt: time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC),
		Protocol:   "test/v1",
		Source:     model.Source{Contributor: contributor},
		Components: map[string]model.BlobRef{
			"chunk": {Algorithm: "sha256", Digest: strings.Repeat("a", 64), Size: 1},
		},
	}
	if err := o.Finalize(); err != nil {
		t.Fatal(err)
	}
	return o
}
