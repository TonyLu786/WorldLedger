package api

import (
	"archive/zip"
	"bytes"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/worldledger/worldledger-mc/desktop/internal/health"
	"github.com/worldledger/worldledger-mc/desktop/internal/installer"
)

// Set-up going wrong half way is the case its record exists for, and it was the
// one part of set-up no test here could reach: the handler fetched through the
// real internet, so the only way to have a download fail was to have one fail.

// internet is what installing fetches from in these tests. Every address
// answers with a jar except the ones that are down, and whileDown, if set, is
// what else happens at the moment one of those is asked for.
type internet struct {
	down      map[string]bool
	whileDown func()
}

func (n *internet) Fetch(source string) ([]byte, error) {
	if n.down[source] {
		if n.whileDown != nil {
			n.whileDown()
		}
		return nil, errors.New("connection reset by peer")
	}
	return aJar(), nil
}

// aJar is a real jar with nothing in it, which is all these tests need of one.
// Closing a zip writer over a buffer cannot fail, so there is no error to pass
// on.
func aJar() []byte {
	var jar bytes.Buffer
	_ = zip.NewWriter(&jar).Close()
	return jar.Bytes()
}

// installFabric gives the machine the Minecraft release the mod is built for,
// and Fabric Loader for it. Leaving the loader to the set-up would make the plan
// ask whether the launcher is open, which is a question about the computer
// running the test rather than about the test.
func (m machine) installFabric(t *testing.T) {
	t.Helper()
	m.installMinecraft(t)
	for id, profile := range map[string]string{
		health.MinecraftVersion: `{"id":"` + health.MinecraftVersion + `"}`,
		installer.LoaderVersionID(): `{"id":"` + installer.LoaderVersionID() +
			`","inheritsFrom":"` + health.MinecraftVersion + `"}`,
	} {
		path := m.minecraft.VersionProfile(id)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(profile), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func (m machine) fabricAPI() string {
	return m.minecraft.Mod("fabric-api-" + health.FabricAPIVersion + ".jar")
}

// A set-up that fails part way is undone, and then nothing is recorded, because
// nothing is there.
//
// The record used to keep everything the attempt had done although all of it
// had been undone, and that is worse than untidy. Somebody who gave up on the
// window and fetched Fabric API themselves has the same release under the same
// name, byte for byte, and Remove would have found it in the record, matched
// its digest, and deleted it as a file this application wrote.
func TestASetUpThatFailsIsUndoneAndNothingIsRecorded(t *testing.T) {
	api := newRelease(t, &internet{down: map[string]bool{modAddress: true}})
	api.machine.installFabric(t)

	got := api.ask(http.MethodPost, "/api/install", map[string]string{"contributor": "alice"})
	if got.status != http.StatusInternalServerError {
		t.Fatalf("status %d: %s", got.status, got.raw)
	}
	if !strings.Contains(got.problem(), "WorldLedger mod") {
		t.Errorf("the failure does not say which step it was: %s", got.raw)
	}
	// True only because everything was undone, which is what is checked next.
	if !strings.HasPrefix(got.text("next"), "nothing was changed") {
		t.Errorf("it does not say that nothing was changed: %s", got.raw)
	}
	if _, err := os.Stat(api.machine.fabricAPI()); !os.IsNotExist(err) {
		t.Error("the Fabric API jar the attempt wrote is still there")
	}
	if _, err := os.Stat(api.machine.minecraft.Mods()); !os.IsNotExist(err) {
		t.Error("the mods folder the attempt made is still there")
	}
	if held := api.machine.heldRecords(t); held != nil {
		t.Errorf("an attempt that was undone is still recorded: %v", held)
	}

	if err := os.MkdirAll(api.machine.minecraft.Mods(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(api.machine.fabricAPI(), aJar(), 0o644); err != nil {
		t.Fatal(err)
	}
	if removed := api.ask(http.MethodPost, "/api/uninstall", nil); removed.status != http.StatusNotFound {
		t.Errorf("Remove found something of this application's to undo: %s", removed.raw)
	}
	if _, err := os.Stat(api.machine.fabricAPI()); err != nil {
		t.Errorf("Remove took Fabric API that somebody put there themselves: %v", err)
	}
}

// A second set-up that fails leaves the first one recorded as it was, so Remove
// can still undo all of it.
func TestASecondSetUpThatFailsLeavesTheFirstRecordedAsItWas(t *testing.T) {
	api := newRelease(t, &internet{down: map[string]bool{modAddress: true}})
	api.machine.installFabric(t)
	// The first set-up wrote the name. The launcher has since replaced the mods
	// folder, which is what sends people back to Set up.
	first := installedFile(t, api.machine.minecraft.CaptureProperties(), "contributor=alice\n", "")
	api.machine.keepRecord(t, first)

	got := api.ask(http.MethodPost, "/api/install", map[string]string{"contributor": "alice"})
	if got.status != http.StatusInternalServerError {
		t.Fatalf("status %d: %s", got.status, got.raw)
	}
	if held := api.machine.heldRecords(t); len(held) != 1 || held[0] != first {
		t.Fatalf("the record holds %v, want the first set-up's record and nothing else", held)
	}
}

// What an undo cannot put right is named as that, kept in the record, and
// finished by Remove once it can be.
func TestWhatAFailedSetUpCouldNotUndoIsLeftForRemove(t *testing.T) {
	net := &internet{down: map[string]bool{modAddress: true}}
	api := newRelease(t, net)
	api.machine.installFabric(t)
	jar := api.machine.fabricAPI()
	// Security software taking hold of a jar it has just seen written, while
	// the next download fails. What refuses to be removed on every platform a
	// test runs on is a directory with something in it, so that is what stands
	// where the jar was.
	net.whileDown = func() {
		if err := os.Remove(jar); err != nil {
			t.Error(err)
		}
		if err := os.MkdirAll(filepath.Join(jar, "held"), 0o755); err != nil {
			t.Error(err)
		}
	}

	got := api.ask(http.MethodPost, "/api/install", map[string]string{"contributor": "alice"})
	if got.status != http.StatusInternalServerError {
		t.Fatalf("status %d: %s", got.status, got.raw)
	}
	if next := got.text("next"); !strings.Contains(next, "could not be undone") || !strings.Contains(next, "Remove") {
		t.Errorf("it does not say that something is left, or what to do about it: %s", got.raw)
	}
	if held := api.machine.heldRecords(t); len(held) != 1 || held[0].Path != jar {
		t.Fatalf("the record holds %v, want only what could not be undone", held)
	}

	if err := os.RemoveAll(jar); err != nil {
		t.Fatal(err)
	}
	removed := api.ask(http.MethodPost, "/api/uninstall", nil)
	if complete, _ := removed.body["complete"].(bool); !complete {
		t.Fatalf("Remove did not finish what was left: %s", removed.raw)
	}
	if held := api.machine.heldRecords(t); held != nil {
		t.Errorf("the record outlived a finished Remove: %v", held)
	}
	if _, err := os.Stat(api.machine.minecraft.Mods()); !os.IsNotExist(err) {
		t.Error("the mods folder the attempt made is still there")
	}
}
