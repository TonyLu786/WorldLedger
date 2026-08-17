package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/worldledger/worldledger-mc/internal/archive"
	"github.com/worldledger/worldledger-mc/internal/bundle"
	// Aliased because this file's own variable for a spool path is called
	// spool, and a package of the same name would be shadowed inside every
	// function that has one.
	spooldir "github.com/worldledger/worldledger-mc/internal/spool"
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
	if *archivePath == "" || fs.NArg() > 1 {
		return usageError("ingest-spool")
	}
	spool := fs.Arg(0)
	if spool == "" {
		found, err := findSpool()
		if err != nil {
			return err
		}
		spool = found
		fmt.Printf("spool         %s  (found automatically)\n", spool)
	}

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
	if imported > 0 {
		printNextStepAfterImport(a, *archivePath)
	}
	return nil
}

// printNextStepAfterImport names the one command that has to come next.
//
// An import leaves a person holding an archive and no world, and the step
// between them is a publication decision they were never told to expect.
// status already answers this when asked; the point here is not having to know
// to ask.
func printNextStepAfterImport(a archive.Archive, archivePath string) {
	servers, err := a.Servers()
	if err != nil || len(servers) == 0 {
		return
	}
	undeclared := undeclaredServers(a, servers)
	fmt.Println()
	if len(undeclared) > 0 {
		fmt.Println("Next: export refuses a server nobody has decided about. Declare one:")
		fmt.Printf("  worldledger policy set --archive %s --server %s \\\n"+
			"      --disposition private --declared-by your-name\n", archivePath, undeclared[0])
		return
	}
	fmt.Println("Next: write a world you can open in single player:")
	fmt.Printf("  worldledger export --archive %s --server %s --into ./world\n", archivePath, servers[0])
}

// readSpool lists the bundles ready to import, in the order the adapter wrote
// them, and separately reports what else is in there.
//
// A .tmp- entry is a bundle being written and a quarantine- entry is one the
// adapter rejected. Neither is importable, and neither should be silently
// ignored: the first means a client is still running, and the second is a
// failure someone should look at.
// Reading the directory belongs to internal/spool, because the desktop
// application asks the same question of the same three prefixes. What stays
// here is the shape the rest of this file already prints from, and the message
// for a directory that is not there, which names the path this command was
// given.
func readSpool(spool string) (ready []string, leftovers map[string]int, err error) {
	contents, err := spooldir.Read(spool)
	if os.IsNotExist(err) {
		return nil, nil, fmt.Errorf("no spool directory at %s", spool)
	}
	if err != nil {
		return nil, nil, err
	}
	return contents.Ready, map[string]int{
		"being written": contents.InProgress,
		"quarantined":   contents.Quarantined,
	}, nil
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
