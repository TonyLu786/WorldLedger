package api

import (
	"bytes"
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/worldledger/worldledger-mc/desktop/internal/app"
	"github.com/worldledger/worldledger-mc/internal/mcpath"
)

// Every handler in this package was reached by nothing.
//
// The package measured 6.9% covered and each handler 0.0%, which a mutation
// found the sharp end of: deleting the check that refuses to make a world from
// a server nobody has decided about left the whole desktop suite green. That is
// the one consent gate a person using the window ever passes through, and
// nothing anywhere would have noticed it going.
//
// contract_test.go over in ui/ ties the page's calls to the routes that answer
// them, which catches a rename and cannot catch a deletion. These make requests.

// A machine of the test's own.
//
// The first version of these tests pointed WORLDLEDGER_HOME at a temporary
// directory and said that was what made the handlers safe to call. It made the
// archive safe. Minecraft, its saves and the capture folder are found through
// the account's own directories, and those were still the real ones, so GET
// /api/status walked the developer's actual capture folder and POST
// /api/import would have taken every capture they had not yet brought in,
// imported it into an archive deleted when the test ended, and renamed each
// one to say it had been brought in. The window would then have offered to
// clear them. It did not happen on the machine the tests were written on only
// because nothing there was waiting.
//
// It also made the tests pass or fail by what was installed. They were green
// on a machine with Minecraft and red in CI, which has none, and the red was
// the tests being wrong: "Minecraft was not found" is the right answer there.
type machine struct {
	root string
	// minecraft is where Minecraft would be on this platform, inside root. It
	// does not exist until installMinecraft.
	minecraft mcpath.Install
}

// isolate points every directory the handlers look in at the test's own, and
// then proves it: a test that can still see a real installation fails before
// it can touch one.
func isolate(t *testing.T) machine {
	t.Helper()
	root := t.TempDir()
	t.Setenv("WORLDLEDGER_HOME", filepath.Join(root, "worldledger"))
	// mcpath reads APPDATA on Windows and the home directory elsewhere, and
	// os.UserHomeDir reads USERPROFILE on Windows and HOME elsewhere. All four,
	// on every platform, so no platform is left looking at the real one.
	t.Setenv("APPDATA", filepath.Join(root, "appdata"))
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv("USERPROFILE", filepath.Join(root, "home"))

	if install, _, found := mcpath.FindInstall(); found {
		t.Fatalf("this test can still see a Minecraft installation at %s", install.Root)
	}
	if spool, _, found := mcpath.FindSpool(); found {
		t.Fatalf("this test can still see a capture folder at %s", spool)
	}
	candidates := mcpath.Installs()
	if len(candidates) == 0 {
		t.Fatal("there is nowhere Minecraft could be under the isolated directories; mcpath's table changed")
	}
	if !strings.HasPrefix(candidates[0].Root, root) {
		t.Fatalf("Minecraft would be looked for at %s, outside this test's directory %s",
			candidates[0].Root, root)
	}
	return machine{root: root, minecraft: candidates[0]}
}

func (m machine) installMinecraft(t *testing.T) {
	t.Helper()
	if err := os.MkdirAll(m.minecraft.Root, 0o755); err != nil {
		t.Fatal(err)
	}
}

// captureFolder is the folder the mod writes into, as it is after the mod has
// run once and before anything was captured.
func (m machine) captureFolder(t *testing.T) string {
	t.Helper()
	m.installMinecraft(t)
	if err := os.MkdirAll(m.minecraft.Spool(), 0o755); err != nil {
		t.Fatal(err)
	}
	return m.minecraft.Spool()
}

