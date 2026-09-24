package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/worldledger/worldledger-mc/internal/atomicfile"
	"github.com/worldledger/worldledger-mc/internal/diagnose"
)

// Somebody whose capture is not working has had no way to say what is wrong,
// and nobody has had a way to ask. "Send me your archive" is not a request this
// project is allowed to make: an archive is where a person went and when.
//
// This prints what it would hand over, to the screen, before it writes
// anything. That is not a courtesy. It is the only way somebody can decide
// whether to send it, and a support file whose contents are taken on trust is
// one nobody should send.
func cmdDiagnose(args []string) error {
	fs := flag.NewFlagSet("diagnose", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	archivePath := fs.String("archive", "", "archive directory, if the trouble involves one")
	spoolPath := fs.String("spool", "", "capture spool directory, if the trouble involves one")
	out := fs.String("out", "", "write it to a file as well as showing it")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected argument %q\n\n%s", fs.Arg(0), commandUsage["diagnose"])
	}
	if *archivePath == "" && *spoolPath == "" {
		resolved, err := findSpool()
		if err != nil {
			return fmt.Errorf("%w\n\n%s", err, commandUsage["diagnose"])
		}
		*spoolPath = resolved
	}

	report := diagnose.Take(version, *archivePath, *spoolPath)
	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')

	fmt.Println("This is everything it would hand over. Read it before you send it.")
	fmt.Println()
	os.Stdout.Write(encoded)
	fmt.Println()
	fmt.Println("What is deliberately not in it: anything you observed, the names of")
	fmt.Println("servers, contributors or worlds, and anybody's keys. Counts and kinds")
	fmt.Println("are enough to tell one failure from another, and the rest is yours.")

	if *out == "" {
		fmt.Println()
		fmt.Println("Nothing was written. Pass --out FILE to keep a copy.")
		return nil
	}
	if err := atomicfile.Write(*out, encoded, 0o600); err != nil {
		return err
	}
	fmt.Println()
	fmt.Printf("wrote %s\n", *out)
	return nil
}
