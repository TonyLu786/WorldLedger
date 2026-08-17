package api

import (
	"net/http"
	"path/filepath"
	"sync"

	"github.com/worldledger/worldledger-mc/desktop/internal/app"
	"github.com/worldledger/worldledger-mc/internal/bundle"
	"github.com/worldledger/worldledger-mc/internal/mcpath"
	"github.com/worldledger/worldledger-mc/internal/spool"
)

// Importing moves what was captured into the archive.
//
// Two decisions here differ from the command line's defaults, and both are
// because the person cannot see what is happening.
//
// Bundles are kept rather than deleted. ingest-spool removes them by default,
// which is right for somebody watching output scroll past and wrong for a
// window: the spool is the only copy of what was seen until the import has
// finished, and a player who is unsure what just happened should still have it.
//
// A bundle that fails does not stop the ones after it. One damaged capture out
// of two hundred should cost that one capture.

// importResult is what the page shows afterwards.
type importResult struct {
	Imported int      `json:"imported"`
	Total    int      `json:"total"`
	Failed   []string `json:"failed,omitempty"`
	// Kept says the spool was left alone, so the page can say so rather than
	// leaving somebody to wonder whether their captures were consumed.
	Kept bool `json:"kept"`
}

// Only one import may run at a time. Two concurrent imports of the same spool
// would both read the same bundles, and while the archive would survive it --
// an observation imported twice is the same observation -- the counts reported
// to the person would be nonsense.
var importing sync.Mutex

func handleImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		app.WriteFailure(w, http.StatusMethodNotAllowed,
			"importing has to be asked for explicitly", "use the button on the import screen")
		return
	}
	if !importing.TryLock() {
		app.WriteFailure(w, http.StatusConflict,
			"an import is already running", "wait for it to finish")
		return
	}
	defer importing.Unlock()
	defer holdDuringLongWork()()

	dir, candidates, found := mcpath.FindSpool()
	if !found {
		looked := "nowhere to look"
		if len(candidates) > 0 {
			looked = filepath.Dir(candidates[0])
		}
		app.WriteFailure(w, http.StatusNotFound,
			"there is nothing to import: no capture folder exists yet",
			"install the mod and play once. The folder appears under "+looked)
		return
	}

	contents, err := spool.Read(dir)
	if err != nil {
		app.WriteFailure(w, http.StatusInternalServerError,
			"the capture folder could not be read: "+err.Error(),
			"check that "+dir+" still exists")
		return
	}
	if len(contents.Ready) == 0 {
		next := "play on a server, then come back"
		if contents.InProgress > 0 {
			next = "Minecraft is still running and writing captures; quit the game first"
		}
		app.WriteFailure(w, http.StatusOK, "nothing has been captured yet", next)
		return
	}

	a, err := openArchive()
	if err != nil {
		app.WriteFailure(w, http.StatusInternalServerError, err.Error(),
			"the archive could not be opened; restarting the application is the first thing to try")
		return
	}

	result := importResult{Total: len(contents.Ready), Kept: true}
	for _, path := range contents.Ready {
		// DeleteOnSuccess stays false: see the note at the top of this file.
		if _, err := bundle.Import(a, path, bundle.Options{
			Limits: bundle.DefaultLimits(),
		}); err != nil {
			result.Failed = append(result.Failed, filepath.Base(path)+": "+err.Error())
			continue
		}
		result.Imported++
		// Marked only after the import returned, which is after the archive has
		// forced the observation to disk. A failure to rename is not a failure
		// to import, and saying so is better than reporting a loss that did not
		// happen: the bundle will simply be offered again, and importing it
		// twice is the same observation.
		if err := spool.MarkImported(path); err != nil {
			result.Failed = append(result.Failed,
				filepath.Base(path)+": imported, but could not be marked as done, so it will be offered again")
		}
	}
	app.WriteJSON(w, http.StatusOK, result)
}
