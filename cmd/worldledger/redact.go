package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/worldledger/worldledger-mc/internal/archive"
	"github.com/worldledger/worldledger-mc/internal/redact"
)

func cmdRedact(args []string) error {
	if len(args) == 0 {
		return usageError("redact")
	}
	switch args[0] {
	case "set":
		return cmdRedactSet(args[1:])
	case "list":
		return cmdRedactList(args[1:])
	case "withdraw":
		return cmdRedactWithdraw(args[1:])
	case "purge":
		return cmdRedactPurge(args[1:])
	default:
		return fmt.Errorf("unknown redact subcommand %q; want set, list, withdraw, or purge", args[0])
	}
}

type regionFlag struct {
	value *redact.Region
}

// Set parses "minX,minZ,maxX,maxZ" in chunk coordinates.
func (f *regionFlag) Set(raw string) error {
	fields := strings.Split(raw, ",")
	if len(fields) != 4 {
		return fmt.Errorf("want four chunk coordinates as minX,minZ,maxX,maxZ, got %q", raw)
	}
	var bounds [4]int32
	for index, field := range fields {
		var parsed int64
		if _, err := fmt.Sscanf(strings.TrimSpace(field), "%d", &parsed); err != nil {
			return fmt.Errorf("%q is not a chunk coordinate", field)
		}
		bounds[index] = int32(parsed)
	}
	f.value = &redact.Region{MinX: bounds[0], MinZ: bounds[1], MaxX: bounds[2], MaxZ: bounds[3]}
	return nil
}

func (f *regionFlag) String() string {
	if f == nil || f.value == nil {
		return ""
	}
	return f.value.String()
}

