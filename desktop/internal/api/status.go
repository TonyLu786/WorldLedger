package api

import (
	"net/http"
	"os"

	"github.com/worldledger/worldledger-mc/desktop/internal/app"
	"github.com/worldledger/worldledger-mc/internal/archive"
	"github.com/worldledger/worldledger-mc/internal/mcpath"
	"github.com/worldledger/worldledger-mc/internal/policy"
	"github.com/worldledger/worldledger-mc/internal/spool"
)

// Status is the one screen that answers "did any of this work".
//
// The command line makes a person assemble this from three commands and know
// which to run. It is the same information; what is added is the ordering,
// because a player who has just played an evening wants to know whether it was
// recorded before they want to know how many objects are in a content-addressed
// store.
type Status struct {
	ArchiveDir string `json:"archive_dir"`

	Observations int      `json:"observations"`
	Objects      int      `json:"objects"`
	ObjectBytes  int64    `json:"object_bytes"`
	Servers      []Server `json:"servers"`

	// Spool is what is waiting to be brought in. Absent when the mod has never
	// run, which is a different thing from nothing having been captured.
	Spool *SpoolState `json:"spool,omitempty"`

	// Next is what to do about all of it, in one sentence.
	Next string `json:"next"`
}

// Server is one place the player has been, and whether they have said what may
// happen to what they saw there.
type Server struct {
	ID          string `json:"id"`
	Chunks      int    `json:"chunks"`
	Disposition string `json:"disposition,omitempty"`
	Declared    bool   `json:"declared"`
}

type SpoolState struct {
	Dir         string `json:"dir"`
	Ready       int    `json:"ready"`
	InProgress  int    `json:"in_progress"`
	Quarantined int    `json:"quarantined"`
	// Imported are captures already in the archive and kept in the capture
	// folder anyway, so the page can say the recordings still exist rather than
	// leaving somebody to assume they were consumed.
	Imported int `json:"imported"`
}

func handleStatus(w http.ResponseWriter, r *http.Request) {
	a, err := openArchive()
	if err != nil {
		app.WriteFailure(w, http.StatusInternalServerError, err.Error(),
			"the archive could not be opened; restarting the application is the first thing to try")
		return
	}

	status := Status{ArchiveDir: a.Root}
	manifest, err := a.Manifest()
	if err != nil {
		app.WriteFailure(w, http.StatusInternalServerError,
			"the archive could not be read: "+err.Error(),
			"this usually means the archive is damaged; it can be checked from the command line with fsck")
		return
	}
	status.Observations = manifest.Observations
	status.Objects = manifest.Objects
	status.ObjectBytes = manifest.ObjectBytes
	status.Servers = describeServers(a, manifest)
	status.Spool = readSpoolState()
	status.Next = nextStep(status)

	app.WriteJSON(w, http.StatusOK, status)
}

// describeServers pairs what was seen with whether it may be used.
//
// A server with no declaration is not an error and is not hidden. It is the
// normal state after a first evening, and it is exactly what export will refuse
// later, so it is better said now than discovered then.
func describeServers(a archive.Archive, manifest archive.Manifest) []Server {
	store := policy.NewStore(a.Root)
	servers := make([]Server, 0, len(manifest.Servers))
	for _, entry := range manifest.Servers {
		server := Server{ID: entry.Server}
		for _, dimension := range entry.Dimensions {
			server.Chunks += dimension.Chunks
		}
		if declared, found, err := store.Lookup(entry.Server); err == nil && found {
			server.Declared = true
			server.Disposition = string(declared.Disposition)
		}
		servers = append(servers, server)
	}
	return servers
}

// readSpoolState reports what is waiting, or nothing at all when there is no
// spool to read. A missing spool is not a failure here: the status screen is
// often the first thing somebody opens, before the mod has ever run.
func readSpoolState() *SpoolState {
	dir, _, found := mcpath.FindSpool()
	if !found {
		return nil
	}
	contents, err := spool.Read(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return &SpoolState{Dir: dir}
	}
	return &SpoolState{
		Dir:         dir,
		Ready:       len(contents.Ready),
		InProgress:  contents.InProgress,
		Quarantined: contents.Quarantined,
		Imported:    contents.Imported,
	}
}

// nextStep is the sentence the screen leads with.
//
// The order is the order of the path: something waiting to be brought in comes
// before anything else, because leaving it in the spool is the only state where
// data can still be lost. A declaration comes next because it is what blocks
// making a world.
func nextStep(status Status) string {
	if status.Spool != nil && status.Spool.Ready > 0 {
		return "import"
	}
	if status.Observations == 0 {
		if status.Spool == nil {
			return "install"
		}
		return "play"
	}
	for _, server := range status.Servers {
		if !server.Declared {
			return "declare"
		}
	}
	return "export"
}
