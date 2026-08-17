package api

import (
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/worldledger/worldledger-mc/desktop/internal/app"
	"github.com/worldledger/worldledger-mc/internal/mcpath"
)

// Choosing where the world goes is the one place this path cannot be made to
// disappear, and it should not be.
//
// Export writes into a world that Minecraft made. It never writes level.dat,
// because a world's seed, generator, build height and game rules are server
// state nobody observed, and inventing a plausible one is exactly the kind of
// guessing this project exists to refuse. So there has to be a world already,
// and somebody has to say which.
//
// What can be removed is the part where they type a path. The saves are listed,
// the newest first, with enough about each to tell a world just made for this
// from the survival world they have played for a year -- because writing
// observations over the second one is the mistake that cannot be undone by us.

// World is one entry in the player's saves folder.
type World struct {
	Name       string `json:"name"`
	Path       string `json:"path"`
	LastPlayed string `json:"last_played"`
	Bytes      int64  `json:"bytes"`
	// Sizeable marks a world large enough to be one somebody has played in.
	//
	// It is a guess and is presented as one. A freshly created world is a few
	// megabytes of spawn chunks; a world played for a season is hundreds. The
	// page uses this to ask for confirmation rather than to refuse, because
	// there are good reasons to export into a world that already has terrain
	// and no good reason for us to decide that for somebody.
	Sizeable bool `json:"sizeable"`
}

// sizeableBytes is where a world stops looking freshly made. Minecraft writes
// the spawn area on creation, which lands in the low megabytes; anything past
// this has had somebody walking around in it.
const sizeableBytes = 40 << 20

type worldsAnswer struct {
	SavesDir string  `json:"saves_dir"`
	Worlds   []World `json:"worlds"`
	// HowToMake is shown when there is nothing to choose. It is the same three
	// steps the command line gives, in the same order, because they are the
	// steps.
	HowToMake []string `json:"how_to_make,omitempty"`
}

func handleWorlds(w http.ResponseWriter, r *http.Request) {
	install, _, found := mcpath.FindInstall()
	if !found {
		app.WriteFailure(w, http.StatusNotFound,
			"Minecraft was not found on this computer",
			"install Minecraft and play it once, then come back")
		return
	}

	answer := worldsAnswer{SavesDir: install.Saves()}
	entries, err := os.ReadDir(install.Saves())
	if err != nil && !os.IsNotExist(err) {
		app.WriteFailure(w, http.StatusInternalServerError,
			"the saves folder could not be read: "+err.Error(),
			"check that "+install.Saves()+" still exists")
		return
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(install.Saves(), entry.Name())
		// A directory without level.dat is not a world Minecraft made, and it is
		// the one thing export will refuse. Leaving it out of the list is better
		// than offering a choice that cannot work.
		info, err := os.Stat(filepath.Join(dir, "level.dat"))
		if err != nil {
			continue
		}
		size := directorySize(dir)
		answer.Worlds = append(answer.Worlds, World{
			Name:       entry.Name(),
			Path:       dir,
			LastPlayed: info.ModTime().UTC().Format(time.RFC3339),
			Bytes:      size,
			Sizeable:   size >= sizeableBytes,
		})
	}

	// Newest first, so the world somebody just made for this is at the top.
	sort.Slice(answer.Worlds, func(i, j int) bool {
		return answer.Worlds[i].LastPlayed > answer.Worlds[j].LastPlayed
	})

	if len(answer.Worlds) == 0 {
		answer.HowToMake = []string{
			"In Minecraft, choose Singleplayer, then Create New World.",
			"Give it a name and click Create.",
			"Leave the world and quit to the title screen.",
		}
	}
	app.WriteJSON(w, http.StatusOK, answer)
}

// directorySize adds up what is under a directory.
//
// Errors are ignored on purpose. This number is only used to ask a question,
// and a world that is partly unreadable should still be listed rather than
// disappearing from the choices because one file could not be stat'd.
func directorySize(dir string) int64 {
	var total int64
	filepath.WalkDir(dir, func(_ string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		if info, err := entry.Info(); err == nil {
			total += info.Size()
		}
		return nil
	})
	return total
}
