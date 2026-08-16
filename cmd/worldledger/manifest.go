package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/worldledger/worldledger-mc/internal/archive"
)

func cmdManifest(args []string) error {
	fs := flag.NewFlagSet("manifest", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	archivePath := fs.String("archive", "", "archive directory")
	out := fs.String("out", "", "write the manifest to a file instead of standard output")
	compareWith := fs.String("compare", "", "compare against another archive's manifest file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *archivePath == "" {
		return usageError("manifest")
	}

	a, err := archive.Open(*archivePath)
	if err != nil {
		return err
	}
	manifest, err := a.Manifest()
	if err != nil {
		return err
	}

	if *compareWith != "" {
		return reportComparison(manifest, *compareWith, *archivePath)
	}
	if *out == "" {
		// The manifest is the output here, so it goes to stdout alone and stays
		// pipeable into another tool.
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(manifest)
	}

	if err := manifest.Save(*out); err != nil {
		return err
	}
	fmt.Printf("wrote %s\n", *out)
	fmt.Printf("root         %s\n", manifest.Root)
	fmt.Printf("observations %d across %d server(s)\n", manifest.Observations, len(manifest.Servers))
	fmt.Printf("objects      %d (%s)\n", manifest.Objects, humanBytes(manifest.ObjectBytes))
	fmt.Printf("generated    %s\n", manifest.GeneratedAt.Format(time.RFC3339))
	printHandOff("manifest", *out)
	return nil
}

// reportComparison localises where two archives differ without either side
// transferring its contents.
func reportComparison(local archive.Manifest, path, archivePath string) error {
	remote, err := archive.LoadManifest(path)
	if err != nil {
		return err
	}

	fmt.Printf("local  root %s  (%d observations)\n", local.Root, local.Observations)
	fmt.Printf("remote root %s  (%d observations)\n\n", remote.Root, remote.Observations)

	differences := archive.Compare(local, remote)
	if len(differences) == 0 {
		fmt.Println("the two archives hold the same observations")
		return nil
	}

	fmt.Printf("%d difference(s):\n", len(differences))
	for index, difference := range differences {
		if index == 50 {
			fmt.Printf("  ... %d more\n", len(differences)-50)
			break
		}
		location := difference.Server
		if difference.Dimension != "" {
			location += " " + difference.Dimension
		}
		if difference.Chunk != nil {
			location += fmt.Sprintf(" (%d,%d)", difference.Chunk.X, difference.Chunk.Z)
		}
		fmt.Printf("  %-52s %s\n", location, difference.Detail)
	}

	// A list of chunks reads as damage. It is not: two people who explored the
	// same server are supposed to differ, and the list is the work a transfer
	// would do. Saying which direction settles it is the difference between a
	// report and an instruction.
	fmt.Println()
	printLines(classifyDifferences(differences).explain(archivePath))
	return nil
}

func humanBytes(value int64) string {
	const unit = 1024
	if value < unit {
		return fmt.Sprintf("%d B", value)
	}
	div, exp := int64(unit), 0
	for n := value / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(value)/float64(div), "KMGTPE"[exp])
}
