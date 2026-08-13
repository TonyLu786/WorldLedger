package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/worldledger/worldledger-mc/internal/archive"
	"github.com/worldledger/worldledger-mc/internal/bundle"
	"github.com/worldledger/worldledger-mc/internal/model"
	"github.com/worldledger/worldledger-mc/internal/verify"
)

const version = "0.1.0-dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		usage(os.Stderr)
		return nil
	}
	switch args[0] {
	case "version", "--version", "-v":
		fmt.Println("worldledger", version)
		return nil
	case "init":
		return cmdInit(args[1:])
	case "ingest":
		return cmdIngest(args[1:])
	case "ingest-bundle":
		return cmdIngestBundle(args[1:])
	case "inspect":
		return cmdInspect(args[1:])
	case "verify":
		return cmdVerify(args[1:])
	case "coverage":
		return cmdCoverage(args[1:])
	case "export":
		return cmdExport(args[1:])
	case "convert":
		return cmdConvert(args[1:])
	case "seed":
		return cmdSeed(args[1:])
	case "policy":
		return cmdPolicy(args[1:])
	case "manifest":
		return cmdManifest(args[1:])
	case "fingerprint":
		return cmdFingerprint(args[1:])
	case "fsck":
		return cmdFsck(args[1:])
	case "help", "--help", "-h":
		usage(os.Stdout)
		return nil
	default:
		usage(os.Stderr)
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func cmdIngestBundle(args []string) error {
	fs := flag.NewFlagSet("ingest-bundle", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	archivePath := fs.String("archive", "", "archive directory")
	deleteOnSuccess := fs.Bool("delete-on-success", false, "delete the source bundle after a successful import")
	limits := bundle.DefaultLimits()
	fs.Int64Var(&limits.MaxManifestBytes, "max-manifest-bytes", limits.MaxManifestBytes, "maximum bundle.json size")
	fs.IntVar(&limits.MaxComponents, "max-components", limits.MaxComponents, "maximum component count")
	fs.Int64Var(&limits.MaxComponentBytes, "max-component-bytes", limits.MaxComponentBytes, "maximum size of one component")
	fs.Int64Var(&limits.MaxTotalBytes, "max-total-bytes", limits.MaxTotalBytes, "maximum aggregate component size")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *archivePath == "" || fs.NArg() != 1 {
		return errors.New("usage: worldledger ingest-bundle --archive DIR [--delete-on-success] <bundle-dir>")
	}
	a, err := archive.Open(*archivePath)
	if err != nil {
		return err
	}
	result, err := bundle.Import(a, fs.Arg(0), bundle.Options{
		Limits:          limits,
		DeleteOnSuccess: *deleteOnSuccess,
	})
	if err != nil {
		return err
	}
	fmt.Printf("observation %s\nstate %s\ncomponents %d\n", result.ObservationID, result.StateDigest, result.Components)
	if result.Deleted {
		fmt.Println("bundle deleted")
	}
	return nil
}

func cmdInit(args []string) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: worldledger init <archive-dir>")
	}
	if _, err := archive.Init(fs.Arg(0)); err != nil {
		return err
	}
	fmt.Println("initialized", fs.Arg(0))
	return nil
}