// capture puts one real capture in the folder, waiting to be brought in, under
// the given name. It is the bundle the core's own end-to-end test imports,
// copied byte for byte.
func (m machine) capture(t *testing.T, name string) string {
	t.Helper()
	source := filepath.Join("..", "..", "..", "testdata", "e2e-capture-bundle", "spool",
		"ready-5dfe3db2-208e-4cd8-8d11-1d83fa4f951b-00000000000000000417")
	target := filepath.Join(m.captureFolder(t), name)
	err := filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		destination := filepath.Join(target, relative)
		if entry.IsDir() {
			return os.MkdirAll(destination, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(destination, data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	return target
}

type apiUnderTest struct {
	t       *testing.T
	server  *app.Server
	base    string
	machine machine
}

// newAPI is the only way these tests get a server, so no test can reach a
// handler without first being isolated.
func newAPI(t *testing.T) apiUnderTest {
	t.Helper()
	m := isolate(t)

	server, err := app.New()
	if err != nil {
		t.Fatal(err)
	}
	watchdog := app.NewWatchdog(time.Minute)
	watchdog.Mount(server)
	Mount(server, watchdog)
	go server.Serve()
	t.Cleanup(func() { server.Close() })
	return apiUnderTest{t: t, server: server, base: "http://" + server.Addr(), machine: m}
}

type reply struct {
	status int
	body   map[string]any
	raw    string
}

func (r reply) text(field string) string {
	if value, ok := r.body[field].(string); ok {
		return value
	}
	return ""
}

func (r reply) problem() string { return r.text("problem") }

func (r reply) number(field string) float64 {
	value, _ := r.body[field].(float64)
	return value
}

func (a apiUnderTest) ask(method, path string, payload any) reply {
	a.t.Helper()
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			a.t.Fatal(err)
		}
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequest(method, a.base+path, body)
	if err != nil {
		a.t.Fatal(err)
	}
	request.Header.Set(app.TokenHeader, a.server.Token())
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		a.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		a.t.Fatal(err)
	}
	out := reply{status: response.StatusCode, raw: string(raw)}
	_ = json.Unmarshal(raw, &out.body)
	return out
}

// The mutation that survived. An export from a server nobody has declared
// anything about has to be refused, because it is the only moment in the whole
// window where somebody is asked to decide what may happen to what they
// recorded.
func TestMakingAWorldIsRefusedUntilSomebodyHasDecided(t *testing.T) {
	api := newAPI(t)
	world := t.TempDir()
	if err := os.WriteFile(filepath.Join(world, "level.dat"), []byte{0x0a, 0, 0, 0}, 0o644); err != nil {
		t.Fatal(err)
	}

	got := api.ask(http.MethodPost, "/api/export", map[string]string{
		"server":    "example.org:25565",
		"world_dir": world,
	})
	if got.status != http.StatusForbidden {
		t.Fatalf("status %d, want %d; body: %s", got.status, http.StatusForbidden, got.raw)
	}
	if !strings.Contains(got.problem(), "nothing has been said") {
		t.Errorf("the refusal does not say what is missing: %s", got.raw)
	}
	if !strings.Contains(got.text("next"), "Decide") {
		t.Errorf("the refusal does not say where to go: %s", got.raw)
	}
}

// And once somebody has decided, that refusal is gone and a different one takes
// its place. This is what stops the test above passing for the wrong reason: an
// export that refused everything with a 403 would satisfy it too.
func TestOnceSomebodyHasDecidedTheRefusalChanges(t *testing.T) {
	api := newAPI(t)
	world := t.TempDir()
	if err := os.WriteFile(filepath.Join(world, "level.dat"), []byte{0x0a, 0, 0, 0}, 0o644); err != nil {
		t.Fatal(err)
	}

	declare := api.ask(http.MethodPost, "/api/declare", map[string]string{
		"server":      "example.org:25565",
		"disposition": "private",
		"declared_by": "somebody",
	})
	if declare.status != http.StatusOK {
		t.Fatalf("declaring failed: %d %s", declare.status, declare.raw)
	}

	got := api.ask(http.MethodPost, "/api/export", map[string]string{
		"server":    "example.org:25565",
		"world_dir": world,
	})
	if got.status == http.StatusForbidden || strings.Contains(got.problem(), "nothing has been said") {
		t.Fatalf("a declared server was still refused for want of a declaration: %s", got.raw)
	}
	// Nothing was ever recorded for that server, so there is nothing to write,
	// and it has to say so rather than report a world made of nothing. That
	// arrives as a 200 carrying a problem, the same way importing an empty
	// folder does: the page treats any problem as the answer, whatever the
	// status, so the status is not what to check.
	if !strings.Contains(got.problem(), "nothing recorded") {
		t.Errorf("it does not say nothing was recorded: %s", got.raw)
	}
}

// Every one of these changes something or takes a while, and every one of them
// is reached from a button. A GET that did the work would mean a page reload,
// or anything that prefetches, doing it without being asked.
func TestTheHandlersThatActRefuseAGet(t *testing.T) {
	api := newAPI(t)
	for _, path := range []string{
		"/api/import", "/api/tidy", "/api/declare",
		"/api/export", "/api/install", "/api/uninstall",
	} {
		got := api.ask(http.MethodGet, path, nil)
		if got.status != http.StatusMethodNotAllowed {
			t.Errorf("GET %s answered %d, want %d", path, got.status, http.StatusMethodNotAllowed)
		}
		if got.problem() == "" {
			t.Errorf("GET %s refused without saying why: %s", path, got.raw)
		}
	}
}

// The state of a machine somebody has just downloaded this onto and nothing
// else: no archive, and no Minecraft either. Every screen has to open on it.
// The two that are about Minecraft cannot answer with anything but its
// absence, and have to say that and what to do, rather than fail.
func TestOnAMachineWithNothingOnItEveryScreenAnswers(t *testing.T) {
	api := newAPI(t)
	for _, path := range []string{
		"/api/health", "/api/status", "/api/notice", "/api/choices", "/api/moments?server=nobody",
	} {
		got := api.ask(http.MethodGet, path, nil)
		if got.status != http.StatusOK {
			t.Errorf("GET %s answered %d: %s", path, got.status, got.raw)
		}
		if !json.Valid([]byte(got.raw)) {
			t.Errorf("GET %s did not answer with JSON: %q", path, got.raw)
		}
	}
	for _, path := range []string{"/api/worlds", "/api/plan"} {
		got := api.ask(http.MethodGet, path, nil)
		if got.status != http.StatusNotFound {
			t.Errorf("GET %s answered %d with no Minecraft on the machine, want %d: %s",
				path, got.status, http.StatusNotFound, got.raw)
		}
		if !strings.Contains(got.problem(), "Minecraft was not found") || got.text("next") == "" {
			t.Errorf("GET %s did not say Minecraft is missing and what to do: %s", path, got.raw)
		}
	}
}

// Minecraft is there and nothing else is: the state the set-up screen exists
// for. The saves are listed from this machine and nowhere else, and a build
// from source says it cannot install before anybody is asked to agree to it.
func TestOnceMinecraftIsThereItsScreensAnswerFromIt(t *testing.T) {
	api := newAPI(t)
	api.machine.installMinecraft(t)

	worlds := api.ask(http.MethodGet, "/api/worlds", nil)
	if worlds.status != http.StatusOK {
		t.Fatalf("GET /api/worlds answered %d: %s", worlds.status, worlds.raw)
	}
	if got := worlds.text("saves_dir"); got != api.machine.minecraft.Saves() {
		t.Errorf("saves are read from %s, want this machine's %s", got, api.machine.minecraft.Saves())
	}
	if steps, _ := worlds.body["how_to_make"].([]any); len(steps) == 0 {
		t.Errorf("no world to write into and no steps for making one: %s", worlds.raw)
	}

	plan := api.ask(http.MethodGet, "/api/plan", nil)
	if plan.status != http.StatusOK {
		t.Fatalf("GET /api/plan answered %d: %s", plan.status, plan.raw)
	}
	if !strings.Contains(plan.text("refusal"), "does not know where to get the mod") {
		t.Errorf("a build with no mod address offered a plan instead of refusing: %s", plan.raw)
	}
}

func TestImportingBeforeTheModHasRunSaysWhereItLooked(t *testing.T) {
	api := newAPI(t)
	api.machine.installMinecraft(t)

	got := api.ask(http.MethodPost, "/api/import", nil)
	if got.status != http.StatusNotFound {
		t.Fatalf("status %d, want %d: %s", got.status, http.StatusNotFound, got.raw)
	}
	if !strings.Contains(got.problem(), "no capture folder") {
		t.Errorf("it does not say there is no capture folder: %s", got.raw)
	}
	if !strings.Contains(got.text("next"), filepath.Dir(api.machine.minecraft.Spool())) {
		t.Errorf("it does not say where the folder will appear: %s", got.raw)
	}
}

// Nothing captured yet is the ordinary state after installing, and has to be an
// answer rather than a failure.
func TestImportingAnEmptyCaptureFolderIsNotAFailure(t *testing.T) {
	api := newAPI(t)
	api.machine.captureFolder(t)

	got := api.ask(http.MethodPost, "/api/import", nil)
	if got.status != http.StatusOK {
		t.Fatalf("status %d, want %d: %s", got.status, http.StatusOK, got.raw)
	}
	if !strings.Contains(got.problem(), "nothing has been captured") {
		t.Errorf("it does not say nothing was captured: %s", got.raw)
	}
}

// The path every recording takes: captured, brought in, and kept.
//
// This is the test that could not be written while the handlers looked at the
// real machine, because running it there is the damage described at the top.
func TestImportBringsACaptureInAndKeepsIt(t *testing.T) {
	api := newAPI(t)
	name := "ready-5dfe3db2-208e-4cd8-8d11-1d83fa4f951b-00000000000000000417"
	ready := api.machine.capture(t, name)

	got := api.ask(http.MethodPost, "/api/import", nil)
	if got.status != http.StatusOK {
		t.Fatalf("status %d: %s", got.status, got.raw)
	}
	if got.number("imported") != 1 || got.number("total") != 1 {
		t.Fatalf("imported %v of %v, want 1 of 1: %s", got.number("imported"), got.number("total"), got.raw)
	}
	if failed, _ := got.body["failed"].([]any); len(failed) != 0 {
		t.Fatalf("something failed: %s", got.raw)
	}
	if kept, _ := got.body["kept"].(bool); !kept {
		t.Errorf("it does not say the capture was kept: %s", got.raw)
	}

	// Renamed, not consumed: the capture is still there, and no longer waiting.
	if _, err := os.Stat(ready); !os.IsNotExist(err) {
		t.Errorf("the capture is still marked as waiting: %v", err)
	}
	imported := filepath.Join(api.machine.minecraft.Spool(), "imported-"+strings.TrimPrefix(name, "ready-"))
	if _, err := os.Stat(filepath.Join(imported, "bundle.json")); err != nil {
		t.Errorf("the capture was not kept as imported: %v", err)
	}

	status := api.ask(http.MethodGet, "/api/status", nil)
	if status.number("observations") != 1 {
		t.Errorf("the archive holds %v observations after importing one: %s",
			status.number("observations"), status.raw)
	}
	spoolState, _ := status.body["spool"].(map[string]any)
	if spoolState["ready"] != float64(0) || spoolState["imported"] != float64(1) {
		t.Errorf("the capture folder reads %v waiting and %v kept, want 0 and 1: %s",
			spoolState["ready"], spoolState["imported"], status.raw)
	}
	if size, _ := spoolState["imported_bytes"].(float64); size <= 0 {
		t.Errorf("what the kept capture weighs is not reported: %s", status.raw)
	}
}

// Clearing deletes, and it deletes only what is already in the archive. A
// capture still waiting to be brought in is somebody's only copy of it.
func TestClearingRemovesOnlyWhatWasBroughtIn(t *testing.T) {
	api := newAPI(t)
	api.machine.capture(t, "ready-5dfe3db2-208e-4cd8-8d11-1d83fa4f951b-00000000000000000417")
	if got := api.ask(http.MethodPost, "/api/import", nil); got.number("imported") != 1 {
		t.Fatalf("setting up: the import did not happen: %s", got.raw)
	}
	waiting := api.machine.capture(t, "ready-5dfe3db2-208e-4cd8-8d11-1d83fa4f951b-00000000000000000418")

	got := api.ask(http.MethodPost, "/api/tidy", nil)
	if got.status != http.StatusOK {
		t.Fatalf("status %d: %s", got.status, got.raw)
	}
	if got.number("removed") != 1 || got.number("freed") <= 0 || got.number("remaining") != 1 {
		t.Errorf("removed %v freeing %v with %v remaining, want 1, more than 0, and 1: %s",
			got.number("removed"), got.number("freed"), got.number("remaining"), got.raw)
	}
	if _, err := os.Stat(filepath.Join(waiting, "bundle.json")); err != nil {
		t.Errorf("clearing took a capture that had not been brought in: %v", err)
	}
	entries, err := os.ReadDir(api.machine.minecraft.Spool())
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "imported-") {
			t.Errorf("%s is still there after clearing", entry.Name())
		}
	}
}

