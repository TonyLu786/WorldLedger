package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/worldledger/worldledger-mc/internal/archive"
	"github.com/worldledger/worldledger-mc/internal/policy"
)

// The adapter writes its spool under the Minecraft config directory, and until
// now a person had to know that and type it. Nothing about the path is a
// decision they made: it follows from where the launcher put Minecraft.
//
// So the path is worked out rather than demanded, and the directory that was
// chosen is always printed. A tool that silently picks a directory and imports
// from it is worse than one that asks, because the mistake is invisible.

const spoolSuffix = "config/worldledger/spool"

// spoolCandidates lists where an unmodified launcher puts Minecraft, most
// likely first. The environment variables are read rather than assumed so a
// test can point them somewhere harmless.
func spoolCandidates() []string {
	var roots []string
	switch runtime.GOOS {
	case "windows":
		if appData := os.Getenv("APPDATA"); appData != "" {
			roots = append(roots, filepath.Join(appData, ".minecraft"))
		}
	case "darwin":
		if home, err := os.UserHomeDir(); err == nil {
			roots = append(roots,
				filepath.Join(home, "Library", "Application Support", "minecraft"),
				filepath.Join(home, ".minecraft"))
		}
	default:
		if home, err := os.UserHomeDir(); err == nil {
			roots = append(roots, filepath.Join(home, ".minecraft"))
		}
	}
	out := make([]string, 0, len(roots))
	for _, root := range roots {
		out = append(out, filepath.Join(root, filepath.FromSlash(spoolSuffix)))
	}
	return out
}

// findSpool returns the first candidate that exists.
//
// A candidate that exists but holds nothing is still the answer: an empty spool
// means nothing was captured, which is a different problem from not finding
// Minecraft, and the caller reports them differently.
func findSpool() (string, error) {
	candidates := spoolCandidates()
	for _, candidate := range candidates {
		info, err := os.Stat(candidate)
		if err == nil && info.IsDir() {
			return candidate, nil
		}
	}
	if len(candidates) == 0 {
		return "", fmt.Errorf(
			"could not work out where Minecraft keeps its files on this system; pass the spool directory:\n  %s",
			usageLine("ingest-spool"))
	}
	return "", fmt.Errorf(
		"no capture spool found. Looked in:\n%s\n\n"+
			"That directory appears once the mod has run with a contributor set. "+
			"If Minecraft lives elsewhere, pass the directory:\n  %s",
		indentAll(candidates), usageLine("ingest-spool"))
}

// undeclaredServers lists the servers with no publication decision, which are
// exactly the servers export will refuse. status works this out too; this is
// the same question asked from the command that creates the situation.
func undeclaredServers(a archive.Archive, servers []string) []string {
	store := policy.NewStore(a.Root)
	var undeclared []string
	for _, server := range servers {
		_, found, err := store.Lookup(server)
		if err != nil {
			// A policy that cannot be read is a real problem, but it belongs to
			// whatever command is about to refuse the export rather than to a
			// closing hint. Saying nothing here is better than guessing.
			continue
		}
		if !found {
			undeclared = append(undeclared, server)
		}
	}
	return undeclared
}

func indentAll(paths []string) string {
	var out strings.Builder
	for i, path := range paths {
		if i > 0 {
			out.WriteByte('\n')
		}
		out.WriteString("  ")
		out.WriteString(path)
	}
	return out.String()
}
