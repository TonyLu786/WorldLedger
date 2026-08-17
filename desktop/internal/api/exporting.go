package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/worldledger/worldledger-mc/desktop/internal/app"
	"github.com/worldledger/worldledger-mc/internal/anvil"
	"github.com/worldledger/worldledger-mc/internal/archive"
	"github.com/worldledger/worldledger-mc/internal/policy"
	"github.com/worldledger/worldledger-mc/internal/reconstruct"
)

// Making a world is the last step, and the only one that writes outside our own
// directory. Two things guard it, and neither is negotiable here.
//
// The declaration is checked first. Export is what turns a private record into
// something a person can hand to somebody else, and it is refused until a named
// person has said what may happen to it. The page cannot reach this without
// having been through that screen, and it is checked again here anyway: a
// guard that only exists in the interface is not a guard.
//
// The world has to exist. internal/anvil refuses to write level.dat, and this
// does not work around that by making one.

var exporting sync.Mutex

type exportRequest struct {
	Server    string `json:"server"`
	Dimension string `json:"dimension"`
	WorldDir  string `json:"world_dir"`
	// At is optional and RFC3339. Empty means now, which selects the newest
	// state of every chunk.
	At string `json:"at,omitempty"`
}

type exportAnswer struct {
	Chunks      int      `json:"chunks"`
	RegionFiles []string `json:"region_files"`
	WorldDir    string   `json:"world_dir"`
	Unknown     int      `json:"unknown"`
	Withheld    int      `json:"withheld,omitempty"`
}

func handleExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		app.WriteFailure(w, http.StatusMethodNotAllowed,
			"making a world has to be asked for", "use the button on the make a world screen")
		return
	}
	if !exporting.TryLock() {
		app.WriteFailure(w, http.StatusConflict,
			"a world is already being written", "wait for it to finish")
		return
	}
	defer exporting.Unlock()

	var request exportRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&request); err != nil {
		app.WriteFailure(w, http.StatusBadRequest, "the request could not be read", "try again")
		return
	}
	if request.Server == "" || request.WorldDir == "" {
		app.WriteFailure(w, http.StatusBadRequest,
			"a server and a world are both needed", "pick a world from the list")
		return
	}
	if request.Dimension == "" {
		request.Dimension = "minecraft:overworld"
	}

	at := time.Now().UTC()
	if request.At != "" {
		parsed, err := time.Parse(time.RFC3339Nano, request.At)
		if err != nil {
			app.WriteFailure(w, http.StatusBadRequest,
				"that moment could not be read", "pick a time from the list")
			return
		}
		at = parsed.UTC()
	}

	a, err := openArchive()
	if err != nil {
		app.WriteFailure(w, http.StatusInternalServerError, err.Error(),
			"the archive could not be opened; restarting the application is the first thing to try")
		return
	}

	if !declared(a, request.Server) {
		app.WriteFailure(w, http.StatusForbidden,
			"nothing has been said about what may happen to what you recorded on "+request.Server,
			"go to Decide and make that choice first")
		return
	}

	snapshot, inputs, err := reconstruct.SnapshotAt(a, request.Server, request.Dimension, at)
	if err != nil {
		app.WriteFailure(w, http.StatusInternalServerError, err.Error(),
			"this usually means the archive is damaged")
		return
	}

	sources := make([]anvil.ChunkSource, 0, len(snapshot.Selections))
	for _, selection := range snapshot.Selections {
		if !selection.Known() {
			continue
		}
		sources = append(sources, anvil.ChunkSource{Chunk: selection.Chunk, Observation: *selection.Selected})
	}
	if len(sources) == 0 {
		app.WriteFailure(w, http.StatusOK,
			"there is nothing recorded for "+request.Server+" at that moment",
			"import your captures first, or pick a later moment")
		return
	}

	prepared, err := anvil.Prepare(a.CAS, sources)
	if err != nil {
		app.WriteFailure(w, http.StatusInternalServerError,
			"the recordings could not be read back: "+err.Error(),
			"this usually means the archive is damaged")
		return
	}

	// Asked before exporting, so that the one failure with a known cause can be
	// told apart from every other one.
	//
	// Collapsing them was worse than useless: the first version of this reported
	// "that folder is not a Minecraft world" for any failure at all, which sent
	// somebody to create a world they already had while the real problem went
	// unmentioned. A message that is wrong is worse than a message that is
	// vague, because it is actionable.
	if _, err := os.Stat(filepath.Join(request.WorldDir, "level.dat")); os.IsNotExist(err) {
		app.WriteFailure(w, http.StatusBadRequest,
			"that folder is not a Minecraft world yet",
			"in Minecraft: Singleplayer, Create New World, then quit to the title screen, and check again")
		return
	}

	report, err := anvil.Export(prepared, anvil.ExportRequest{
		WorldDir:    request.WorldDir,
		Dimension:   request.Dimension,
		DataVersion: anvil.DataVersion26_2,
		Overwrite:   true,
	})
	if err != nil {
		app.WriteFailure(w, http.StatusInternalServerError,
			"the world could not be written: "+err.Error(),
			"check that Minecraft is closed and that there is room on the disk, then try again")
		return
	}

	answer := exportAnswer{
		Chunks:      report.Chunks,
		RegionFiles: shortNames(report.RegionFiles),
		WorldDir:    request.WorldDir,
		Unknown:     snapshot.Summary.Unknown,
		Withheld:    inputs.Withheld,
	}
	app.WriteJSON(w, http.StatusOK, answer)
}

// declared reports whether somebody has said what may happen to this server's
// observations.
func declared(a archive.Archive, server string) bool {
	_, found, err := policy.NewStore(a.Root).Lookup(server)
	return err == nil && found
}

// shortNames keeps the file names without the path. A player has no use for the
// full path of each region file, and the world folder is reported once.
func shortNames(paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		out = append(out, filepath.Base(path))
	}
	return out
}
