package main

import (
	"flag"
	"fmt"
	"io"

	"github.com/worldledger/worldledger-mc/internal/archive"
	"github.com/worldledger/worldledger-mc/internal/transfer"
)

func cmdSend(args []string) error {
	fs := flag.NewFlagSet("send", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	archivePath := fs.String("archive", "", "archive directory")
	peerFingerprint := fs.String("to", "", "the receiving mirror's fingerprint file")
	peerManifest := fs.String("their-manifest", "", "the receiving mirror's manifest, so records they already hold are not resent")
	out := fs.String("out", "", "directory to write the transfer bundle to")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *archivePath == "" || *peerFingerprint == "" || *out == "" {
		return usageError("send")
	}

	a, err := archive.Open(*archivePath)
	if err != nil {
		return err
	}
	peer, err := readFingerprint(*peerFingerprint)
	if err != nil {
		return err
	}

	var manifest *archive.Manifest
	if *peerManifest != "" {
		loaded, err := archive.LoadManifest(*peerManifest)
		if err != nil {
			return err
		}
		manifest = &loaded
	}

	sent, err := transfer.Send(a, peer, manifest, *out)
	if err != nil {
		return err
	}
	if sent.Observations == 0 && sent.Objects == 0 {
		fmt.Println("that mirror already holds everything this archive has; no bundle was written")
		return nil
	}
	fmt.Printf("wrote %s\n", *out)
	fmt.Printf("observations %d\n", sent.Observations)
	fmt.Printf("objects      %d (%s)\n", sent.Objects, humanBytes(sent.Bytes))
	// The object count is usually far below the observation count and reads as
	// an error until someone explains it. It is the payoff of content
	// addressing: the peer's fingerprint already listed the component bytes it
	// held, so only the records and the genuinely new bytes travel.
	if sent.Objects < sent.Observations {
		fmt.Printf("\n%d observation(s) needed only %d object(s), because the fingerprint said\n",
			sent.Observations, sent.Objects)
		fmt.Println("which component bytes that archive already holds.")
	}
	fmt.Println("\nThe bundle is a plain directory. Copy it however you like; the receiver")
	fmt.Println("verifies every byte against the digest the bundle declares.")
	fmt.Println("\nNext: hand them the directory. They run:")
	fmt.Printf("  worldledger receive --archive THEIR-ARCHIVE %s\n", *out)
	return nil
}

func cmdReceive(args []string) error {
	fs := flag.NewFlagSet("receive", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	archivePath := fs.String("archive", "", "archive directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *archivePath == "" || fs.NArg() != 1 {
		return usageError("receive")
	}
	a, err := archive.Open(*archivePath)
	if err != nil {
		return err
	}
	received, err := transfer.Receive(a, fs.Arg(0))
	if err != nil {
		return err
	}
	fmt.Printf("merged   %d observation(s)\n", received.Observations)
	fmt.Printf("objects  %d\n", received.Objects)
	if received.AlreadyHeld > 0 {
		fmt.Printf("already held %d observation(s); a repeated import changes nothing\n", received.AlreadyHeld)
	}
	if received.Observations > 0 {
		// One transfer moves observations one way, so the two archives are now
		// deliberately unequal. Comparing them here lists every chunk the sender
		// has not got yet, which reads as a fault and is the expected midpoint.
		fmt.Println("\nThis archive now holds their observations. Theirs does not yet hold this")
		fmt.Println("one's: a transfer moves in one direction. To close the loop, send them:")
		fmt.Printf("  worldledger fingerprint --archive %s --out my-fingerprint.txt\n", *archivePath)
		fmt.Printf("  worldledger manifest    --archive %s --out my-manifest.json\n", *archivePath)
		fmt.Println("and ask them to send back what those two say is missing.")
	}
	return nil
}
