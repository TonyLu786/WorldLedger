package bundle

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/worldledger/worldledger-mc/internal/archive"
)

// Components are stored several at a time. Two things have to stay true of
// that: the answer is the same as the loop it replaced, and so is the
// complaint when a bundle is wrong.

func manyComponentBundle(t *testing.T, dir string, count int, broken bool) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "components"), 0o755); err != nil {
		t.Fatal(err)
	}
	components := map[string]any{}
	for i := 0; i < count; i++ {
		payload := []byte(fmt.Sprintf("component %d of a capture", i))
		rel := fmt.Sprintf("components/part%02d.bin", i)
		if err := os.WriteFile(filepath.Join(dir, rel), payload, 0o644); err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(payload)
		digest := hex.EncodeToString(sum[:])
		size := len(payload)
		if broken && i == 3 {
			// A digest that does not match the bytes, in the middle, so a
			// worker other than the first is the one that finds it.
			digest = hex.EncodeToString(make([]byte, 32))
		}
		components[fmt.Sprintf("mcjava.part.%02d", i)] = map[string]any{
			"path": rel, "algorithm": "sha256", "digest": digest, "size": size,
		}
	}
	body, err := json.Marshal(map[string]any{
		"schema": Schema, "server_id": "example.org:25565",
		"dimension":   "minecraft:overworld",
		"chunk":       map[string]any{"x": 7, "z": -3},
		"observed_at": time.Date(2026, 9, 25, 9, 0, 0, 0, time.UTC).Format(time.RFC3339Nano),
		"protocol":    "770",
		"source":      map[string]any{"contributor": "alice"},
		"components":  components,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bundle.json"), body, 0o644); err != nil {
		t.Fatal(err)
	}
}

func importWith(t *testing.T, workers, components int, broken bool) (Result, error) {
	t.Helper()
	previous := forcedImportWorkers
	forcedImportWorkers = workers
	defer func() { forcedImportWorkers = previous }()

	root := t.TempDir()
	a, err := archive.Init(filepath.Join(root, "archive"))
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "ready-one")
	manyComponentBundle(t, dir, components, broken)
	return Import(a, dir, Options{Limits: DefaultLimits()})
}

func TestStoringSeveralAtOnceGivesTheSameObservation(t *testing.T) {
	sequential, err := importWith(t, 1, 30, false)
	if err != nil {
		t.Fatalf("sequential: %v", err)
	}
	parallel, err := importWith(t, 4, 30, false)
	if err != nil {
		t.Fatalf("parallel: %v", err)
	}
	if sequential.ObservationID != parallel.ObservationID {
		t.Errorf("identity differs: %s sequential, %s parallel",
			sequential.ObservationID[:12], parallel.ObservationID[:12])
	}
	if sequential.StateDigest != parallel.StateDigest {
		t.Error("the state digest differs between the two paths")
	}
	if sequential.Components != 30 || parallel.Components != 30 {
		t.Errorf("components: %d sequential, %d parallel", sequential.Components, parallel.Components)
	}
}

// A bundle with a bad component must produce one message, and the same one,
// however the workers were scheduled. Otherwise the same broken bundle reports
// differently run to run, which is the kind of thing that makes somebody doubt
// the tool rather than the bundle.
func TestABadComponentIsReportedTheSameWayEveryTime(t *testing.T) {
	_, sequentialErr := importWith(t, 1, 30, true)
	if sequentialErr == nil {
		t.Fatal("a bundle with a mismatched digest imported")
	}
	for attempt := 0; attempt < 8; attempt++ {
		_, parallelErr := importWith(t, 4, 30, true)
		if parallelErr == nil {
			t.Fatal("a bundle with a mismatched digest imported in parallel")
		}
		if parallelErr.Error() != sequentialErr.Error() {
			t.Fatalf("attempt %d reported %q; sequential reports %q",
				attempt, parallelErr.Error(), sequentialErr.Error())
		}
	}
}

// One component must not start a worker pool for itself.
func TestASingleComponentTakesTheSimplePath(t *testing.T) {
	if _, err := importWith(t, 4, 1, false); err != nil {
		t.Fatalf("a one-component bundle failed: %v", err)
	}
}