func TestClearingWithNothingBroughtInChangesNothing(t *testing.T) {
	api := newAPI(t)
	waiting := api.machine.capture(t, "ready-5dfe3db2-208e-4cd8-8d11-1d83fa4f951b-00000000000000000417")

	got := api.ask(http.MethodPost, "/api/tidy", nil)
	if got.status != http.StatusOK || !strings.Contains(got.problem(), "nothing to clear") {
		t.Fatalf("status %d: %s", got.status, got.raw)
	}
	if _, err := os.Stat(filepath.Join(waiting, "bundle.json")); err != nil {
		t.Errorf("a capture waiting to be brought in was touched: %v", err)
	}
}

// Travel is a read rather than an action, so it takes a GET. What it must not
// do is compare against a moment it was not given: the whole screen is two
// moments and a difference between them, and one missing moment silently
// defaulted is a difference somebody would believe.
func TestComparingNeedsBothMoments(t *testing.T) {
	api := newAPI(t)
	for _, query := range []string{
		"?server=example.org",
		"?server=example.org&from=2026-01-01T00:00:00Z",
		"?server=example.org&to=2026-01-01T00:00:00Z",
		"?server=example.org&from=yesterday&to=today",
	} {
		got := api.ask(http.MethodGet, "/api/travel"+query, nil)
		if got.status != http.StatusBadRequest {
			t.Errorf("GET /api/travel%s answered %d, want %d: %s",
				query, got.status, http.StatusBadRequest, got.raw)
		}
		if got.problem() == "" {
			t.Errorf("GET /api/travel%s refused without saying why: %s", query, got.raw)
		}
	}
}

