package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
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
	// Refused here rather than after the plan has been shown and agreed to.
	//
	// A build from source carries no address for the mod, so installing was
	// always going to fail. It failed at the end: somebody typed their name,
	// read five file paths, agreed to all five, and then got a line of small
	// text. Consent had been collected for something that could not happen,
	// which is worse than not asking, and the same refusal a step earlier costs
	// nothing and is true at the moment it is said.
	if ModSource == "" {
		app.WriteJSON(w, http.StatusOK, installer.Plan{
			Refusal: "this build of the application does not know where to get the mod from, " +
				"so it cannot set anything up. Use a released version, which carries that address.",
		})
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
		next := reachabilityAdvice(err)
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

// reachabilityAdvice turns a failure into the thing to try.
//
// Installing needs two files from the internet, and the ways that goes wrong
// for a player are dull and specific: no connection, a network that blocks
// what it does not recognise, or security software that stopped the download.
// "dial tcp: lookup meta.fabricmc.net" is an accurate way of saying the first
// of those to somebody who will never read it.
func reachabilityAdvice(err error) string {
	text := strings.ToLower(err.Error())
	switch {
	case strings.Contains(text, "no such host"),
		strings.Contains(text, "dial tcp"),
		strings.Contains(text, "timeout"),
		strings.Contains(text, "deadline exceeded"),
		strings.Contains(text, "connection refused"),
		strings.Contains(text, "connection reset"):
		return "nothing was changed. This needs the internet to fetch Fabric and the mod. " +
			"Check your connection; on a school or work network, or behind security software, " +
			"the download may be blocked"
	case strings.Contains(text, "is not a mod file"),
		strings.Contains(text, "is not a version profile"):
		return "nothing was changed. Something answered instead of the file itself, " +
			"which usually means a network sign-in page or a blocked download"
	case strings.Contains(text, "answered 404"):
		return "nothing was changed. That file is not where this version of the application " +
			"expects it; a newer release should be used"
	case strings.Contains(text, "permission denied"), strings.Contains(text, "access is denied"):
		return "nothing was changed. Windows refused the write. Close Minecraft and its launcher, " +
			"and check that security software is not protecting the folder"
	}
	return "nothing was left behind"
}

// saveManifest writes the record and returns where it went. A failure to save
// is not worth failing the install over, but it does mean the path reported
// back should not claim a file that is not there.
func saveManifest(dir string, manifest installer.Manifest) string {
	path := manifestPath(dir)
	// Added to what is already recorded, not written over it.
	//
	// A plan only contains steps for what is missing, so a second set-up is
	// usually one step. Replacing the record with that one step is how Remove
	// came to remove a single jar and report "Your Minecraft is back to what it
	// was" while the Fabric profile, the launcher entry, Fabric API and
	// capture.properties all stayed -- and the play screen sends people back to
	// Set up for exactly the case that causes it, a launcher that replaced the
	// mods folder.
	if previous, err := os.ReadFile(path); err == nil {
		var held installer.Manifest
		if json.Unmarshal(previous, &held) == nil {
			manifest = mergeManifests(held, manifest)
		}
	}
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

// mergeManifests keeps everything an earlier set-up recorded and adds what this
// one did.
//
// Where both touched the same file the two halves come from different places,
// and getting that backwards undoes the wrong thing. The digest has to be the
// new one, because that is what is on disk now and an uninstall compares against
// it before removing anything -- keeping the old digest would make every
// reinstalled file look changed by somebody else and be left behind. The backup
// has to be the oldest one, because that is what was there before this
// application first touched the file; the later backup is a copy of our own
// previous install, and restoring that would leave the player with our file
// rather than theirs.
func mergeManifests(held, fresh installer.Manifest) installer.Manifest {
	out := fresh
	out.Records = nil

	freshByPath := make(map[string]installer.Record, len(fresh.Records))
	for _, record := range fresh.Records {
		freshByPath[record.Path] = record
	}

	seen := map[string]struct{}{}
	for _, earlier := range held.Records {
		if _, done := seen[earlier.Path]; done {
			continue
		}
		seen[earlier.Path] = struct{}{}
		if now, again := freshByPath[earlier.Path]; again {
			now.Backup = earlier.Backup
			out.Records = append(out.Records, now)
			continue
		}
		out.Records = append(out.Records, earlier)
	}
	for _, record := range fresh.Records {
		if _, done := seen[record.Path]; done {
			continue
		}
		seen[record.Path] = struct{}{}
		out.Records = append(out.Records, record)
	}

	// Deepest last, so an uninstall removes children before parents. Both lists
	// already hold that order, and neither is long enough for the duplicate
	// check to be worth more than a linear scan.
	out.Directories = held.Directories
	for _, dir := range fresh.Directories {
		known := false
		for _, existing := range out.Directories {
			if existing == dir {
				known = true
				break
			}
		}
		if !known {
			out.Directories = append(out.Directories, dir)
		}
	}
	// The earliest install is when this application first wrote into that
	// Minecraft, which is what the record is a record of.
	if !held.InstalledAt.IsZero() && held.InstalledAt.Before(out.InstalledAt) {
		out.InstalledAt = held.InstalledAt
	}
	return out
}
