package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/worldledger/worldledger-mc/desktop/internal/app"
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

func newAPI(t *testing.T) (*app.Server, string) {
	t.Helper()
	// The handlers find the archive through home, which reads this. Pointing it
	// at a temporary directory is what makes them safe to call.
	t.Setenv("WORLDLEDGER_HOME", t.TempDir())

	server, err := app.New()
	if err != nil {
		t.Fatal(err)
	}
	watchdog := app.NewWatchdog(time.Minute)
	watchdog.Mount(server)
	Mount(server, watchdog)
	go server.Serve()
	t.Cleanup(func() { server.Close() })
	return server, "http://" + server.Addr()
}

type reply struct {
	status int
	body   map[string]any
	raw    string
}

func (r reply) problem() string {
	if value, ok := r.body["problem"].(string); ok {
		return value
	}
	return ""
}

func ask(t *testing.T, server *app.Server, base, method, path string, payload any) reply {
	t.Helper()
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequest(method, base+path, body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set(app.TokenHeader, server.Token())
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
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
	server, base := newAPI(t)
	world := t.TempDir()
	if err := os.WriteFile(filepath.Join(world, "level.dat"), []byte{0x0a, 0, 0, 0}, 0o644); err != nil {
		t.Fatal(err)
	}

	got := ask(t, server, base, http.MethodPost, "/api/export", map[string]string{
		"server":    "example.org:25565",
		"world_dir": world,
	})
	if got.status != http.StatusForbidden {
		t.Fatalf("status %d, want %d; body: %s", got.status, http.StatusForbidden, got.raw)
	}
	if !strings.Contains(got.problem(), "nothing has been said") {
		t.Errorf("the refusal does not say what is missing: %s", got.raw)
	}
	if next, _ := got.body["next"].(string); !strings.Contains(next, "Decide") {
		t.Errorf("the refusal does not say where to go: %s", got.raw)
	}
}

// And once somebody has decided, the refusal is a different one. This is what
// stops the test above from passing for the wrong reason: if export refused
// everything, it would still be green.
func TestOnceSomebodyHasDecidedTheRefusalChanges(t *testing.T) {
	server, base := newAPI(t)
	world := t.TempDir()
	if err := os.WriteFile(filepath.Join(world, "level.dat"), []byte{0x0a, 0, 0, 0}, 0o644); err != nil {
		t.Fatal(err)
	}

	declare := ask(t, server, base, http.MethodPost, "/api/declare", map[string]string{
		"server":      "example.org:25565",
		"disposition": "private",
		"declared_by": "somebody",
	})
	if declare.status != http.StatusOK {
		t.Fatalf("declaring failed: %d %s", declare.status, declare.raw)
	}

	got := ask(t, server, base, http.MethodPost, "/api/export", map[string]string{
		"server":    "example.org:25565",
		"world_dir": world,
	})
	if got.status == http.StatusForbidden {
		t.Fatalf("a declared server was still refused for want of a declaration: %s", got.raw)
	}
	// It now fails for the real reason, which is that nothing was recorded.
	if !strings.Contains(got.raw, "nothing recorded") && got.status == http.StatusOK {
		t.Logf("export answered %d: %s", got.status, got.raw)
	}
}

// Every one of these changes something or takes a while, and every one of them
// is reached from a button. A GET that did the work would mean a page reload,
// or anything that prefetches, doing it without being asked.
func TestTheHandlersThatActRefuseAGet(t *testing.T) {
	server, base := newAPI(t)
	for _, path := range []string{
		"/api/import", "/api/tidy", "/api/declare",
		"/api/export", "/api/install", "/api/uninstall",
	} {
		got := ask(t, server, base, http.MethodGet, path, nil)
		if got.status != http.StatusMethodNotAllowed {
			t.Errorf("GET %s answered %d, want %d", path, got.status, http.StatusMethodNotAllowed)
		}
		if got.problem() == "" {
			t.Errorf("GET %s refused without saying why: %s", path, got.raw)
		}
	}
}

// The reading handlers answer a fresh install without an archive, because that
// is the state every first run is in and a window that errors there is a window
// nobody gets past.
func TestTheReadingHandlersAnswerOnAFreshInstall(t *testing.T) {
	server, base := newAPI(t)
	for _, path := range []string{
		"/api/health", "/api/status", "/api/notice", "/api/choices",
		"/api/worlds", "/api/plan", "/api/moments?server=nobody",
	} {
		got := ask(t, server, base, http.MethodGet, path, nil)
		if got.status != http.StatusOK {
			t.Errorf("GET %s answered %d on a fresh install: %s", path, got.status, got.raw)
		}
		if len(got.raw) == 0 || !json.Valid([]byte(got.raw)) {
			t.Errorf("GET %s did not answer with JSON: %q", path, got.raw)
		}
	}
}

// Travel is a read rather than an action, so it takes a GET. What it must not
// do is compare against a moment it was not given: the whole screen is two
// moments and a difference between them, and one missing moment silently
// defaulted is a difference somebody would believe.
func TestComparingNeedsBothMoments(t *testing.T) {
	server, base := newAPI(t)
	for _, query := range []string{
		"?server=example.org",
		"?server=example.org&from=2026-01-01T00:00:00Z",
		"?server=example.org&to=2026-01-01T00:00:00Z",
		"?server=example.org&from=yesterday&to=today",
	} {
		got := ask(t, server, base, http.MethodGet, "/api/travel"+query, nil)
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
	server, base := newAPI(t)
	got := ask(t, server, base, http.MethodPost, "/api/declare", map[string]string{
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
	server, base := newAPI(t)
	got := ask(t, server, base, http.MethodPost, "/api/declare", map[string]string{
		"server":      "example.org:25565",
		"disposition": "whatever-i-feel-like",
		"declared_by": "somebody",
	})
	if got.status == http.StatusOK {
		t.Fatalf("a disposition that does not exist was accepted: %s", got.raw)
	}
}

// Importing with nothing captured is the ordinary first-run state, and it has
// to be an answer rather than a failure.
func TestImportingNothingSaysSoRatherThanFailing(t *testing.T) {
	server, base := newAPI(t)
	got := ask(t, server, base, http.MethodPost, "/api/import", nil)
	if got.status >= 500 {
		t.Fatalf("importing on a fresh install answered %d: %s", got.status, got.raw)
	}
	if got.problem() == "" && got.body["total"] == nil {
		t.Errorf("it neither imported nor said why not: %s", got.raw)
	}
}

// The token is the whole of the authentication, and it is checked by the server
// rather than by these handlers. That it applies to them is worth one test,
// because a route registered outside the guarded prefix would not be covered by
// it and nothing else would say so.
func TestEveryHandlerIsBehindTheToken(t *testing.T) {
	_, base := newAPI(t)
	for _, path := range []string{
		"/api/health", "/api/status", "/api/notice", "/api/choices", "/api/worlds",
		"/api/import", "/api/tidy", "/api/declare", "/api/export", "/api/moments",
		"/api/travel", "/api/plan", "/api/install", "/api/uninstall", "/api/alive",
	} {
		request, err := http.NewRequest(http.MethodGet, base+path, nil)
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
