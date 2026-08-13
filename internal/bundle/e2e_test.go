package bundle

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/worldledger/worldledger-mc/internal/model"
)

func TestCommittedFabricBundleImportsEndToEnd(t *testing.T) {
	spool := filepath.Join("..", "..", "testdata", "e2e-capture-bundle", "spool")
	entries, err := os.ReadDir(spool)
	if err != nil {
		t.Fatal(err)
	}
	var ready string
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), "ready-") {
			if ready != "" {
				t.Fatal("committed end-to-end spool contains multiple ready bundles")
			}
			ready = filepath.Join(spool, entry.Name())
		}
	}
	if ready == "" {
		t.Fatal("committed end-to-end spool contains no ready bundle")
	}

	a := newArchive(t)
	result, err := Import(a, ready, Options{})
	if err != nil {
		t.Fatal(err)
	}
	// The observation id changed when the identity preimage moved from an
	// RFC 3339 timestamp string to integer seconds and nanoseconds, so that two
	// implementations cannot disagree over trailing zeros in the fractional
	// part. The state digest is unchanged, which is the expected blast radius:
	// it never included the timestamp. See spec/observation-v1.md.
	if result.ObservationID != "f6c2788d9899fe1db4bf05755bf37c9280f07ab3851363fb74043fb3c629b819" ||
		result.StateDigest != "a56c43589292429b80bb3d6b00c1bbf64e9600c83e5be5f34091cc3cb61ebc91" ||
		result.Components != 4 {
		t.Fatalf("unexpected committed bundle identity: %#v", result)
	}

	duplicate, err := Import(a, ready, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if duplicate != result {
		t.Fatalf("duplicate end-to-end import changed identity: first=%#v second=%#v", result, duplicate)
	}
	observations, err := a.Observations(model.ChunkRef{
		ServerID:  "example.org:25565",
		Dimension: "minecraft:overworld",
		X:         14,
		Z:         -8,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(observations) != 1 || observations[0].ID != result.ObservationID {
		t.Fatalf("unexpected imported observations: %#v", observations)
	}
	wantDigests := map[string]string{
		"mcjava.shape":          "c407dde4e324670d3f9f76af08b9a86dff9d09f0a08de92b1e1174ea043a5790",
		"mcjava.blocks.-4":      "ed0cd3cbaa8a1165700c575cfaffe8cf3f58bee5602a3ba5ab3c0db78ea7f49f",
		"mcjava.biomes.-4":      "5a8e16fe0cd235992d0189e9a894d47bd8455c6c963329821524b9d9b7e95b07",
		"mcjava.block_entities": "8966ca92974146f8d58c2c3337de38c3b8f38fdda4c6d45595c37311b8727a77",
	}
	for name, want := range wantDigests {
		if got := observations[0].Components[name].Digest; got != want {
			t.Errorf("component %q digest = %q; want %q", name, got, want)
		}
	}
	assertArchiveClean(t, a, 1, 4)
}
