package api

import (
	"net/http"
	"os"

	"github.com/worldledger/worldledger-mc/desktop/internal/app"
	"github.com/worldledger/worldledger-mc/desktop/internal/health"
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

	// Capturing is whether the mod is in place to record anything at all.
	//
	// It is here rather than left to the set-up screen because a spool folder
	// outlives the mod that made it. Somebody who has removed the mod, or whose
	// launcher replaced the mods folder, still has the folder and every capture
	// in it, and a page that reads the folder's existence as "you are recording"
	// tells them to go and play while nothing is being kept.
	Capturing bool `json:"capturing"`

	// Contributor is the name captures are recorded under, when the mod has
	// been set up. The declaration screen offers it as the default for who is
	// deciding, because in almost every case it is the same person and making
	// them type it again is asking a question already answered.
	Contributor string `json:"contributor,omitempty"`

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
	// Dimensions are the worlds recorded on that server, with the count for
	// each.
	//
	// The page never sent one, so export, moments and travel all defaulted to
	// the overworld while this count summed every dimension. A player whose
	// evening was in the Nether was told "800 places recorded" and then, on the
	// next screen, that there was nothing recorded at that moment. Three
	// screens disagreeing about the same archive, and none of them ever saying
	// the word "overworld".
	Dimensions []Dimension `json:"dimensions"`
}

// Dimension is one world of one server.
type Dimension struct {
	ID     string `json:"id"`
	Chunks int    `json:"chunks"`
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
	// Unreadable carries why the folder could not be read, when it could not be.
	// Empty on the ordinary path.
	Unreadable string `json:"unreadable,omitempty"`
	// ImportedBytes is what those are costing inside the Minecraft directory.
	//
	// Keeping them is the safe default and stays the default. Not saying what
	// they weigh is how a player ends up with gigabytes under .minecraft that
	// nothing in this application ever mentioned, so the number is reported and
	// there is a way to act on it.
	ImportedBytes int64 `json:"imported_bytes"`
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
	status.Capturing, status.Contributor = captureState()
	status.Next = nextStep(status)

	app.WriteJSON(w, http.StatusOK, status)
}

// captureState answers whether anything would be recorded if the player started
// the game now, and under whose name.
//
// It asks the same checks the set-up screen shows rather than a second opinion
// of its own. Two places deciding separately whether capture works is how the
// two screens come to disagree in front of somebody who has to believe one of
// them.
func captureState() (capturing bool, contributor string) {
	install, _, found := mcpath.FindInstall()
	if !found {
		return false, ""
	}
	report := health.Inspect(install)
	for _, check := range report.Checks {
		if check.ID == "contributor" && check.State == health.OK {
			contributor = check.Detail
		}
	}
	return report.Ready, contributor
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
			server.Dimensions = append(server.Dimensions, Dimension{
				ID: dimension.Dimension, Chunks: dimension.Chunks,
			})
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
		// A folder that cannot be read is not an empty folder. Returning zero
		// counts made the play screen say "Nothing new since last time" about a
		// folder it had failed to open, while the import screen, given the
		// same folder, said so honestly. Two screens disagreeing about one
		// directory, and the reassuring one was wrong.
		return &SpoolState{Dir: dir, Unreadable: err.Error()}
	}
	return &SpoolState{
		Dir:           dir,
		Ready:         len(contents.Ready),
		InProgress:    contents.InProgress,
		Quarantined:   contents.Quarantined,
		Imported:      len(contents.Imported),
		ImportedBytes: kept.total(contents.Imported),
	}
}

// nextStep is the sentence the screen leads with.
//
// The order is the order of the path, with two exceptions that are the whole
// reason this is worked out here rather than by the page.
//
// Anything waiting to be brought in comes first, whatever else is true. The
// spool is the only place observed state exists in one copy, so emptying it is
// the only step where waiting can still lose something.
//
// What can already be done comes before what would have to be set up again. A
// player who has removed the mod, or whose launcher replaced the mods folder,
// still has an archive they can declare and make a world from, and sending them
// back to Set up would read as the application having forgotten all of it.
func nextStep(status Status) string {
	if status.Spool != nil && status.Spool.Ready > 0 {
		return "import"
	}
	if status.Observations > 0 {
		for _, server := range status.Servers {
			if !server.Declared {
				return "declare"
			}
		}
		return "export"
	}
	if !status.Capturing {
		return "install"
	}
	return "play"
}
