package api

import (
	"net/http"

	"github.com/worldledger/worldledger-mc/desktop/internal/app"
	"github.com/worldledger/worldledger-mc/internal/mcpath"
	"github.com/worldledger/worldledger-mc/internal/spool"
)

// Clearing recordings that have already been taken in.
//
// Keeping them is the right default and stays the default: until an import has
// returned, the spool holds the only copy of what somebody saw, and a window
// cannot delete that on the strength of one button. But a default is not a
// policy, and this one has a cost that nobody was being shown. A player who
// captures an evening a week accumulates hundreds of megabytes inside their
// Minecraft directory, and until now the application mentioned neither the size
// nor a way to do anything about it.
//
// So the size is reported on the play screen and this is what acts on it. It
// removes only bundles the archive has already taken in -- which is what the
// imported- prefix means, and that prefix is only written after an import
// returned, which is after the observation was forced to disk. Anything else in
// the folder is left where it is, including quarantined bundles, which exist
// precisely so somebody can look at them.

type tidyResult struct {
	Removed int   `json:"removed"`
	Freed   int64 `json:"freed"`
	// Remaining is what is still in the folder afterwards, so the page can
	// report the new state rather than asking for it again and hoping.
	Remaining int `json:"remaining"`
}

func handleTidy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		app.WriteFailure(w, http.StatusMethodNotAllowed,
			"clearing recordings has to be asked for",
			"use the button on the play screen")
		return
	}

	dir, _, found := mcpath.FindSpool()
	if !found {
		app.WriteFailure(w, http.StatusNotFound,
			"there is no recordings folder to clear",
			"nothing was changed")
		return
	}
	contents, err := spool.Read(dir)
	if err != nil {
		app.WriteFailure(w, http.StatusInternalServerError,
			"the recordings folder could not be read: "+err.Error(),
			"nothing was changed; check that "+dir+" still exists")
		return
	}
	if len(contents.Imported) == 0 {
		app.WriteFailure(w, http.StatusOK,
			"there is nothing to clear",
			"only recordings already added to your archive can be cleared, and there are none")
		return
	}

	removed, freed, err := spool.Discard(contents.Imported)
	if err != nil {
		app.WriteFailure(w, http.StatusInternalServerError,
			"not all of it could be cleared: "+err.Error(),
			"what was removed is gone and the rest is untouched; try again, or check that Minecraft is closed")
		return
	}

	after, err := spool.Read(dir)
	if err != nil {
		// The removal happened; failing to count afterwards is not a failure of
		// it, and reporting one would send somebody looking for a problem that
		// is not there.
		app.WriteJSON(w, http.StatusOK, tidyResult{Removed: removed, Freed: freed})
		return
	}
	app.WriteJSON(w, http.StatusOK, tidyResult{
		Removed:   removed,
		Freed:     freed,
		Remaining: len(after.Ready) + len(after.Imported) + after.Quarantined + after.InProgress,
	})
}
