package main

import (
	"encoding/json"
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

// version is overridden at release time with -ldflags "-X main.version=...".
// A released binary that reports a development version is worse than useless:
// it is the one thing a person checks when deciding whether a bug is already
// fixed.
var version = "0.1.0-dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// defaultDimension is what a client reports and what an archive stores. A bare
// "overworld" is a different string that matches nothing, so every command that
// defaults this has to default it the same way.
const defaultDimension = "minecraft:overworld"

func run(args []string) error {
	if len(args) == 0 {
		usage(os.Stdout)
		return nil
	}
	// Answered before dispatch so that every command supports it identically,
	// and on stdout with a zero exit, because being asked for help is not a
	// failure and the answer is what the caller wanted.
	if name, ok := helpRequest(args); ok {
		return printHelp(os.Stdout, name)
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
	case "ingest-spool":
		return cmdIngestSpool(args[1:])
	case "inspect":
		return cmdInspect(args[1:])
	case "verify":
		return cmdVerify(args[1:])
	case "coverage":
		return cmdCoverage(args[1:])
	case "diff":
		return cmdDiff(args[1:])
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
	case "redact":
		return cmdRedact(args[1:])
	case "identity":
		return cmdIdentity(args[1:])
	case "attest":
		return cmdAttest(args[1:])
	case "send":
		return cmdSend(args[1:])
	case "receive":
		return cmdReceive(args[1:])
	case "status":
		return cmdStatus(args[1:])
	case "fsck":
		return cmdFsck(args[1:])
	default:
		// The suggestion comes first and alone. Printing twenty-five lines of
		// usage above it buried the one line that says what went wrong.
		return unknownCommandError(args[0])
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
		return usageError("ingest-bundle")
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
		return usageError("init")
	}
	if _, err := archive.Init(fs.Arg(0)); err != nil {
		return err
	}
	fmt.Println("initialized", fs.Arg(0))
	// An empty archive is not the goal, and the command that fills it takes a
	// directory most people have never been told about. Naming it here means
	// the next step never has to be looked up.
	fmt.Println("\nNext: import what the mod captured. With Minecraft in its usual place,")
	fmt.Printf("      the spool is found for you:\n  worldledger ingest-spool --archive %s\n", fs.Arg(0))
	return nil
}

func cmdIngest(args []string) error {
	fs := flag.NewFlagSet("ingest", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	archivePath := fs.String("archive", "", "archive directory")
	server := fs.String("server", "", "stable server id")
	dimension := fs.String("dimension", defaultDimension, "dimension id")
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
		return usageError("ingest")
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
	if len(obs) == 0 {
		// Encoding an empty result gave a bare "null", which reads as a broken
		// command rather than as an answer. The chunk selector defaults to 0,0,
		// so the commonest way to get here is accepting defaults that were never
		// meant to be a query.
		return emptyChunkError(a, chunk)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(obs)
}

// emptyChunkError says which of several situations produced no observations,
// in the manner emptySelectionError already does for a whole dimension.
func emptyChunkError(a archive.Archive, chunk model.ChunkRef) error {
	servers, err := a.Servers()
	if err != nil || len(servers) == 0 {
		return fmt.Errorf("this archive holds no observations yet; import a capture with: %s",
			usageLine("ingest-spool"))
	}
	if !contains(servers, model.NormalizeToken(chunk.ServerID)) {
		return fmt.Errorf("this archive holds nothing for server %q; it knows about %s",
			chunk.ServerID, strings.Join(servers, ", "))
	}
	dimensions, err := a.Dimensions(chunk.ServerID)
	if err == nil && len(dimensions) > 0 && !contains(dimensions, model.NormalizeToken(chunk.Dimension)) {
		return fmt.Errorf("server %s has no dimension %q; it has %s",
			chunk.ServerID, chunk.Dimension, strings.Join(dimensions, ", "))
	}
	return fmt.Errorf(
		"no observations for chunk %d,%d in %s on %s; coverage lists the chunks that were observed:\n  %s",
		chunk.X, chunk.Z, chunk.Dimension, chunk.ServerID, usageLine("coverage"))
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func cmdVerify(args []string) error {
	fs := flag.NewFlagSet("verify", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	archivePath := fs.String("archive", "", "archive directory")
	server := fs.String("server", "", "stable server id")
	dimension := fs.String("dimension", defaultDimension, "dimension id")
	x := fs.Int("x", 0, "chunk x")
	z := fs.Int("z", 0, "chunk z")
	window := fs.Duration("window", 10*time.Second, "observation comparison window")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *archivePath == "" || *server == "" {
		return usageError("verify")
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
		return usageError("fsck")
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
	// Archives store the namespaced identifier a client reports. A bare
	// "overworld" is a different string and matches nothing, so this default
	// used to guarantee an empty answer for anyone who accepted it.
	dimension := fs.String("dimension", defaultDimension, "dimension id")
	x := fs.Int("x", 0, "chunk x")
	z := fs.Int("z", 0, "chunk z")
	if err := fs.Parse(args); err != nil {
		return archive.Archive{}, model.ChunkRef{}, err
	}
	if *archivePath == "" || *server == "" {
		return archive.Archive{}, model.ChunkRef{}, usageError(name)
	}
	a, err := archive.Open(*archivePath)
	if err != nil {
		return archive.Archive{}, model.ChunkRef{}, err
	}
	return a, model.ChunkRef{ServerID: *server, Dimension: *dimension, X: int32(*x), Z: int32(*z)}, nil
}

func usage(w io.Writer) {
	// Grouped by what someone is trying to do. Twenty-one commands in one list
	// tells a newcomer nothing about which of them they need first, and the
	// first four are the whole path from an empty directory to a world.
	lines := []string{
		"WorldLedger - community world observation archive",
		"",
		"From a capture to a world:",
		"  worldledger init <archive-dir>",
		"  worldledger ingest-spool --archive <archive-dir> [--keep] [--dry-run] <spool-dir>",
		"  worldledger status --archive <archive-dir> [--spool <spool-dir>]",
		"  worldledger export --archive <archive-dir> --server <id> --into <world-dir> [flags]",
		"",
		"Reading an archive:",
		"  worldledger coverage --archive <archive-dir> --server <id> [flags]",
		"  worldledger diff --archive <archive-dir> --server <id> [--since <dur>] [flags]",
		"  worldledger inspect [flags]",
		"  worldledger verify [flags]",
		"  worldledger fsck --archive <archive-dir>",
		"",
		"Deciding what may be shared:",
		"  worldledger policy <show|set|list> --archive <archive-dir> [flags]",
		"  worldledger redact <set|list|withdraw|purge> --archive <archive-dir> [flags]",
		"",
		"Exchanging with another archive:",
		"  worldledger manifest --archive <archive-dir> [--out <file>] [--compare <file>]",
		"  worldledger fingerprint (--archive <archive-dir> | --file <file>) [--out <file>] [--compare <file>]",
		"  worldledger send --archive <archive-dir> --to <fingerprint-file> --out <bundle-dir>",
		"  worldledger receive --archive <archive-dir> <bundle-dir>",
		"  worldledger identity <create|register|list|remove> --archive <archive-dir> [flags]",
		"  worldledger attest <sign|verify> --archive <archive-dir> [flags]",
		"",
		"Less often:",
		"  worldledger convert --archive <archive-dir> --server <id> --into <world-dir> --target-profile <file> [flags]",
		"  worldledger ingest-bundle --archive <archive-dir> [flags] <bundle-dir>",
		"  worldledger ingest [flags] <payload-file>",
		"  worldledger seed --observations <file> --operator <name> --accept-terms [flags]",
		"  worldledger version",
		"",
		"Add --help to any command to see what it takes.",
		"README.md alongside this binary explains what each one is for.",
	}
	fmt.Fprintln(w, strings.Join(lines, "\n"))
}