func cmdIngest(args []string) error {
	fs := flag.NewFlagSet("ingest", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	archivePath := fs.String("archive", "", "archive directory")
	server := fs.String("server", "", "stable server id")
	dimension := fs.String("dimension", "overworld", "dimension id")
	x := fs.Int("x", 0, "chunk x")
	z := fs.Int("z", 0, "chunk z")
	observedAt := fs.String("observed-at", "", "RFC3339 timestamp")
	contributor := fs.String("contributor", "", "contributor id")
	agent := fs.String("agent", "", "capture agent")
	protocol := fs.String("protocol", "", "capture protocol/version")
	component := fs.String("component", "chunk", "component name")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *archivePath == "" || *server == "" || *contributor == "" || *observedAt == "" || fs.NArg() != 1 {
		return errors.New("usage: worldledger ingest --archive DIR --server ID --dimension DIM --x X --z Z --observed-at TIME --contributor ID [options] <payload-file>")
	}
	t, err := time.Parse(time.RFC3339Nano, *observedAt)
	if err != nil {
		return fmt.Errorf("parse observed-at: %w", err)
	}
	a, err := archive.Open(*archivePath)
	if err != nil {
		return err
	}
	f, err := os.Open(fs.Arg(0))
	if err != nil {
		return err
	}
	defer f.Close()
	ref, err := a.CAS.Put(f)
	if err != nil {
		return err
	}
	o := model.Observation{
		Chunk:      model.ChunkRef{ServerID: *server, Dimension: *dimension, X: int32(*x), Z: int32(*z)},
		ObservedAt: t,
		Protocol:   *protocol,
		Source:     model.Source{Contributor: *contributor, Agent: *agent},
		Components: map[string]model.BlobRef{*component: ref},
	}
	if err := o.Finalize(); err != nil {
		return err
	}
	if err := a.AddObservation(o); err != nil {
		return err
	}
	fmt.Printf("observation %s\nstate %s\nobject %s\n", o.ID, o.StateDigest, ref.Digest)
	return nil
}

func cmdInspect(args []string) error {
	a, chunk, err := parseChunkSelector("inspect", args)
	if err != nil {
		return err
	}
	obs, err := a.Observations(chunk)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(obs)
}

func cmdVerify(args []string) error {
	fs := flag.NewFlagSet("verify", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	archivePath := fs.String("archive", "", "archive directory")
	server := fs.String("server", "", "stable server id")
	dimension := fs.String("dimension", "overworld", "dimension id")
	x := fs.Int("x", 0, "chunk x")
	z := fs.Int("z", 0, "chunk z")
	window := fs.Duration("window", 10*time.Second, "observation comparison window")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *archivePath == "" || *server == "" {
		return errors.New("usage: worldledger verify --archive DIR --server ID --dimension DIM --x X --z Z [--window 10s]")
	}
	a, err := archive.Open(*archivePath)
	if err != nil {
		return err
	}
	obs, err := a.Observations(model.ChunkRef{ServerID: *server, Dimension: *dimension, X: int32(*x), Z: int32(*z)})
	if err != nil {
		return err
	}
	windows := verify.BuildWindows(obs, *window)
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(windows)
}

func cmdFsck(args []string) error {
	fs := flag.NewFlagSet("fsck", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	archivePath := fs.String("archive", "", "archive directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *archivePath == "" {
		return errors.New("usage: worldledger fsck --archive DIR")
	}
	a, err := archive.Open(*archivePath)
	if err != nil {
		return err
	}
	report := a.Check()
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(report); err != nil {
		return err
	}
	if len(report.Errors) != 0 {
		return fmt.Errorf("archive check found %d error(s)", len(report.Errors))
	}
	return nil
}

func parseChunkSelector(name string, args []string) (archive.Archive, model.ChunkRef, error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	archivePath := fs.String("archive", "", "archive directory")
	server := fs.String("server", "", "stable server id")
	dimension := fs.String("dimension", "overworld", "dimension id")
	x := fs.Int("x", 0, "chunk x")
	z := fs.Int("z", 0, "chunk z")
	if err := fs.Parse(args); err != nil {
		return archive.Archive{}, model.ChunkRef{}, err
	}
	if *archivePath == "" || *server == "" {
		return archive.Archive{}, model.ChunkRef{}, fmt.Errorf("usage: worldledger %s --archive DIR --server ID --dimension DIM --x X --z Z", name)
	}
	a, err := archive.Open(*archivePath)
	if err != nil {
		return archive.Archive{}, model.ChunkRef{}, err
	}
	return a, model.ChunkRef{ServerID: *server, Dimension: *dimension, X: int32(*x), Z: int32(*z)}, nil
}

func usage(w io.Writer) {
	lines := []string{
		"WorldLedger 鈥?community world observation archive",
		"",
		"Usage:",
		"  worldledger init <archive-dir>",
		"  worldledger ingest [flags] <payload-file>",
		"  worldledger ingest-bundle --archive <archive-dir> [flags] <bundle-dir>",
		"  worldledger inspect [flags]",
		"  worldledger verify [flags]",
		"  worldledger coverage --archive <archive-dir> --server <id> [flags]",
		"  worldledger export --archive <archive-dir> --server <id> --into <world-dir> [flags]",
		"  worldledger convert --archive <archive-dir> --server <id> --into <world-dir> --target-profile <file> [flags]",
		"  worldledger policy <show|set|list> --archive <archive-dir> [flags]",
		"  worldledger seed --observations <file> --operator <name> --accept-terms [flags]",
		"  worldledger manifest --archive <archive-dir> [--out <file>] [--compare <file>]",
		"  worldledger fingerprint --archive <archive-dir> [--server <id>] [--out <file>] [--compare <file>]",
		"  worldledger fsck --archive <archive-dir>",
		"  worldledger version",
		"",
		"Run a command with the source tree documentation for details.",
	}
	fmt.Fprintln(w, strings.Join(lines, "\n"))
}
