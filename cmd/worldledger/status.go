package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/worldledger/worldledger-mc/internal/archive"
	"github.com/worldledger/worldledger-mc/internal/attest"
	"github.com/worldledger/worldledger-mc/internal/policy"
	"github.com/worldledger/worldledger-mc/internal/redact"
)

// cmdStatus answers the question every other command makes someone assemble
// from three or four separate answers: what is in here, and what has to happen
// next.
//
// Nothing here is new information. It exists because a person who has just
// installed the mod and played for an evening has no way to find out whether
// any of it worked, and reading a coverage report to discover that an archive
// is empty is a poor way to learn it.
func cmdStatus(args []string) error {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	archivePath := fs.String("archive", "", "archive directory")
	spoolPath := fs.String("spool", "", "capture spool to report on as well")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *archivePath == "" {
		return errors.New("usage: worldledger status --archive DIR [--spool DIR]")
	}

	a, err := archive.Open(*archivePath)
	if err != nil {
		return err
	}
	manifest, err := a.Manifest()
	if err != nil {
		return err
	}

	fmt.Printf("archive      %s\n", *archivePath)
	fmt.Printf("observations %d\n", manifest.Observations)
	fmt.Printf("objects      %d (%s)\n", manifest.Objects, humanBytes(manifest.ObjectBytes))
	fmt.Printf("root         %s\n", manifest.Root)

	spoolAlreadyAdvised := false
	if *spoolPath != "" {
		advised, err := reportSpoolStatus(*spoolPath)
		if err != nil {
			return err
		}
		spoolAlreadyAdvised = advised
	}

	if len(manifest.Servers) == 0 {
		// The spool report has just said what to run. Saying it again in
		// different words reads as two separate problems.
		if !spoolAlreadyAdvised {
			fmt.Println("\nThis archive holds nothing yet.")
			fmt.Println("Import a capture spool into it:")
			fmt.Println("  worldledger ingest-spool --archive <archive> <minecraft-config>/worldledger/spool")
		}
		return nil
	}

	policies := policy.NewStore(a.Root)
	redactions, err := redact.NewStore(a.Root).List()
	if err != nil {
		return err
	}
	identities, err := attest.NewIdentityStore(a.Root).List()
	if err != nil {
		return err
	}

	fmt.Println()
	var undeclared []string
	for _, server := range manifest.Servers {
		chunks, observations := 0, 0
		for _, dimension := range server.Dimensions {
			chunks += dimension.Chunks
			observations += dimension.Observations
		}
		fmt.Printf("%s\n", server.Server)
		fmt.Printf("  %d chunk(s), %d observation(s) across %d dimension(s)\n",
			chunks, observations, len(server.Dimensions))

		declared, found, err := policies.Lookup(server.Server)
		if err != nil {
			return err
		}
		if !found {
			fmt.Println("  publication  not declared")
			undeclared = append(undeclared, server.Server)
		} else {
			allowed, why := declared.DistributionAllowed(time.Now().UTC())
			verdict := "not allowed"
			if allowed {
				verdict = "allowed"
			}
			fmt.Printf("  publication  %s, distribution %s (%s)\n", declared.Disposition, verdict, why)
		}
	}

	if len(redactions) > 0 {
		fmt.Printf("\n%d redaction(s) declared; matching observations are withheld from coverage, export and convert\n",
			len(redactions))
	}
	if len(identities) > 0 {
		fmt.Printf("%d registered contributor key(s)\n", len(identities))
	}

	if len(undeclared) > 0 {
		fmt.Println("\nExport and convert refuse a server nobody has decided about. Declare one:")
		fmt.Printf("  worldledger policy set --archive %s --server %s \\\n", *archivePath, undeclared[0])
		fmt.Println("      --disposition private --declared-by your-name")
	}
	return nil
}

// reportSpoolStatus returns whether it already told the reader what to run,
// so the caller does not say the same thing again in different words.
func reportSpoolStatus(spool string) (bool, error) {
	ready, leftovers, err := readSpool(spool)
	if err != nil {
		return false, err
	}
	bytes, err := directoryBytes(spool)
	if err != nil {
		return false, err
	}

	fmt.Printf("\nspool        %s\n", spool)
	fmt.Printf("waiting      %d bundle(s) (%s)\n", len(ready), humanBytes(bytes))
	if leftovers["being written"] > 0 {
		fmt.Printf("in progress  %d (a client is probably running)\n", leftovers["being written"])
	}
	if leftovers["quarantined"] > 0 {
		fmt.Printf("quarantined  %d (the adapter rejected these; worth a look)\n", leftovers["quarantined"])
	}
	if len(ready) == 0 {
		return false, nil
	}
	fmt.Println("\nTake them in with:")
	fmt.Printf("  worldledger ingest-spool --archive <archive> %s\n", spool)
	return true, nil
}

func directoryBytes(root string) (int64, error) {
	var total int64
	err := filepath.Walk(root, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			total += info.Size()
		}
		return nil
	})
	return total, err
}
