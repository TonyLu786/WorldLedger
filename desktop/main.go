// Command worldledger-desktop is WorldLedger for somebody who has never opened
// a terminal.
//
// The command line asks a person to know four things before they start: that a
// spool exists, where it is, what a publication policy is, and that an export
// needs a world that already exists. Each is defensible and together they mean
// the project is unusable by the people it is for. This asks none of them.
//
// It opens a window. If a window cannot be created -- no web view runtime, a
// locked-down machine -- it opens the browser instead and everything still
// works. A person whose machine is unusual should get a working program.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/worldledger/worldledger-mc/desktop/internal/api"
	"github.com/worldledger/worldledger-mc/desktop/internal/app"
	"github.com/worldledger/worldledger-mc/desktop/internal/shell"
	"github.com/worldledger/worldledger-mc/desktop/ui"
)

var version = "dev"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run() error {
	const usage = "usage: worldledger-desktop [--browser] [--print-url]"
	fs := flag.NewFlagSet("worldledger-desktop", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	useBrowser := fs.Bool("browser", false, "open in the default browser instead of a window")
	printURL := fs.Bool("print-url", false, "print the address and stay open without opening anything")
	showVersion := fs.Bool("version", false, "print the version and exit")
	// Where the mod jar comes from. A released build has this compiled in,
	// pointing at that release's own asset; a build from source has nothing,
	// and refuses to install rather than fetching something arbitrary. This
	// flag is how somebody working on the project supplies a jar they built
	// themselves, and it is deliberately something you have to type.
	modSource := fs.String("mod-source", "", "where to get the mod jar (a path or URL; development builds only)")
	if err := fs.Parse(os.Args[1:]); err != nil {
		if err == flag.ErrHelp {
			fmt.Println(usage)
			return nil
		}
		return fmt.Errorf("%w\n\n%s", err, usage)
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected argument %q\n\n%s", fs.Arg(0), usage)
	}
	if *showVersion {
		fmt.Println("worldledger-desktop", version)
		return nil
	}

	if *modSource != "" {
		api.ModSource = *modSource
	}

	server, err := app.New()
	if err != nil {
		return err
	}
	defer server.Close()

	if err := ui.Mount(server); err != nil {
		return err
	}
	// Generous on purpose. A machine that pauses for ten seconds under load has
	// not been abandoned, and quitting on somebody mid-import would be a worse
	// failure than lingering a little.
	watchdog := app.NewWatchdog(45 * time.Second)
	abandoned := watchdog.Mount(server)
	api.Mount(server, watchdog)

	errs := make(chan error, 1)
	go func() { errs <- server.Serve() }()

	if *printURL {
		// Nothing is watching, so this stays up until it is stopped. It exists
		// to be driven by something other than a person.
		fmt.Println(server.URL())
		return <-errs
	}

	// A window that cannot be opened is not a reason to fail. Whatever happened
	// is worth saying once, and then the browser gets the same application.
	mode, note := shell.Present(server.URL(), *useBrowser)
	if note != "" {
		if mode == shell.Unopened {
			// Nothing is on screen and the address is in here, so this has to
			// reach somebody even on a build that cannot print.
			shell.Tell(note)
		} else {
			fmt.Fprintln(os.Stderr, note)
		}
	}

	// The three ways this can have gone end differently, and getting it wrong
	// leaves somebody with a program they cannot see and cannot close. A window
	// closing is the program being closed, and Present has already returned by
	// the time we are here. A browser tab closing tells nobody anything, so the
	// page reports in while it is open and the quiet is what ends this.
	switch mode {
	case shell.InWindow:
		return nil
	case shell.InBrowser:
		// A browser was asked for. Two minutes is long enough for a cold start
		// on a slow machine and short enough that a browser which never arrives
		// does not leave this running unseen.
		watchdog.ExpectPage(2 * time.Minute)
	default:
		// Nobody was asked for anything: the address is on somebody's screen
		// and they have to go and open it. That takes as long as reading and
		// typing takes, so the wait is much longer. It is still a wait, though,
		// because the alternative is a process that outlives the person's
		// interest in it.
		watchdog.ExpectPage(15 * time.Minute)
	}
	select {
	case err := <-errs:
		return err
	case <-abandoned:
		if watchdog.NeverAppeared() {
			fmt.Fprintln(os.Stderr, "no page ever opened, so there was nothing to keep running for")
		}
		return nil
	}
}
