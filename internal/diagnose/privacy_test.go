package diagnose

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/worldledger/worldledger-mc/internal/archive"
	"github.com/worldledger/worldledger-mc/internal/model"
)

// This package exists to be handed to somebody else, so the only test that
// matters is what it refuses to carry.
//
// It is written as a denial rather than as a list of fields. A list of fields
// passes for as long as nobody adds one; putting real names into a real archive
// and then looking for them in the output fails the moment anything starts
// carrying them, including something added years from now by somebody who never
// read this file.

const (
	serverName      = "veryparticularservername.example:25565"
	contributorName = "AVeryParticularContributorName"
	dimensionName   = "customnamespace:averyparticulardimension"
)

func archiveWithRealNames(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	a, err := archive.Init(dir)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := a.CAS.Put(bytes.NewReader([]byte("some canonical bytes nobody else should see")))
	if err != nil {
		t.Fatal(err)
	}
	o := model.Observation{
		Chunk: model.ChunkRef{
			ServerID:  serverName,
			Dimension: dimensionName,
			X:         1234,
			Z:         -5678,
		},
		ObservedAt: time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC),
		Protocol:   "770",
		Source:     model.Source{Contributor: contributorName},
		Components: map[string]model.BlobRef{"blocks": ref},
	}
	if err := o.Finalize(); err != nil {
		t.Fatal(err)
	}
	if err := a.AddObservation(o); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestNothingAPersonObservedLeavesInADiagnosis(t *testing.T) {
	dir := archiveWithRealNames(t)

	encoded, err := json.Marshal(Take("0.3.0", dir, ""))
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)

	for _, secret := range []string{serverName, contributorName, dimensionName} {
		if strings.Contains(text, secret) {
			t.Errorf("a diagnosis carries %q, which is the archive's subject matter", secret)
		}
	}
	// The coordinates of a chunk somebody visited are the same kind of thing as
	// its name.
	for _, coordinate := range []string{"1234", "5678"} {
		if strings.Contains(text, coordinate) {
			t.Errorf("a diagnosis carries the chunk coordinate %s", coordinate)
		}
	}

	// And it still has to be worth sending.
	var report Report
	if err := json.Unmarshal(encoded, &report); err != nil {
		t.Fatal(err)
	}
	if report.Archive == nil {
		t.Fatal("the archive was not described at all")
	}
	if report.Archive.Observations != 1 || report.Archive.Servers != 1 {
		t.Errorf("counts are wrong: %d observation(s), %d server(s)",
			report.Archive.Observations, report.Archive.Servers)
	}
	if report.Tool.Version != "0.3.0" {
		t.Errorf("the tool version is %q", report.Tool.Version)
	}
}

// An integrity failure is the case somebody most wants help with, and its
// messages carry the identifiers of what failed.
func TestTheKindsOfFailureSurviveAndTheIdentifiersDoNot(t *testing.T) {
	kinds := kindsOf([]string{
		"object 4f2a9c1b8e7d6a5b4c3d2e1f0a9b8c7d6e5f4a3b2c1d0e9f8a7b6c5d4e3f2a1b is corrupt",
		"object 9c11ffeeddccbbaa99887766554433221100ffeeddccbbaa9988776655443322 is corrupt",
		"observation aabbccddeeff00112233445566778899aabbccddeeff001122334455667788 is missing from its chunk index",
	})

	if len(kinds) != 2 {
		t.Errorf("got %d kinds, want 2: %v", len(kinds), kinds)
	}
	joined := strings.Join(kinds, "\n")
	if !strings.Contains(joined, "is corrupt") || !strings.Contains(joined, "missing from its chunk index") {
		t.Errorf("the kinds were lost: %v", kinds)
	}
	if strings.Contains(joined, "4f2a9c1b") || strings.Contains(joined, "aabbccdd") {
		t.Errorf("an identifier survived: %v", kinds)
	}
}

// A path is useful and on Windows it carries the account name.
func TestAPathKeepsItsShapeAndLosesTheName(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("no home directory on this machine, which is the case masking is for")
	}
	inside := filepath.Join(home, "AppData", "Roaming", ".minecraft")

	masked := maskPath(inside)
	if strings.Contains(masked, home) {
		t.Errorf("the home directory survived: %q", masked)
	}
	if !strings.Contains(masked, ".minecraft") {
		t.Errorf("the shape was lost, so the path stopped being useful: %q", masked)
	}
}

// Ordinary words and small numbers are not identifiers, and a masker that ate
// them would make the kinds unreadable.
func TestMaskingLeavesOrdinaryTextAlone(t *testing.T) {
	for _, text := range []string{
		"index references missing observation",
		"chunk 12,34 is not in its dimension",
		"deadbeef is short enough to be a word",
	} {
		if got := hexMasked(text); got != text {
			t.Errorf("hexMasked(%q) = %q; ordinary text was masked", text, got)
		}
	}
}

// The case that was found by running it rather than by reading it. Windows
// keeps a short form for a name with a space in it, so the same directory
// arrives as a different string and a substring replacement never matches.
func TestTheAccountNameGoesInEveryFormItArrivesIn(t *testing.T) {
	for _, path := range []string{
		`C:\Users\Juntong Lu\AppData\Roaming\.minecraft`,
		"C:/Users/JUNTON~1/AppData/Local/Temp/somewhere",
		`c:\users\SomebodyElse\Desktop\archive`,
		"/home/someone/.minecraft/config",
	} {
		masked := maskPath(path)
		for _, name := range []string{"Juntong", "JUNTON~1", "SomebodyElse"} {
			if strings.Contains(masked, name) {
				t.Errorf("maskPath(%q) = %q, which still names %q", path, masked, name)
			}
		}
	}
}

// And the shape has to survive, or the path stops being worth carrying.
func TestMaskingKeepsWhatMakesAPathUseful(t *testing.T) {
	masked := maskPath("C:/Users/JUNTON~1/AppData/Roaming/.minecraft/config/worldledger/spool")
	for _, kept := range []string{"AppData", ".minecraft", "worldledger", "spool"} {
		if !strings.Contains(masked, kept) {
			t.Errorf("masking removed %q, which is the part that helps: %q", kept, masked)
		}
	}
}

// A path with nothing after Users must not send the masker round forever.
func TestAPathThatEndsAtUsersTerminates(t *testing.T) {
	done := make(chan string, 1)
	go func() { done <- maskPath(`C:\Users\`) }()
	select {
	case got := <-done:
		if strings.Contains(got, "<user>") {
			t.Errorf("an empty segment was masked as a name: %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("maskPath did not terminate on a path that ends at Users")
	}
}
