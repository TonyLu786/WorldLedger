package installer

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/worldledger/worldledger-mc/desktop/internal/health"
	"github.com/worldledger/worldledger-mc/internal/mcpath"
)

// This is the only code here that writes into somebody's game, and a mistake in
// it does not look like a mistake: it looks like their Minecraft has stopped
// starting. Everything below is about the two things that keeps safe -- not
// touching what we were not asked to, and being able to put back exactly what
// was there.

// stubFetcher serves bytes from a map, so the whole of applying can be tested
// without the network. An installer that can only be exercised by really
// downloading things is one that gets exercised rarely.
type stubFetcher map[string][]byte

func (s stubFetcher) Fetch(source string) ([]byte, error) {
	body, ok := s[source]
	if !ok {
		return nil, os.ErrNotExist
	}
	return body, nil
}

func fixture(t *testing.T) mcpath.Install {
	t.Helper()
	install := mcpath.Install{Root: t.TempDir()}
	if err := os.MkdirAll(install.Versions(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(install.LauncherProfiles(),
		[]byte(`{"profiles":{"vanilla":{"name":"","lastVersionId":"latest-release"}},"version":6}`), 0o644); err != nil {
		t.Fatal(err)
	}
	return install
}

// report builds a health report with the given states, so a plan can be asked
// for from a known starting point.
func report(states map[string]health.State) health.Report {
	out := health.Report{}
	for _, id := range []string{"minecraft", "release", "loader", "fabric-api", "worldledger", "contributor"} {
		state, given := states[id]
		if !given {
			state = health.Missing
		}
		out.Checks = append(out.Checks, health.Check{ID: id, State: state})
	}
	return out
}

func allMissing() map[string]health.State {
	return map[string]health.State{"minecraft": health.OK, "release": health.OK}
}

// The refusal that matters most. The mod is compiled against one release, and
// putting it into another is how a game stops starting.
func TestAWrongMinecraftIsRefusedRatherThanInstalledInto(t *testing.T) {
	plan := BuildPlan(fixture(t), report(map[string]health.State{
		"minecraft": health.OK, "release": health.Missing,
	}), "mod-source", "alice")

	if plan.Runnable() {
		t.Fatal("a plan was produced for a Minecraft the mod is not built for")
	}
	if !strings.Contains(plan.Refusal, health.MinecraftVersion) {
		t.Errorf("the refusal does not name the release it needs: %q", plan.Refusal)
	}
	if len(plan.Steps) != 0 {
		t.Errorf("a refused plan still lists %d steps", len(plan.Steps))
	}
}

func TestNoMinecraftIsRefusedAndOffersNothing(t *testing.T) {
	plan := BuildPlan(fixture(t), report(map[string]health.State{"minecraft": health.Missing}), "mod", "alice")
	if plan.Runnable() || len(plan.Steps) != 0 {
		t.Fatalf("a plan was produced with no Minecraft: %+v", plan)
	}
}

// Every step has to name the exact file it will touch, because this list is
// what somebody approves and "install Fabric" is not something anybody can
// meaningfully agree to.
func TestEveryStepNamesTheFileItWillWrite(t *testing.T) {
	install := fixture(t)
	plan := BuildPlan(install, report(allMissing()), "mod-source", "alice")
	if !plan.Runnable() {
		t.Fatalf("nothing to do: %+v", plan)
	}
	for _, step := range plan.Steps {
		if step.Target == "" {
			t.Errorf("step %q does not say what it writes", step.Title)
		}
		if !strings.HasPrefix(step.Target, install.Root) {
			t.Errorf("step %q writes outside the Minecraft directory: %s", step.Title, step.Target)
		}
		if step.Title == "" {
			t.Errorf("step with target %s has nothing to show a person", step.Target)
		}
	}
}

// Nothing already in place should be done again. Reinstalling Fabric API over a
// working one is a needless write into somebody's game.
func TestWhatIsAlreadyThereIsNotDoneAgain(t *testing.T) {
	install := fixture(t)
	plan := BuildPlan(install, report(map[string]health.State{
		"minecraft": health.OK, "release": health.OK, "loader": health.OK, "fabric-api": health.OK,
	}), "mod-source", "alice")

	for _, step := range plan.Steps {
		if step.Kind == WriteLoaderProfile || strings.Contains(step.Title, "Fabric API") {
			t.Errorf("a step was planned for something already installed: %q", step.Title)
		}
	}
}

func applyFixture(t *testing.T) (mcpath.Install, Manifest) {
	t.Helper()
	install := fixture(t)
	plan := BuildPlan(install, report(allMissing()), "https://example.invalid/worldledger.jar", "alice")

	fetcher := stubFetcher{
		loaderProfileURL(health.MinecraftVersion, health.LoaderVersion): []byte(
			`{"id":"` + LoaderVersionID() + `","inheritsFrom":"` + health.MinecraftVersion + `"}`),
		fabricAPIURL(health.FabricAPIVersion):     []byte("fabric api jar"),
		"https://example.invalid/worldledger.jar": []byte("worldledger jar"),
	}

	manifest, err := Apply(plan, fetcher, filepath.Join(t.TempDir(), "backups"))
	if err != nil {
		t.Fatalf("applying failed: %v", err)
	}
	return install, manifest
}

func TestApplyingPutsEverythingWhereTheGameLooksForIt(t *testing.T) {
	install, _ := applyFixture(t)

	for _, path := range []string{
		install.VersionProfile(LoaderVersionID()),
		install.Mod("fabric-api-" + health.FabricAPIVersion + ".jar"),
		install.Mod("worldledger.jar"),
		install.CaptureProperties(),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("%s was not written: %v", path, err)
		}
	}

	properties, err := os.ReadFile(install.CaptureProperties())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(properties), "contributor=alice") {
		t.Errorf("the contributor was not written: %q", properties)
	}
}

