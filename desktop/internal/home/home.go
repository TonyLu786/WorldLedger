// Package home is where the application keeps the player's archive.
//
// The command line takes a directory because whoever runs it has one in mind.
// A player does not, and asking would be asking them to make a filing decision
// before they know what is being filed. So there is one place, it is the
// platform's own directory for application data, and the application says where
// it is rather than hiding it.
//
// It is deliberately not inside .minecraft. An archive is the player's record
// and outlives any particular Minecraft directory; putting it there would mean
// a reinstall of the game quietly takes the archive with it.
package home

import (
	"errors"
	"os"
	"path/filepath"
)

// overrideEnv lets a test, or somebody with a reason, put the archive
// elsewhere. Reading it here rather than in each caller keeps one answer.
const overrideEnv = "WORLDLEDGER_HOME"

// Dir is the application's own directory.
func Dir() (string, error) {
	if override := os.Getenv(overrideEnv); override != "" {
		return override, nil
	}
	config, err := os.UserConfigDir()
	if err != nil {
		return "", errors.New(
			"could not work out where this system keeps application data, " +
				"so there is nowhere to put the archive")
	}
	return filepath.Join(config, "WorldLedger"), nil
}

// ArchiveDir is where observations are kept.
func ArchiveDir() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "archive"), nil
}
