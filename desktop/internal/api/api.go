// Package api answers what the page asks.
//
// Handlers here are thin on purpose. Everything they do is done by the archive
// core, unchanged and called directly rather than by running the command line
// and reading its output: parsing a program's prose is how a wording change
// becomes a broken application.
//
// What this layer does add is the shape a person needs. The core answers
// precise questions -- how many observations, which servers, what does this
// policy say -- and a player has one question, which is what to do next. Every
// response here carries that.
package api

import (
	"net/http"
	"os"

	"github.com/worldledger/worldledger-mc/desktop/internal/app"
	"github.com/worldledger/worldledger-mc/desktop/internal/health"
	"github.com/worldledger/worldledger-mc/desktop/internal/home"
	"github.com/worldledger/worldledger-mc/internal/archive"
	"github.com/worldledger/worldledger-mc/internal/mcpath"
)

// Mount registers every endpoint the page uses.
//
// What a handler needs beyond the request is handed to it here: the watchdog
// that long work holds open, and where installing gets its files. The watchdog
// used to be a package variable that Mount assigned and two handlers read,
// which is safe in the program, where Mount runs once. The tests start a
// server for every test, and that made it one variable shared by all of them,
// kept in order only because each handler happened to read it before
// answering. What each server's handlers are given, nobody else can reach.
func Mount(server *app.Server, watchdog *app.Watchdog, supply Supply) {
	longWork := hold(watchdog.Hold)

	server.HandleFunc("/api/notice", handleNotice)
	server.HandleFunc("/api/health", handleHealth)
	server.HandleFunc("/api/status", handleStatus)
	server.HandleFunc("/api/import", func(w http.ResponseWriter, r *http.Request) {
		handleImport(w, r, longWork)
	})
	server.HandleFunc("/api/tidy", handleTidy)
	server.HandleFunc("/api/choices", handleChoices)
	server.HandleFunc("/api/declare", handleDeclare)
	server.HandleFunc("/api/worlds", handleWorlds)
	server.HandleFunc("/api/export", func(w http.ResponseWriter, r *http.Request) {
		handleExport(w, r, longWork)
	})
	server.HandleFunc("/api/moments", handleMoments)
	server.HandleFunc("/api/travel", handleTravel)
	server.HandleFunc("/api/plan", func(w http.ResponseWriter, r *http.Request) {
		handlePlan(w, r, supply)
	})
	server.HandleFunc("/api/install", func(w http.ResponseWriter, r *http.Request) {
		handleInstall(w, r, supply)
	})
	server.HandleFunc("/api/uninstall", handleUninstall)
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	install, candidates, found := mcpath.FindInstall()
	if !found {
		looked := make([]string, 0, len(candidates))
		for _, candidate := range candidates {
			looked = append(looked, candidate.Root)
		}
		app.WriteJSON(w, http.StatusOK, health.NotFound(looked))
		return
	}
	app.WriteJSON(w, http.StatusOK, health.Inspect(install))
}

// openArchive opens the player's archive, creating it the first time.
//
// Creating on demand is safe because an empty archive is a valid one, and it
// removes a step that exists only to satisfy the program: nobody sat down to
// initialise an archive, they sat down to keep what they saw.
func openArchive() (archive.Archive, error) {
	dir, err := home.ArchiveDir()
	if err != nil {
		return archive.Archive{}, err
	}
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return archive.Init(dir)
	}
	return archive.Open(dir)
}

// A hold marks work that must outlive a quiet page, and returns what releases
// it. An import of two hundred bundles can take longer than the watchdog's
// patience, and a page waiting for it is not a page nobody is looking at.
type hold func() func()