// A version on disk with no launcher entry exists and cannot be chosen, and the
// entries already there belong to somebody else.
func TestTheLauncherEntryIsAddedWithoutDisturbingTheOthers(t *testing.T) {
	install, _ := applyFixture(t)

	raw, err := os.ReadFile(install.LauncherProfiles())
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatalf("the launcher settings are no longer valid JSON: %v", err)
	}
	profiles := document["profiles"].(map[string]any)
	if _, kept := profiles["vanilla"]; !kept {
		t.Error("an installation that was already there was removed")
	}
	if _, added := profiles["worldledger-"+LoaderVersionID()]; !added {
		t.Error("the Fabric installation was not added to the launcher")
	}
	if document["version"] == nil {
		t.Error("the rest of the launcher settings were dropped")
	}
}

// The whole point of the record.
func TestUninstallingPutsEverythingBack(t *testing.T) {
	install, manifest := applyFixture(t)

	before, err := os.ReadFile(install.LauncherProfiles())
	if err != nil {
		t.Fatal(err)
	}

	skipped, err := Uninstall(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if len(skipped) != 0 {
		t.Errorf("uninstalling skipped things it wrote itself: %v", skipped)
	}

	for _, path := range []string{
		install.VersionProfile(LoaderVersionID()),
		install.Mod("fabric-api-" + health.FabricAPIVersion + ".jar"),
		install.Mod("worldledger.jar"),
		install.CaptureProperties(),
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("%s is still there after uninstalling", path)
		}
	}

	// The launcher settings existed before, so they are restored rather than
	// deleted. Deleting them would take somebody's whole list of installations.
	after, err := os.ReadFile(install.LauncherProfiles())
	if err != nil {
		t.Fatalf("the launcher settings were deleted rather than restored: %v", err)
	}
	if string(after) == string(before) {
		t.Error("the launcher settings still carry the entry that was added")
	}
	var document map[string]any
	if err := json.Unmarshal(after, &document); err != nil {
		t.Fatal(err)
	}
	if _, kept := document["profiles"].(map[string]any)["vanilla"]; !kept {
		t.Error("restoring the launcher settings lost the installation that was there first")
	}
}

// Somebody may have edited what we wrote. Removing a mod is not a licence to
// throw away what they wrote afterwards.
func TestAFileChangedSinceInstallingIsLeftAlone(t *testing.T) {
	install, manifest := applyFixture(t)

	edited := "# WorldLedger capture adapter\ncontributor=someone-else\ncoalesce_ticks=20\n"
	if err := os.WriteFile(install.CaptureProperties(), []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}

	skipped, err := Uninstall(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if len(skipped) == 0 {
		t.Fatal("the edited file was removed without a word")
	}
	current, err := os.ReadFile(install.CaptureProperties())
	if err != nil {
		t.Fatalf("the edited file was deleted: %v", err)
	}
	if string(current) != edited {
		t.Errorf("the edited file was changed: %q", current)
	}
}

// A mods directory somebody has put their own mods in is not ours to remove.
func TestUninstallingLeavesADirectoryThatHasSomethingElseInIt(t *testing.T) {
	install, manifest := applyFixture(t)

	other := install.Mod("somebody-elses-mod.jar")
	if err := os.WriteFile(other, []byte("not ours"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Uninstall(manifest); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(other); err != nil {
		t.Errorf("somebody else's mod was removed: %v", err)
	}
	if _, err := os.Stat(install.Mods()); err != nil {
		t.Errorf("the mods directory was removed while it still had something in it: %v", err)
	}
}

// A version profile that is not JSON is a version the launcher refuses, and it
// would fail at the point where somebody is trying to play.
func TestSomethingThatIsNotAVersionProfileIsRefusedOnArrival(t *testing.T) {
	install := fixture(t)
	plan := BuildPlan(install, report(allMissing()), "https://example.invalid/mod.jar", "alice")

	fetcher := stubFetcher{
		loaderProfileURL(health.MinecraftVersion, health.LoaderVersion): []byte("<html>not found</html>"),
	}
	manifest, err := Apply(plan, fetcher, filepath.Join(t.TempDir(), "backups"))
	if err == nil {
		t.Fatal("a version profile that is not JSON was written")
	}
	if _, statErr := os.Stat(install.VersionProfile(LoaderVersionID())); !os.IsNotExist(statErr) {
		t.Error("the bad profile was written anyway")
	}
	// The manifest still comes back so the caller can undo whatever did happen.
	if manifest.Schema == "" {
		t.Error("a failure returned no manifest, so there is nothing to undo with")
	}
}

// Failing part way through has to leave a record, or there is no way back.
func TestAFailurePartWayThroughStillReportsWhatWasDone(t *testing.T) {
	install := fixture(t)
	plan := BuildPlan(install, report(allMissing()), "https://example.invalid/mod.jar", "alice")

	// The loader profile succeeds; the Fabric API download is not served.
	fetcher := stubFetcher{
		loaderProfileURL(health.MinecraftVersion, health.LoaderVersion): []byte(`{"id":"x"}`),
	}
	manifest, err := Apply(plan, fetcher, filepath.Join(t.TempDir(), "backups"))
	if err == nil {
		t.Fatal("a missing download was not reported")
	}
	if len(manifest.Records) == 0 {
		t.Fatal("nothing was recorded, so the part that succeeded cannot be undone")
	}
	if _, err := Uninstall(manifest); err != nil {
		t.Fatalf("undoing a partial install failed: %v", err)
	}
	if _, err := os.Stat(install.VersionProfile(LoaderVersionID())); !os.IsNotExist(err) {
		t.Error("the half-installed version profile is still there")
	}
}
