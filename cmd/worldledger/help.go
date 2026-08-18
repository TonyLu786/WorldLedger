package main

import (
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
)

// commandUsage is the one place each command's shape is written down. It is
// what a missing flag reports and what --help prints, so the two cannot drift
// apart the way they do when each command carries its own string.
//
// Subcommands are keyed by their full path, because "worldledger policy set"
// and "worldledger policy" answer different questions and a reader asking about
// one is not helped by the other.
var commandUsage = map[string]string{
	"init":          "init <archive-dir>",
	"ingest":        "ingest --archive DIR --server ID --dimension DIM --x X --z Z --observed-at TIME --contributor ID [options] <payload-file>",
	"ingest-bundle": "ingest-bundle --archive DIR [--delete-on-success] <bundle-dir>",
	"ingest-spool":  "ingest-spool --archive DIR [--keep] [--dry-run] [<spool-dir>]",
	"inspect":       "inspect --archive DIR --server ID --dimension DIM --x X --z Z",
	"verify":        "verify --archive DIR --server ID --dimension DIM --x X --z Z [--window 10s]",
	"coverage":      "coverage --archive DIR --server ID --dimension DIM [--at TIME] [--landmark NAME] [--json] [--map FILE]",
	"epoch":         "epoch --archive DIR --server ID --dimension DIM [--at TIME] [--out FILE] [--compare FILE] [--json]",
	"diff":          "diff --archive DIR --server ID --dimension DIM [--from TIME] [--to TIME] [--since DUR] [--json]",
	"export":        "export --archive DIR --server ID --dimension DIM --into WORLD_DIR [--at TIME] [--overwrite]",
	"convert":       "convert --archive DIR --server ID --dimension DIM --into WORLD_DIR --target-profile FILE [--rules FILE] [--on-unrepresentable POLICY]",
	"seed":          "seed --observations FILE --operator NAME --accept-terms [--seed N | --from A --to B] [--out FILE]",
	"status":        "status --archive DIR [--spool DIR]",
	"corpus":        "corpus --archive DIR [--server ID] [--dimension DIM]",
	"fsck":          "fsck --archive DIR",
	"manifest":      "manifest --archive DIR [--out FILE] [--compare FILE]",
	"fingerprint":   "fingerprint (--archive DIR | --file FILE) [--server ID] [--out FILE] [--compare FILE]",
	"send":          "send --archive DIR --to FINGERPRINT --out DIR [--their-manifest FILE]",
	"receive":       "receive --archive DIR <bundle-dir>",

	"policy":      "policy <show|set|list> --archive DIR [flags]",
	"policy show": "policy show --archive DIR --server ID",
	"policy set":  "policy set --archive DIR --server ID --disposition D --declared-by NAME [--until TIME] [--note TEXT]",
	"policy list": "policy list --archive DIR",

	"redact":          "redact <set|list|withdraw|purge> --archive DIR [flags]",
	"redact set":      "redact set --archive DIR --server ID --reason TEXT --declared-by NAME [--contributor NAME] [--dimension ID] [--region minX,minZ,maxX,maxZ]",
	"redact list":     "redact list --archive DIR",
	"redact withdraw": "redact withdraw --archive DIR --id ID",
	"redact purge":    "redact purge --archive DIR [--yes]",

	"landmark":        "landmark <set|list|remove> --archive DIR [flags]",
	"landmark set":    "landmark set --archive DIR --server ID --name NAME --region minX,minZ,maxX,maxZ --declared-by NAME [--dimension ID] [--note TEXT]",
	"landmark list":   "landmark list --archive DIR",
	"landmark remove": "landmark remove --archive DIR --server ID --name NAME [--dimension ID]",

	"identity":          "identity <create|register|list|remove> --archive DIR [flags]",
	"identity create":   "identity create --archive DIR --label NAME --declared-by NAME --key-out FILE",
	"identity register": "identity register --archive DIR --label NAME --public-key HEX --declared-by NAME [--note TEXT]",
	"identity list":     "identity list --archive DIR",
	"identity remove":   "identity remove --archive DIR --fingerprint FP",

	"attest":        "attest <sign|verify> --archive DIR [flags]",
	"attest sign":   "attest sign --archive DIR --key FILE",
	"attest verify": "attest verify --archive DIR",

	"version": "version",
	"help":    "help [command]",
}

