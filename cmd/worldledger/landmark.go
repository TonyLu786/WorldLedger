package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/worldledger/worldledger-mc/internal/archive"
	"github.com/worldledger/worldledger-mc/internal/epoch"
	"github.com/worldledger/worldledger-mc/internal/landmark"
	"github.com/worldledger/worldledger-mc/internal/model"
)

func cmdLandmark(args []string) error {
	if len(args) == 0 {
		return usageError("landmark")
	}
	switch args[0] {
	case "set":
		return cmdLandmarkSet(args[1:])
	case "list":
		return cmdLandmarkList(args[1:])
	case "remove":
		return cmdLandmarkRemove(args[1:])
	default:
		return fmt.Errorf("unknown landmark subcommand %q; want set, list, or remove", args[0])
	}
}

func cmdLandmarkSet(args []string) error {
	fs := flag.NewFlagSet("landmark set", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	archivePath := fs.String("archive", "", "archive directory")
	server := fs.String("server", "", "server id")
	dimension := fs.String("dimension", defaultDimension, "dimension id")
	name := fs.String("name", "", "what to call this place")
	note := fs.String("note", "", "optional description")
	declaredBy := fs.String("declared-by", "", "who says so")
	bounds := &chunkBoundsFlag{}
	fs.Var(bounds, "region", "chunk bounds as minX,minZ,maxX,maxZ")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *archivePath == "" || *server == "" || *name == "" || *declaredBy == "" || bounds.value == nil {
		return usageError("landmark set")
	}

	a, err := archive.Open(*archivePath)
	if err != nil {
		return err
	}
	declared, err := landmark.NewStore(a.Root).Declare(landmark.Landmark{
		Server:     *server,
		Dimension:  *dimension,
		Name:       *name,
		Bounds:     *bounds.value,
		Note:       *note,
		DeclaredBy: *declaredBy,
	})
	if err != nil {
		return err
	}
	fmt.Printf("declared %q over %s on %s by %s\n",
		declared.Name, declared.Bounds, declared.Server, declared.DeclaredBy)
	fmt.Printf("\nNext: see how much of it this archive has:\n"+
		"  worldledger coverage --archive %s --server %s --landmark %q\n",
		*archivePath, declared.Server, declared.Name)
	return nil
}

func cmdLandmarkList(args []string) error {
	fs := flag.NewFlagSet("landmark list", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	archivePath := fs.String("archive", "", "archive directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *archivePath == "" {
		return usageError("landmark list")
	}
	a, err := archive.Open(*archivePath)
	if err != nil {
		return err
	}
	set, err := landmark.NewStore(a.Root).List()
	if err != nil {
		return err
	}
	if len(set) == 0 {
		fmt.Println("no landmarks declared")
		fmt.Println("\nA landmark gives an area a name, so coverage can say \"spawn\" rather than")
		fmt.Println("a range of chunk coordinates:")
		fmt.Printf("  %s\n", usageLine("landmark set"))
		return nil
	}
	for _, place := range set {
		fmt.Printf("%-24s %s %s\n", place.Name, place.Server, place.Dimension)
		fmt.Printf("  %s, %d chunk(s)\n", place.Bounds, place.Bounds.Chunks())
		fmt.Printf("  declared by %s\n", place.DeclaredBy)
		if place.Note != "" {
			fmt.Printf("  %s\n", place.Note)
		}
	}
	fmt.Println("\nLandmarks are local. A transfer bundle carries observations, and a name")
	fmt.Println("somebody chose for a place is not one.")
	return nil
}

func cmdLandmarkRemove(args []string) error {
	fs := flag.NewFlagSet("landmark remove", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	archivePath := fs.String("archive", "", "archive directory")
	server := fs.String("server", "", "server id")
	dimension := fs.String("dimension", defaultDimension, "dimension id")
	name := fs.String("name", "", "which landmark")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *archivePath == "" || *server == "" || *name == "" {
		return usageError("landmark remove")
	}
	a, err := archive.Open(*archivePath)
	if err != nil {
		return err
	}
	store := landmark.NewStore(a.Root)
	set, err := store.List()
	if err != nil {
		return err
	}
	place, found := set.Find(*server, *dimension, *name)
	if !found {
		return unknownLandmarkError(set, *server, *dimension, *name)
	}
	if _, err := store.Remove(place.ID); err != nil {
		return err
	}
	fmt.Printf("removed %q\n", place.Name)
	fmt.Println("Nothing observed was touched; a landmark is a name, not evidence.")
	return nil
}

func unknownLandmarkError(set landmark.Set, server, dimension, name string) error {
	known := set.Names(server, dimension)
	if len(known) == 0 {
		return fmt.Errorf("no landmark %q, and none are declared for %s %s",
			name, model.NormalizeToken(server), model.NormalizeToken(dimension))
	}
	return fmt.Errorf("no landmark %q; this archive knows %s", name, strings.Join(known, ", "))
}

// chunkBoundsFlag parses "minX,minZ,maxX,maxZ" in chunk coordinates. It is
// shared with redact, which names an area for the opposite reason.
type chunkBoundsFlag struct {
	value *model.ChunkBounds
}

func (f *chunkBoundsFlag) Set(raw string) error {
	fields := strings.Split(raw, ",")
	if len(fields) != 4 {
		return fmt.Errorf("want four chunk coordinates as minX,minZ,maxX,maxZ, got %q", raw)
	}
	var parsed [4]int32
	for index, field := range fields {
		var value int64
		if _, err := fmt.Sscanf(strings.TrimSpace(field), "%d", &value); err != nil {
			return fmt.Errorf("%q is not a chunk coordinate", field)
		}
		parsed[index] = int32(value)
	}
	f.value = &model.ChunkBounds{MinX: parsed[0], MinZ: parsed[1], MaxX: parsed[2], MaxZ: parsed[3]}
	return nil
}

func (f *chunkBoundsFlag) String() string {
	if f == nil || f.value == nil {
		return ""
	}
	return f.value.String()
}

// landmarkCoverage works out how much of each landmark a snapshot has readings
// for.
//
// A chunk with no observed state at the instant counts against the landmark
// rather than being left out of the total. "Eighty percent of spawn" has to
// mean eighty percent of the place, not of the part somebody already visited,
// or the number climbs to complete without anybody going anywhere.
func landmarkCoverage(set landmark.Set, snapshot epoch.Snapshot) []landmark.Coverage {
	observed := map[[2]int32]bool{}
	for _, selection := range snapshot.Selections {
		if selection.Known() {
			observed[[2]int32{selection.Chunk.X, selection.Chunk.Z}] = true
		}
	}

	var coverage []landmark.Coverage
	for _, place := range set {
		if place.Server != snapshot.Server || place.Dimension != snapshot.Dimension {
			continue
		}
		entry := landmark.Coverage{Landmark: place, Total: place.Bounds.Chunks()}
		for x := place.Bounds.MinX; x <= place.Bounds.MaxX; x++ {
			for z := place.Bounds.MinZ; z <= place.Bounds.MaxZ; z++ {
				if observed[[2]int32{x, z}] {
					entry.Observed++
				}
			}
		}
		coverage = append(coverage, entry)
	}
	sort.Slice(coverage, func(i, j int) bool {
		return strings.ToLower(coverage[i].Landmark.Name) < strings.ToLower(coverage[j].Landmark.Name)
	})
	return coverage
}

func printLandmarkCoverage(coverage []landmark.Coverage) {
	if len(coverage) == 0 {
		return
	}
	fmt.Println("\nlandmarks")
	for _, entry := range coverage {
		state := fmt.Sprintf("%d of %d chunk(s), %.0f%%",
			entry.Observed, entry.Total, entry.Fraction()*100)
		if entry.Complete() {
			state = fmt.Sprintf("all %d chunk(s)", entry.Total)
		} else if entry.Observed == 0 {
			state = fmt.Sprintf("none of its %d chunk(s)", entry.Total)
		}
		fmt.Printf("  %-24s %s\n", entry.Landmark.Name, state)
	}
}

// requireLandmark resolves --landmark to the area it names.
func requireLandmark(a archive.Archive, server, dimension, name string) (landmark.Landmark, error) {
	set, err := landmark.NewStore(a.Root).List()
	if err != nil {
		return landmark.Landmark{}, err
	}
	place, found := set.Find(server, dimension, name)
	if !found {
		return landmark.Landmark{}, unknownLandmarkError(set, server, dimension, name)
	}
	return place, nil
}

// restrictToLandmark drops selections outside the named area.
func restrictToLandmark(snapshot epoch.Snapshot, place landmark.Landmark) epoch.Snapshot {
	restricted := snapshot
	restricted.Selections = nil
	restricted.Summary = epoch.Summary{}
	for _, selection := range snapshot.Selections {
		if !place.Bounds.Contains(selection.Chunk.X, selection.Chunk.Z) {
			continue
		}
		restricted.Selections = append(restricted.Selections, selection)
		restricted.Summary.Chunks++
		switch selection.Status {
		case epoch.StatusCorroborated:
			restricted.Summary.Corroborated++
		case epoch.StatusSingleSource:
			restricted.Summary.SingleSource++
		case epoch.StatusConflict:
			restricted.Summary.Conflict++
		case epoch.StatusSuperseded:
			restricted.Summary.Superseded++
		default:
			restricted.Summary.Unknown++
		}
	}
	return restricted
}

var errLandmarkHasNothing = errors.New("this landmark has no observed chunks")
