package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/worldledger/worldledger-mc/internal/archive"
	"github.com/worldledger/worldledger-mc/internal/epoch"
)

// An exported world carries no record of where it came from. Two people can
// export the same server at the same instant from archives holding different
// observations, get different worlds, and have no way to find out short of
// comparing region files.
//
// This writes the document that answers it: what the archive selected at every
// position, with a root digest over the positions and the states, so the
// question is one value.
func cmdEpoch(args []string) error {
	fs := flag.NewFlagSet("epoch", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	archivePath := fs.String("archive", "", "archive directory")
	server := fs.String("server", "", "server id")
	dimension := fs.String("dimension", defaultDimension, "dimension id")
	moment := fs.String("at", "", "RFC3339 instant; defaults to now")
	out := fs.String("out", "", "write the manifest to this file")
	compareWith := fs.String("compare", "", "compare against a manifest written by --out")
	asJSON := fs.Bool("json", false, "write the manifest to stdout as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *archivePath == "" || *server == "" || *dimension == "" {
		return usageError("epoch")
	}

	a, err := archive.Open(*archivePath)
	if err != nil {
		return err
	}
	snapshot, err := snapshotAt(a, *server, *dimension, *moment)
	if err != nil {
		return err
	}
	if snapshot.Summary.Chunks == 0 {
		return emptySelectionError(a, *server, *dimension, snapshot.At)
	}
	manifest := epoch.BuildManifest(snapshot)

	if *compareWith != "" {
		return reportEpochComparison(manifest, *compareWith)
	}
	if *asJSON {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(manifest)
	}

	printEpochManifest(manifest)
	if *out != "" {
		if err := manifest.Save(*out); err != nil {
			return err
		}
		fmt.Printf("\nwrote %s\n", *out)
		fmt.Println("\nGive this to anyone exporting the same moment. They run:")
		fmt.Printf("  worldledger epoch --archive THEIRS --server %s --dimension %s \\\n",
			manifest.Server, manifest.Dimension)
		fmt.Printf("      --at %s --compare %s\n", manifest.At.Format(time.RFC3339Nano), *out)
	} else {
		fmt.Println("\nPass --out to write this down, so somebody else can compare against it.")
	}
	return nil
}

func printEpochManifest(manifest epoch.Manifest) {
	fmt.Printf("server      %s\n", manifest.Server)
	fmt.Printf("dimension   %s\n", manifest.Dimension)
	fmt.Printf("at          %s\n", manifest.At.Format(time.RFC3339Nano))
	fmt.Printf("policy      %s\n", manifest.Policy)
	fmt.Printf("root        %s\n\n", manifest.Root)

	summary := manifest.Summary
	fmt.Printf("chunks        %d\n", summary.Chunks)
	fmt.Printf("corroborated  %d\n", summary.Corroborated)
	fmt.Printf("single-source %d\n", summary.SingleSource)
	fmt.Printf("superseded    %d\n", summary.Superseded)
	fmt.Printf("conflict      %d\n", summary.Conflict)
	fmt.Printf("unknown       %d  (left unwritten by export, not filled with air)\n", summary.Unknown)

	// The root is over positions and states only. Saying so here is what stops
	// somebody reading a matching root as "our archives are the same".
	fmt.Println("\nThe root covers the chunk positions and the state chosen at each one.")
	fmt.Println("Two archives agreeing on it would export the same world, whoever observed")
	fmt.Println("it and however well attested it is.")
}

func reportEpochComparison(local epoch.Manifest, path string) error {
	remote, err := epoch.LoadManifest(path)
	if err != nil {
		return err
	}
	comparison, err := epoch.CompareManifests(local, remote)
	if err != nil {
		return err
	}

	fmt.Printf("local  root %s\n", local.Root)
	fmt.Printf("remote root %s\n\n", remote.Root)

	if comparison.SameWorld {
		fmt.Println("both archives would export the same world at this moment")
	} else {
		fmt.Printf("these would export different worlds: %d chunk(s) differ",
			len(comparison.Mismatched))
		if extra := len(comparison.OnlyLocal) + len(comparison.OnlyRemote); extra > 0 {
			fmt.Printf(", and %d are known to only one of them", extra)
		}
		fmt.Println()
	}

	printEpochChunkDifferences("chunks whose state differs", comparison.Mismatched)
	printEpochOnlySide("only this archive has a reading for", comparison.OnlyLocal)
	printEpochOnlySide("only the other archive has a reading for", comparison.OnlyRemote)

	if len(comparison.Confidence) > 0 {
		// Separated on purpose. These export identically; one archive simply has
		// more evidence, and reporting it beside a state difference would make
		// "the worlds differ" the reader's conclusion when it is not.
		fmt.Printf("\n%d chunk(s) hold the same state with different confidence:\n",
			len(comparison.Confidence))
		for index, difference := range comparison.Confidence {
			if index == 20 {
				fmt.Printf("  ... %d more\n", len(comparison.Confidence)-20)
				break
			}
			fmt.Printf("  (%d,%d)  here %s, there %s\n",
				difference.X, difference.Z, difference.Local.Status, difference.Remote.Status)
		}
		fmt.Println("These do not change the exported world.")
	}

	if !comparison.SameWorld {
		return errors.New("the two archives do not describe the same world at this moment")
	}
	return nil
}

func printEpochChunkDifferences(title string, differences []epoch.ChunkDifference) {
	if len(differences) == 0 {
		return
	}
	fmt.Printf("\n%s:\n", title)
	for index, difference := range differences {
		if index == 20 {
			fmt.Printf("  ... %d more\n", len(differences)-20)
			break
		}
		fmt.Printf("  (%d,%d)  %s -> %s\n", difference.X, difference.Z,
			shortDigest(orNone(difference.Local.StateDigest)),
			shortDigest(orNone(difference.Remote.StateDigest)))
	}
}

func printEpochOnlySide(title string, chunks []epoch.Chunk) {
	if len(chunks) == 0 {
		return
	}
	fmt.Printf("\n%d chunk(s) %s:\n", len(chunks), title)
	for index, chunk := range chunks {
		if index == 20 {
			fmt.Printf("  ... %d more\n", len(chunks)-20)
			break
		}
		fmt.Printf("  (%d,%d)  %s\n", chunk.X, chunk.Z, chunk.Status)
	}
}

// orNone keeps an unobserved chunk from printing as an empty column, which
// reads as a missing value rather than as the answer it is.
func orNone(digest string) string {
	if digest == "" {
		return "unobserved"
	}
	return digest
}
