package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/worldledger/worldledger-mc/internal/archive"
	"github.com/worldledger/worldledger-mc/internal/corpus"
)

// cmdCorpus answers a question the fingerprint cannot: does the capture still
// contain the shapes the fixture world was built to contain?
//
// The fingerprint notices when a Minecraft upgrade changes what the game
// reports about the fixture. It only covers what the fixture actually has, and
// it says nothing about what the fixture was supposed to have. The game test
// places its world with server commands whose results nobody reads, so a block
// a release renames, or a command whose syntax moves, places nothing, fails
// nothing, and produces a smaller fingerprint that is just as green.
func cmdCorpus(args []string) error {
	fs := flag.NewFlagSet("corpus", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	archivePath := fs.String("archive", "", "archive directory")
	server := fs.String("server", "", "stable server id; every server in the archive when omitted")
	dimension := fs.String("dimension", defaultDimension, "dimension id")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *archivePath == "" {
		return usageError("corpus")
	}

	a, err := archive.Open(*archivePath)
	if err != nil {
		return err
	}
	report, err := corpus.Inspect(a, *server, *dimension)
	if err != nil {
		return err
	}

	fmt.Print(report.Describe())
	if report.Complete() {
		fmt.Println("\nthe capture contains every shape a Minecraft upgrade could break")
		return nil
	}
	// Written to stderr and returned as a failure, because this runs in a
	// pipeline and a thin corpus that only prints is a thin corpus that ships.
	fmt.Fprintf(os.Stderr,
		"\n%d of the %d shapes the fixture world is meant to contain were not captured.\n"+
			"Either the world was not built as intended -- a command that places nothing\n"+
			"fails nothing -- or a release has changed what it reports about them.\n",
		len(report.Missing), len(corpus.Required))
	return errCorpusIncomplete
}

var errCorpusIncomplete = fmt.Errorf("the capture does not contain every shape the fixture is meant to have")
