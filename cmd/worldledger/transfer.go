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
	fmt.Println("\nThe bundle is a plain directory. Copy it however you like; the receiver")
	fmt.Println("verifies every byte against the digest the bundle declares.")
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
	return nil
}
