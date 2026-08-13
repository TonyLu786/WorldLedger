package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/worldledger/worldledger-mc/internal/archive"
)

func cmdFingerprint(args []string) error {
	fs := flag.NewFlagSet("fingerprint", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	archivePath := fs.String("archive", "", "archive directory")
	file := fs.String("file", "", "read a fingerprint from a file instead of an archive")
	server := fs.String("server", "", "limit to one server")
	out := fs.String("out", "", "write the fingerprint to a file instead of standard output")
	compareWith := fs.String("compare", "", "compare against a fingerprint file from another machine")
	if err := fs.Parse(args); err != nil {
		return err
	}
	switch {
	case *archivePath == "" && *file == "":
		return errors.New("usage: worldledger fingerprint (--archive DIR | --file FILE) [--server ID] [--out FILE] [--compare FILE]")
	case *archivePath != "" && *file != "":
		return errors.New("give either --archive or --file, not both")
	}

	// Two machines each produce a file, and neither keeps the other's archive.
	// Comparing the files directly is the normal case rather than the exception.
	var fingerprint archive.Fingerprint
	if *file != "" {
		loaded, err := readFingerprint(*file)
		if err != nil {
			return err
		}
		fingerprint = loaded
	} else {
		a, err := archive.Open(*archivePath)
		if err != nil {
			return err
		}
		loaded, err := a.Fingerprint(*server)
		if err != nil {
			return err
		}
		fingerprint = loaded
	}

	if *compareWith != "" {
		return reportFingerprintComparison(fingerprint, *compareWith)
	}
	if *out == "" {
		return fingerprint.WriteText(os.Stdout)
	}

	destination, err := os.Create(*out)
	if err != nil {
		return err
	}
	if err := fingerprint.WriteText(destination); err != nil {
		destination.Close()
		return err
	}
	if err := destination.Close(); err != nil {
		return err
	}
	fmt.Printf("wrote %s\n", *out)
	fmt.Printf("root       %s\n", fingerprint.Root)
	fmt.Printf("states     %d\n", len(fingerprint.States))
	fmt.Printf("components %d\n", len(fingerprint.Components))
	return nil
}

// reportFingerprintComparison answers whether two machines canonicalized the
// same observed state into the same bytes. It deliberately says nothing about
// who observed or when, because those differ between any two captures and are
// not what this comparison is for.
func readFingerprint(path string) (archive.Fingerprint, error) {
	handle, err := os.Open(path)
	if err != nil {
		return archive.Fingerprint{}, err
	}
	defer handle.Close()
	return archive.ParseFingerprint(handle)
}

func reportFingerprintComparison(local archive.Fingerprint, path string) error {
	remote, err := readFingerprint(path)
	if err != nil {
		return err
	}

	fmt.Printf("local  root %s  (%d states, %d components)\n", local.Root, len(local.States), len(local.Components))
	fmt.Printf("remote root %s  (%d states, %d components)\n\n", remote.Root, len(remote.States), len(remote.Components))

	comparison := archive.CompareFingerprints(local, remote)
	content := comparison.ContentDifferences()

	counts := map[string]int{}
	for _, difference := range comparison.Differences {
		counts[difference.Kind]++
	}

	fmt.Printf("%d chunk(s) observed by both\n", comparison.Shared)
	if counts[archive.FingerprintCoverageDifference] > 0 {
		fmt.Printf("%d chunk(s) seen by only one capture; coverage follows session length, not encoding\n",
			counts[archive.FingerprintCoverageDifference])
	}
	if counts[archive.FingerprintStatesDifference] > 0 {
		fmt.Printf("%d shared chunk(s) where one capture saw a change the other missed; the states they share are identical\n",
			counts[archive.FingerprintStatesDifference])
	}

	if comparison.Shared == 0 {
		return errors.New("the two captures share no chunk, so this comparison shows nothing")
	}
	if len(content) == 0 {
		fmt.Println("\nevery state observed by both captures canonicalized to identical bytes")
		return nil
	}

	fmt.Printf("\n%d chunk(s) hold states the other cannot account for:\n", len(content))
	for index, difference := range content {
		if index == 50 {
			fmt.Printf("  ... %d more\n", len(content)-50)
			break
		}
		fmt.Printf("  %s %s (%d,%d)  %s\n", difference.Server, difference.Dimension, difference.X, difference.Z, difference.Detail)
	}
	return fmt.Errorf("%d chunk(s) disagree", len(content))
}
