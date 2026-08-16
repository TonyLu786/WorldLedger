package main

import (
	"fmt"

	"github.com/worldledger/worldledger-mc/internal/archive"
)

// Merging two archives is the thing this project claims a world downloader
// structurally cannot do, and it is the one journey nothing guided.
//
// It takes four commands and two hand-offs in each direction, and the state
// between the two directions looks like a failure: one archive is ahead, and
// comparing them lists every chunk the other has not got yet. That is the
// expected midpoint of a working exchange, and until now it was reported as a
// list of differences with nothing saying so.
//
// Every step below therefore ends by naming the next one, with the real paths
// filled in, in the same way init and ingest-spool do for a single archive.

// exchangeDirection summarises which way observations still need to move.
type exchangeDirection struct {
	localAhead  int
	remoteAhead int
	both        int
}

func classifyDifferences(differences []archive.Difference) exchangeDirection {
	var direction exchangeDirection
	for _, difference := range differences {
		switch {
		case difference.OnlyLocal():
			direction.localAhead++
		case difference.OnlyRemote():
			direction.remoteAhead++
		default:
			// A chunk both hold with different observation counts. Each side has
			// something the other lacks, so it takes both directions.
			direction.both++
		}
	}
	return direction
}

func (d exchangeDirection) settled() bool {
	return d.localAhead == 0 && d.remoteAhead == 0 && d.both == 0
}

// explain says what the difference means and what would settle it.
//
// The wording avoids "missing", which reads as damage. Neither archive is
// wrong: they saw different things, which is the situation this is for.
func (d exchangeDirection) explain(archivePath string) []string {
	if d.settled() {
		return nil
	}
	var lines []string
	sendNeeded := d.localAhead > 0 || d.both > 0
	receiveNeeded := d.remoteAhead > 0 || d.both > 0

	switch {
	case d.both > 0:
		lines = append(lines,
			"Some chunks differ on both sides. A manifest carries digests rather than",
			"observation identities, so it can say a chunk differs without saying which",
			"archive holds what; one transfer in each direction settles it either way.",
			"Each send works out exactly what to include from the other's fingerprint,",
			"so a direction that turns out to be unnecessary writes nothing.")
	case sendNeeded && receiveNeeded:
		lines = append(lines,
			"Each archive holds observations the other does not, so it takes one transfer",
			"in each direction to settle.")
	case sendNeeded:
		lines = append(lines,
			"This archive holds observations the other does not. One transfer settles it.")
	default:
		lines = append(lines,
			"The other archive holds observations this one does not. One transfer settles it,",
			"sent by them.")
	}

	if sendNeeded {
		lines = append(lines,
			"",
			"To send what they are missing, with their fingerprint file:",
			"  worldledger send --archive "+archivePath+" --to their-fingerprint.txt \\",
			"      --their-manifest their-manifest.json --out ./outbound",
			"  (then hand them ./outbound, and they run: worldledger receive --archive THEIRS ./outbound)")
	}
	if receiveNeeded {
		lines = append(lines,
			"",
			"To receive what this archive is missing, give them these two files:",
			"  worldledger fingerprint --archive "+archivePath+" --out my-fingerprint.txt",
			"  worldledger manifest    --archive "+archivePath+" --out my-manifest.json")
	}
	return lines
}

func printLines(lines []string) {
	for _, line := range lines {
		fmt.Println(line)
	}
}

// printHandOff is what a command that wrote a file for a peer says afterwards.
//
// A path and a digest tell a reader that something happened and nothing about
// what to do with it. Both files go to the same person for the same reason, and
// saying which command consumes them is what turns two artefacts into a step.
func printHandOff(kind, path string) {
	fmt.Println()
	switch kind {
	case "fingerprint":
		fmt.Println("Give this to whoever is sending you observations. It says what this")
		fmt.Println("archive already holds, so they send only what it does not.")
		fmt.Printf("  they run: worldledger send --archive THEIRS --to %s --out ./outbound\n", path)
		fmt.Println()
		fmt.Println("A manifest alongside it lets them skip whole chunks rather than only")
		fmt.Println("objects:")
		fmt.Println("  worldledger manifest --archive . --out my-manifest.json")
	case "manifest":
		fmt.Println("This goes to the same person as the fingerprint, and is what lets them")
		fmt.Println("skip observations this archive already has:")
		fmt.Println("  they run: worldledger send --archive THEIRS --to my-fingerprint.txt \\")
		fmt.Printf("      --their-manifest %s --out ./outbound\n", path)
	}
}