// usageError is what a command returns when it was not given enough to run.
func usageError(name string) error {
	return errors.New("usage: worldledger " + usageLine(name))
}

func usageLine(name string) string {
	if line, ok := commandUsage[name]; ok {
		return line
	}
	// A command with no entry is a mistake caught by TestEveryCommandHasUsage,
	// but printing its name is more use at runtime than printing nothing.
	return name
}

// helpRequest reports the command a --help, -h, or "help <command>" refers to.
//
// Asking for help is not an error, and every command answers the same three
// spellings, because a person who has to discover which one a given tool wants
// has already been let down by it.
func helpRequest(args []string) (string, bool) {
	if len(args) == 0 {
		return "", false
	}
	if isHelpFlag(args[0]) {
		// "help --help" asks about help, not about a command called "--help".
		// Dropping the spellings rather than joining them blindly is what keeps
		// every combination of the three from being an unknown command.
		var name []string
		for _, arg := range args[1:] {
			if !isHelpFlag(arg) {
				name = append(name, arg)
			}
		}
		return strings.Join(name, " "), true
	}
	for _, arg := range args[1:] {
		// Stop at "--": everything after it is an operand, and a file genuinely
		// named -h should not silently turn a command into a help request.
		if arg == "--" {
			break
		}
		if isHelpFlag(arg) {
			return commandPath(args), true
		}
	}
	return "", false
}

func isHelpFlag(arg string) bool {
	return arg == "help" || arg == "--help" || arg == "-h"
}

// commandPath is the longest known command the arguments name, so that
// "policy set --help" answers about the subcommand rather than about policy.
func commandPath(args []string) string {
	if len(args) >= 2 {
		pair := args[0] + " " + args[1]
		if _, ok := commandUsage[pair]; ok {
			return pair
		}
	}
	return args[0]
}

// printHelp writes usage for one command, or the whole tool when name is empty.
func printHelp(w io.Writer, name string) error {
	if name == "" {
		usage(w)
		return nil
	}
	line, ok := commandUsage[name]
	if !ok {
		return unknownCommandError(name)
	}
	fmt.Fprintln(w, "usage: worldledger "+line)
	if subs := subcommandsOf(name); len(subs) > 0 {
		fmt.Fprintln(w)
		for _, sub := range subs {
			fmt.Fprintln(w, "  worldledger "+commandUsage[sub])
		}
	}
	return nil
}

func subcommandsOf(name string) []string {
	prefix := name + " "
	var out []string
	for key := range commandUsage {
		if strings.HasPrefix(key, prefix) {
			out = append(out, key)
		}
	}
	sort.Strings(out)
	return out
}

// unknownCommandError names the closest command rather than only saying no.
// A typo is the commonest way to reach here and the fix is usually one letter.
func unknownCommandError(name string) error {
	if suggestion, ok := closestCommand(name); ok {
		return fmt.Errorf("unknown command %q; did you mean %q?", name, suggestion)
	}
	return fmt.Errorf("unknown command %q; run \"worldledger help\" for the list", name)
}

// closestCommand returns the nearest top-level command within a distance that
// still means it was a typo. Suggesting something three edits away is worse
// than suggesting nothing, because it sends a reader off to read about the
// wrong thing.
func closestCommand(name string) (string, bool) {
	best, bestDistance := "", 0
	limit := len(name)/2 + 1
	if limit > 3 {
		limit = 3
	}
	for candidate := range commandUsage {
		if strings.Contains(candidate, " ") {
			continue
		}
		distance := editDistance(name, candidate)
		if distance > limit {
			continue
		}
		if best == "" || distance < bestDistance || (distance == bestDistance && candidate < best) {
			best, bestDistance = candidate, distance
		}
	}
	return best, best != ""
}

func editDistance(a, b string) int {
	previous := make([]int, len(b)+1)
	current := make([]int, len(b)+1)
	for j := range previous {
		previous[j] = j
	}
	for i := 1; i <= len(a); i++ {
		current[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			current[j] = min(previous[j]+1, min(current[j-1]+1, previous[j-1]+cost))
		}
		previous, current = current, previous
	}
	return previous[len(b)]
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
