package health

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/worldledger/worldledger-mc/internal/mcpath"
)

// Every case here is a state a real machine is in at some point. The one that
// matters most is the half-installed one: a red cross that does not distinguish
// "not there" from "there but for another Minecraft" sends somebody to install
// what they already have.

func fixture(t *testing.T) mcpath.Install {
	t.Helper()
	return mcpath.Install{Root: t.TempDir()}
}

func writeVersion(t *testing.T, install mcpath.Install, name, body string) {
	t.Helper()
	dir := filepath.Join(install.Versions(), name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name+".json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeMod(t *testing.T, install mcpath.Install, name string) {
	t.Helper()
	if err := os.MkdirAll(install.Mods(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(install.Mods(), name), []byte("jar"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeCaptureProperties(t *testing.T, install mcpath.Install, body string) {
	t.Helper()
	path := install.CaptureProperties()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func find(t *testing.T, report Report, id string) Check {
	t.Helper()
	for _, c := range report.Checks {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("no check with id %q in %+v", id, report.Checks)
	return Check{}
}

// A ready machine, so the rest of the tests are known to be failing for the
// reason they name rather than because nothing ever passes.
func TestAFullySetUpMachineIsReady(t *testing.T) {
	install := fixture(t)
	writeVersion(t, install, MinecraftVersion, `{"id":"`+MinecraftVersion+`","type":"release"}`)
	writeVersion(t, install, "fabric-loader-"+LoaderVersion+"-"+MinecraftVersion,
		`{"id":"fabric-loader-`+LoaderVersion+`-`+MinecraftVersion+`","inheritsFrom":"`+MinecraftVersion+`"}`)
	writeMod(t, install, "fabric-api-"+FabricAPIVersion+".jar")
	writeMod(t, install, "worldledger-0.2.0.jar")
	writeCaptureProperties(t, install, "contributor=alice\n")

	report := Inspect(install)
	if !report.Ready {
		for _, c := range report.Checks {
			if c.State != OK {
				t.Errorf("%s: %s (%s)", c.ID, c.State, c.Detail)
			}
		}
		t.Fatal("a machine with everything in place was not reported ready")
	}
}

// Nothing else can be answered without this, and it is the one thing the
// application must not offer to fix.
func TestNoMinecraftStopsAndOffersNothing(t *testing.T) {
	install := mcpath.Install{Root: filepath.Join(t.TempDir(), "absent")}
	report := Inspect(install)
	if report.Ready {
		t.Fatal("a machine with no Minecraft was reported ready")
	}
	if len(report.Checks) != 1 {
		t.Fatalf("got %d checks; with no Minecraft there is nothing further to report", len(report.Checks))
	}
	if fix := report.Checks[0].Fix; fix != "" {
		t.Errorf("the application offered to fix a missing Minecraft: %q", fix)
	}
}

// The distinction this whole report exists for. A loader installed for another
// release is not a missing loader, and telling somebody to install what they
// have is how they conclude the application is broken.
func TestALoaderForAnotherReleaseIsWrongRatherThanMissing(t *testing.T) {
	install := fixture(t)
	writeVersion(t, install, MinecraftVersion, `{"id":"`+MinecraftVersion+`"}`)
	writeVersion(t, install, "fabric-loader-0.16.0-1.21.11",
		`{"id":"fabric-loader-0.16.0-1.21.11","inheritsFrom":"1.21.11"}`)

	loader := find(t, Inspect(install), "loader")
	if loader.State != Wrong {
		t.Fatalf("state = %q, want %q", loader.State, Wrong)
	}
	if loader.Detail == "" || loader.Fix == "" {
		t.Errorf("a wrong loader has to say what is wrong and what would be done: %+v", loader)
	}
}

func TestAMissingLoaderIsOfferedAsSomethingToInstall(t *testing.T) {
	install := fixture(t)
	writeVersion(t, install, MinecraftVersion, `{"id":"`+MinecraftVersion+`"}`)

	loader := find(t, Inspect(install), "loader")
	if loader.State != Missing {
		t.Fatalf("state = %q, want %q", loader.State, Missing)
	}
	if loader.Fix == "" {
		t.Error("a missing loader offered nothing to do about it")
	}
}

// The release the mod targets is installed by the launcher, not by this. An
// offer to do it would be a button that cannot work.
func TestAMissingReleaseNamesWhatIsInstalledAndOffersNoButton(t *testing.T) {
	install := fixture(t)
	writeVersion(t, install, "1.21.11", `{"id":"1.21.11","type":"release"}`)

	release := find(t, Inspect(install), "release")
	if release.State == OK {
		t.Fatal("a machine without the target release was reported as having it")
	}
	if release.Fix != "" {
		t.Errorf("the application offered to install a Minecraft release: %q", release.Fix)
	}
	if !strings.Contains(release.Detail, "1.21.11") {
		t.Errorf("the detail does not say what is installed instead: %q", release.Detail)
	}
}

// An empty contributor is the single most likely reason to play an evening and
// find nothing recorded, so it cannot read the same as a missing file.
func TestAnEmptyContributorIsReportedEvenThoughTheFileExists(t *testing.T) {
	install := fixture(t)
	writeVersion(t, install, MinecraftVersion, `{"id":"`+MinecraftVersion+`"}`)
	writeCaptureProperties(t, install, "# WorldLedger capture adapter\ncontributor=\nqueue_capacity=32\n")

	contributor := find(t, Inspect(install), "contributor")
	if contributor.State != Missing {
		t.Fatalf("state = %q, want %q", contributor.State, Missing)
	}
}

func TestAContributorWithSurroundingSpaceIsStillASetName(t *testing.T) {
	install := fixture(t)
	writeCaptureProperties(t, install, "  contributor =  alice  \n")

	contributor := find(t, Inspect(install), "contributor")
	if contributor.State != OK {
		t.Fatalf("state = %q, want %q (detail %q)", contributor.State, OK, contributor.Detail)
	}
	if contributor.Detail != "alice" {
		t.Errorf("detail = %q, want the trimmed name", contributor.Detail)
	}
}

func TestACommentedContributorIsNotASetName(t *testing.T) {
	install := fixture(t)
	writeCaptureProperties(t, install, "#contributor=alice\n")

	if contributor := find(t, Inspect(install), "contributor"); contributor.State == OK {
		t.Fatal("a commented-out contributor was read as set")
	}
}

// A version directory whose manifest is unreadable must not take the rest of
// the report down with it.
func TestAnUnreadableVersionManifestDoesNotStopTheReport(t *testing.T) {
	install := fixture(t)
	writeVersion(t, install, MinecraftVersion, `{"id":"`+MinecraftVersion+`"}`)
	writeVersion(t, install, "broken", "this is not json")

	report := Inspect(install)
	if find(t, report, "release").State != OK {
		t.Error("a broken manifest elsewhere hid an installed release")
	}
	if len(report.Checks) < 6 {
		t.Errorf("got %d checks, want the full report", len(report.Checks))
	}
}