// A declaration needs a name against it. An unattributed one is the thing the
// whole step exists to prevent.
func TestADeclarationNeedsAName(t *testing.T) {
	api := newAPI(t)
	got := api.ask(http.MethodPost, "/api/declare", map[string]string{
		"server":      "example.org:25565",
		"disposition": "private",
	})
	if got.status == http.StatusOK {
		t.Fatalf("an unattributed declaration was accepted: %s", got.raw)
	}
	if got.problem() == "" {
		t.Errorf("it was refused without saying why: %s", got.raw)
	}
}

// And a disposition nobody defined is refused rather than stored.
func TestADeclarationNeedsARealDisposition(t *testing.T) {
	api := newAPI(t)
	got := api.ask(http.MethodPost, "/api/declare", map[string]string{
		"server":      "example.org:25565",
		"disposition": "whatever-i-feel-like",
		"declared_by": "somebody",
	})
	if got.status == http.StatusOK {
		t.Fatalf("a disposition that does not exist was accepted: %s", got.raw)
	}
}

// The token is the whole of the authentication, and it is checked by the server
// rather than by these handlers. That it applies to them is worth one test,
// because a route registered outside the guarded prefix would not be covered by
// it and nothing else would say so.
func TestEveryHandlerIsBehindTheToken(t *testing.T) {
	api := newAPI(t)
	for _, path := range []string{
		"/api/health", "/api/status", "/api/notice", "/api/choices", "/api/worlds",
		"/api/import", "/api/tidy", "/api/declare", "/api/export", "/api/moments",
		"/api/travel", "/api/plan", "/api/install", "/api/uninstall", "/api/alive",
	} {
		request, err := http.NewRequest(http.MethodGet, api.base+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s answered %d without a token, want %d",
				path, response.StatusCode, http.StatusUnauthorized)
		}
	}
}
