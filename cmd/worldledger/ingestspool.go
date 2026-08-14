package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/worldledger/worldledger-mc/internal/archive"
	"github.com/worldledger/worldledger-mc/internal/bundle"
)

func cmdIngestSpool(args []string) error {
	fs := flag.NewFlagSet("ingest-spool", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	archivePath := fs.String("archive", "", "archive directory")
	keep := fs.Bool("keep", false, "leave imported bundles in the spool instead of removing them")
	dryRun := fs.Bool("dry-run", false, "report what would be imported without changing anything")
	limits := bundle.DefaultLimits()
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *archivePath == "" || fs.NArg() != 1 {
		return errors.New("usage: worldledger ingest-spool --archive DIR [--keep] [--dry-run] <spool-dir>")
	}
	spool := fs.Arg(0)

	ready, leftovers, err := readSpool(spool)
	if err != nil {
		return err
	}
	if len(ready) == 0 {
		fmt.Printf("no ready bundle in %s\n", spool)
		reportSpoolLeftovers(leftovers)
		return nil
	}

	if *dryRun {
		fmt.Printf("%d bundle(s) would be imported from %s\n", len(ready), spool)
		if !*keep {
			fmt.Println("and removed from the spool afterwards; pass --keep to leave them")
		}
		reportSpoolLeftovers(leftovers)
		return nil
	}

	a, err := archive.Open(*archivePath)
	if err != nil {
		return err
	}

	imported := 0
	var failures []string
	for _, path := range ready {
		// Removal happens only after the import returned, which is after the
		// archive has fsynced the observation. A bundle that failed stays where
		// it is: it is still the only copy of what someone saw.
		_, err := bundle.Import(a, path, bundle.Options{Limits: limits, DeleteOnSuccess: !*keep})
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", filepath.Base(path), err))
			continue
		}
		imported++
	}

	fmt.Printf("imported %d of %d bundle(s)\n", imported, len(ready))
	if !*keep && imported > 0 {
		fmt.Printf("removed %d imported bundle(s) from the spool\n", imported)
	}
	reportSpoolLeftovers(leftovers)

	if len(failures) > 0 {
		fmt.Printf("\n%d bundle(s) were left in place because they did not import:\n", len(failures))
		for index, failure := range failures {
			if index == 20 {
				fmt.Printf("  ... %d more\n", len(failures)-20)
				break
			}
			fmt.Printf("  %s\n", failure)
		}
		return fmt.Errorf("%d bundle(s) did not import", len(failures))
	}
	return nil
}

// readSpool lists the bundles ready to import, in the order the adapter wrote
// them, and separately reports what else is in there.
//
// A .tmp- entry is a bundle being written and a quarantine- entry is one the
// adapter rejected. Neither is importable, and neither should be silently
// ignored: the first means a client is still running, and the second is a
// failure someone should look at.
func readSpool(spool string) (ready []string, leftovers map[string]int, err error) {
	entries, err := os.ReadDir(spool)
	if os.IsNotExist(err) {
		return nil, nil, fmt.Errorf("no spool directory at %s", spool)
	}
	if err != nil {
		return nil, nil, err
	}

	leftovers = map[string]int{}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		switch {
		case strings.HasPrefix(name, "ready-"):
			ready = append(ready, filepath.Join(spool, name))
		case strings.HasPrefix(name, ".tmp-"):
			leftovers["being written"]++
		case strings.HasPrefix(name, "quarantine-"):
			leftovers["quarantined"]++
		}
	}
	sort.Strings(ready)
	return ready, leftovers, nil
}

func reportSpoolLeftovers(leftovers map[string]int) {
	if leftovers["being written"] > 0 {
		fmt.Printf("%d entr(y/ies) are still being written; a client is probably running\n",
			leftovers["being written"])
	}
	if leftovers["quarantined"] > 0 {
		fmt.Printf("%d quarantined entr(y/ies) were left alone; the adapter rejected them and they are worth a look\n",
			leftovers["quarantined"])
	}
}
