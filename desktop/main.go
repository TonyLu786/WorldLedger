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
	"net/http"
	"os"

	"github.com/worldledger/worldledger-mc/desktop/internal/app"
	"github.com/worldledger/worldledger-mc/desktop/internal/health"
	"github.com/worldledger/worldledger-mc/desktop/internal/shell"
	"github.com/worldledger/worldledger-mc/desktop/ui"
	"github.com/worldledger/worldledger-mc/internal/mcpath"
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

	server, err := app.New()
	if err != nil {
		return err
	}
	defer server.Close()

	if err := ui.Mount(server); err != nil {
		return err
	}
	mountAPI(server)

	errs := make(chan error, 1)
	go func() { errs <- server.Serve() }()

	switch {
	case *printURL:
		fmt.Println(server.URL())
	default:
		// A window that cannot be opened is not a reason to fail. Whatever
		// happened is worth saying once, and then the browser gets the same
		// application.
		if note := shell.Open(server.URL(), *useBrowser); note != "" {
			fmt.Fprintln(os.Stderr, note)
		}
	}

	return <-errs
}

// mountAPI registers what the page calls. Handlers stay thin: everything they
// do belongs to a package that can be tested without a socket.
func mountAPI(server *app.Server) {
	server.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		install, candidates, found := mcpath.FindInstall()
		if !found {
			looked := make([]string, 0, len(candidates))
			for _, candidate := range candidates {
				looked = append(looked, candidate.Root)
			}
			app.WriteJSON(w, http.StatusOK, health.NotFound(looked))
			return
		}
		app.WriteJSON(w, http.StatusOK, health.Inspect(install))
	})
}