func cmdRedactSet(args []string) error {
	fs := flag.NewFlagSet("redact set", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	archivePath := fs.String("archive", "", "archive directory")
	server := fs.String("server", "", "stable server id")
	contributor := fs.String("contributor", "", "limit to one contributor; omit for every contributor")
	dimension := fs.String("dimension", "", "limit to one dimension; omit for every dimension")
	reason := fs.String("reason", "", "why this was withheld")
	declaredBy := fs.String("declared-by", "", "who decided")
	region := &regionFlag{}
	fs.Var(region, "region", "chunk bounds as minX,minZ,maxX,maxZ")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *archivePath == "" || *server == "" {
		return usageError("redact set")
	}

	a, err := archive.Open(*archivePath)
	if err != nil {
		return err
	}
	declared, err := redact.NewStore(a.Root).Declare(redact.Redaction{
		Server:      *server,
		Contributor: *contributor,
		Dimension:   *dimension,
		Region:      region.value,
		Reason:      *reason,
		DeclaredBy:  *declaredBy,
	})
	if err != nil {
		return err
	}

	matched, err := countMatching(a, declared)
	if err != nil {
		return err
	}
	fmt.Printf("declared %s\n", declared.ID[:12])
	fmt.Printf("scope    %s\n", declared.Describe())
	fmt.Printf("matches  %d observation(s) currently in the archive\n", matched)
	fmt.Println("\nThese are now withheld from coverage, export, and convert. They are still")
	fmt.Println("stored. Run 'worldledger redact purge' to remove what can be removed.")
	return nil
}

func cmdRedactList(args []string) error {
	fs := flag.NewFlagSet("redact list", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	archivePath := fs.String("archive", "", "archive directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *archivePath == "" {
		return usageError("redact list")
	}
	a, err := archive.Open(*archivePath)
	if err != nil {
		return err
	}
	declared, err := redact.NewStore(a.Root).List()
	if err != nil {
		return err
	}
	if len(declared) == 0 {
		fmt.Println("no redaction has been declared")
		return nil
	}
	for _, redaction := range declared {
		matched, err := countMatching(a, redaction)
		if err != nil {
			return err
		}
		fmt.Printf("%s  %s\n", redaction.ID[:12], redaction.Describe())
		fmt.Printf("  declared by %s on %s\n", redaction.DeclaredBy, redaction.DeclaredAt.Format(time.RFC3339))
		fmt.Printf("  reason      %s\n", redaction.Reason)
		fmt.Printf("  matches     %d observation(s) still stored\n", matched)
	}
	return nil
}

func cmdRedactWithdraw(args []string) error {
	fs := flag.NewFlagSet("redact withdraw", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	archivePath := fs.String("archive", "", "archive directory")
	id := fs.String("id", "", "redaction id from redact list")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *archivePath == "" || *id == "" {
		return usageError("redact withdraw")
	}
	a, err := archive.Open(*archivePath)
	if err != nil {
		return err
	}
	store := redact.NewStore(a.Root)
	full, err := resolveRedactionID(store, *id)
	if err != nil {
		return err
	}
	removed, err := store.Withdraw(full)
	if err != nil {
		return err
	}
	if !removed {
		return fmt.Errorf("no redaction with id %s", *id)
	}
	fmt.Printf("withdrew %s\n", full[:12])
	fmt.Println("Observations it covered are visible again. Anything already purged is gone.")
	return nil
}

func cmdRedactPurge(args []string) error {
	fs := flag.NewFlagSet("redact purge", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	archivePath := fs.String("archive", "", "archive directory")
	confirm := fs.Bool("yes", false, "carry out the removal rather than describing it")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *archivePath == "" {
		return usageError("redact purge")
	}
	a, err := archive.Open(*archivePath)
	if err != nil {
		return err
	}
	declared, err := redact.NewStore(a.Root).List()
	if err != nil {
		return err
	}
	if len(declared) == 0 {
		return errors.New("no redaction has been declared, so there is nothing to purge")
	}

	doomed, err := matchingObservationIDs(a, declared)
	if err != nil {
		return err
	}
	if len(doomed) == 0 {
		fmt.Println("no stored observation matches any declared redaction")
		return nil
	}

	if !*confirm {
		fmt.Printf("%d observation(s) would be removed under %d redaction(s).\n", len(doomed), len(declared))
		fmt.Println("This is irreversible. Re-run with --yes to carry it out.")
		return nil
	}

	result, err := a.RemoveObservations(doomed)
	if err != nil {
		return err
	}
	fmt.Printf("removed %d observation(s)\n", result.ObservationsRemoved)
	fmt.Printf("removed %d object(s), %s\n", result.ObjectsRemoved, humanBytes(result.BytesRemoved))

	if len(result.ObjectsRetained) == 0 {
		return nil
	}
	// The honest part. Objects are addressed by content, so bytes another
	// contributor independently observed are not the withdrawing party's to
	// remove, and saying otherwise would be a false assurance.
	fmt.Printf("\n%d object(s) stayed because a surviving observation still references them:\n", len(result.ObjectsRetained))
	for index, retained := range result.ObjectsRetained {
		if index == 20 {
			fmt.Printf("  ... %d more\n", len(result.ObjectsRetained)-20)
			break
		}
		fmt.Printf("  %s  also observed by %s\n", retained.Digest[:12], strings.Join(retained.Contributors, ", "))
	}
	fmt.Println("\nThose bytes were observed independently by someone else. Removing the")
	fmt.Println("records did not remove them, and this archive cannot claim otherwise.")
	return nil
}

func resolveRedactionID(store redact.Store, prefix string) (string, error) {
	declared, err := store.List()
	if err != nil {
		return "", err
	}
	var matches []string
	for _, redaction := range declared {
		if strings.HasPrefix(redaction.ID, prefix) {
			matches = append(matches, redaction.ID)
		}
	}
	switch len(matches) {
	case 0:
		return "", fmt.Errorf("no redaction id starts with %q", prefix)
	case 1:
		return matches[0], nil
	default:
		return "", fmt.Errorf("%q matches %d redactions; give more of the id", prefix, len(matches))
	}
}

func matchingObservationIDs(a archive.Archive, set redact.Set) ([]string, error) {
	servers, err := a.Servers()
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, server := range servers {
		dimensions, err := a.Dimensions(server)
		if err != nil {
			return nil, err
		}
		for _, dimension := range dimensions {
			chunks, err := a.DimensionObservations(server, dimension)
			if err != nil {
				return nil, err
			}
			for _, chunk := range chunks {
				_, withheld := set.Filter(chunk.Observations)
				for _, observation := range withheld {
					ids = append(ids, observation.ID)
				}
			}
		}
	}
	return ids, nil
}

func countMatching(a archive.Archive, redaction redact.Redaction) (int, error) {
	ids, err := matchingObservationIDs(a, redact.Set{redaction})
	if err != nil {
		return 0, err
	}
	return len(ids), nil
}
