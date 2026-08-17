package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sync"

	"github.com/worldledger/worldledger-mc/desktop/internal/app"
	"github.com/worldledger/worldledger-mc/desktop/internal/health"
	"github.com/worldledger/worldledger-mc/desktop/internal/home"
	"github.com/worldledger/worldledger-mc/desktop/internal/installer"
	"github.com/worldledger/worldledger-mc/internal/mcpath"
)

// Installing is meant to be one click and a few seconds, and the way it stays
// that way is by not asking anything it can work out.
//
// The one thing it does ask is the name to record under, because nothing is
// kept until there is one and nobody else can choose it. Everything else --
// which Fabric, which version, where each file goes -- follows from the release
// the mod was built against.
//
// The confirmation is a single list of exact paths, shown once before anything
// happens. That is the point at which somebody agrees to have their game
// written into, and it is the only such point: asking four times is not four
// times the consent, it is a dialog people learn to click through.

var installing sync.Mutex

// ModSource is where the WorldLedger jar is fetched from.
//
// It is a variable rather than a constant so a release build can point it at
// that release's asset while a development build points at a file. The default
// is empty, which makes the plan refuse rather than download something
// arbitrary: an installer that guesses where its own payload lives is one that
// can be pointed anywhere.
var ModSource = ""

func handlePlan(w http.ResponseWriter, r *http.Request) {
	install, _, found := mcpath.FindInstall()
	if !found {
		app.WriteFailure(w, http.StatusNotFound,
			"Minecraft was not found on this computer",
			"install Minecraft and play it once, then come back")
		return
	}
	contributor := r.URL.Query().Get("contributor")
	app.WriteJSON(w, http.StatusOK,
		installer.BuildPlan(install, health.Inspect(install), ModSource, contributor))
}

type installRequest struct {
	Contributor string `json:"contributor"`
}

func handleInstall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		app.WriteFailure(w, http.StatusMethodNotAllowed,
			"installing has to be asked for", "use the button on the set up screen")
		return
	}
	if !installing.TryLock() {
		app.WriteFailure(w, http.StatusConflict, "an install is already running", "wait for it to finish")
		return
	}
	defer installing.Unlock()

	var request installRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&request); err != nil {
		app.WriteFailure(w, http.StatusBadRequest, "the request could not be read", "try again")
		return
	}
	if request.Contributor == "" {
		app.WriteFailure(w, http.StatusBadRequest,
			"a name is needed before anything is recorded",
			"type the name you want your recordings kept under")
		return
	}

	install, _, found := mcpath.FindInstall()
	if !found {
		app.WriteFailure(w, http.StatusNotFound,
			"Minecraft was not found on this computer",
			"install Minecraft and play it once, then come back")
		return
	}

	plan := installer.BuildPlan(install, health.Inspect(install), ModSource, request.Contributor)
	if plan.Refusal != "" {
		app.WriteFailure(w, http.StatusConflict, plan.Refusal, "nothing was changed")
		return
	}
	if len(plan.Steps) == 0 {
		app.WriteJSON(w, http.StatusOK, map[string]any{"done": 0, "already": true})
		return
	}
	if ModSource == "" {
		app.WriteFailure(w, http.StatusServiceUnavailable,
			"this build does not know where to get the mod from",
			"use a released version of this application")
		return
	}

	dir, err := home.Dir()
	if err != nil {
		app.WriteFailure(w, http.StatusInternalServerError, err.Error(), "nothing was changed")
		return
	}
	backups := filepath.Join(dir, "backups")

	manifest, err := installer.Apply(plan, installer.HTTPFetcher{}, backups)
	// The manifest is saved whether or not it worked. A partial install with no
	// record is the one outcome with no way back, and undoing has to be possible
	// from a later run of the application rather than only from this one.
	saved := saveManifest(dir, manifest)

	if err != nil {
		undone, undoErr := installer.Uninstall(manifest)
		next := "nothing was left behind"
		if undoErr != nil || len(undone) > 0 {
			next = "some of it could not be undone automatically; the record is in " + saved
		}
		app.WriteFailure(w, http.StatusInternalServerError, err.Error(), next)
		return
	}

	app.WriteJSON(w, http.StatusOK, map[string]any{
		"done":   len(manifest.Records),
		"record": saved,
	})
}

func handleUninstall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		app.WriteFailure(w, http.StatusMethodNotAllowed,
			"removing has to be asked for", "use the button on the set up screen")
		return
	}
	dir, err := home.Dir()
	if err != nil {
		app.WriteFailure(w, http.StatusInternalServerError, err.Error(), "nothing was changed")
		return
	}
	raw, err := os.ReadFile(manifestPath(dir))
	if err != nil {
		app.WriteFailure(w, http.StatusNotFound,
			"there is no record of this application having installed anything",
			"nothing was changed. Anything you installed yourself is yours to remove")
		return
	}
	var manifest installer.Manifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		app.WriteFailure(w, http.StatusInternalServerError,
			"the record of what was installed could not be read",
			"nothing was changed")
		return
	}

	skipped, err := installer.Uninstall(manifest)
	if err != nil {
		app.WriteFailure(w, http.StatusInternalServerError, err.Error(),
			"some of it was removed; the record is still in "+manifestPath(dir))
		return
	}
	os.Remove(manifestPath(dir))
	app.WriteJSON(w, http.StatusOK, map[string]any{"skipped": skipped})
}

func manifestPath(dir string) string { return filepath.Join(dir, "installed.json") }

// saveManifest writes the record and returns where it went. A failure to save
// is not worth failing the install over, but it does mean the path reported
// back should not claim a file that is not there.
func saveManifest(dir string, manifest installer.Manifest) string {
	path := manifestPath(dir)
	body, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return ""
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ""
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		return ""
	}
	return path
}
