package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/worldledger/worldledger-mc/internal/archive"
	"github.com/worldledger/worldledger-mc/internal/epoch"
)

// defaultChangeListing is how many changed chunks are listed before the rest
// are summarised. A session can change hundreds of chunks and a wall of
// coordinates is not an answer to anything.
const defaultChangeListing = 20

func cmdDiff(args []string) error {
	fs := flag.NewFlagSet("diff", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	archiveDir := fs.String("archive", "", "archive directory")
	server := fs.String("server", "", "server id")
	dimension := fs.String("dimension", defaultDimension, "dimension id")
	fromFlag := fs.String("from", "", "start of the interval, RFC3339; defaults to the first observation")
	toFlag := fs.String("to", "", "end of the interval, RFC3339; defaults to now")
	since := fs.String("since", "", "start the interval this long before --to, for example 24h")
	asJSON := fs.Bool("json", false, "write the full comparison as JSON")
	limit := fs.Int("limit", defaultChangeListing, "how many changed chunks to list; 0 lists all")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *archiveDir == "" || *server == "" || *dimension == "" {
		return usageError("diff")
	}
	if *fromFlag != "" && *since != "" {
		return errors.New("pass either --from or --since, not both")
	}

	a, err := archive.Open(*archiveDir)
	if err != nil {
		return err
	}
	inputs, err := dimensionInputs(a, *server, *dimension)
	if err != nil {
		return err
	}
	if len(inputs) == 0 {
		return emptySelectionError(a, *server, *dimension, time.Now().UTC())
	}

	from, to, fromDefaulted, err := resolveInterval(*fromFlag, *toFlag, *since, inputs)
	if err != nil {
		return err
	}

	diff := epoch.BuildDiff(*server, *dimension, from, to, inputs)
	if *asJSON {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(diff)
	}
	printDiff(diff, observedSpan(inputs), fromDefaulted, *limit)
	return nil
}

// resolveInterval turns the three time flags into one interval. It reports
// whether the start was defaulted, because output that silently invents a
// boundary is output nobody can check.
func resolveInterval(fromFlag, toFlag, since string, inputs []epoch.ChunkInput) (
	from, to time.Time, fromDefaulted bool, err error) {
	to = time.Now().UTC()
	if toFlag != "" {
		parsed, parseErr := time.Parse(time.RFC3339Nano, toFlag)
		if parseErr != nil {
			return time.Time{}, time.Time{}, false, fmt.Errorf("--to must be an RFC3339 timestamp: %w", parseErr)
		}
		to = parsed.UTC()
	}

	switch {
	case fromFlag != "":
		parsed, parseErr := time.Parse(time.RFC3339Nano, fromFlag)
		if parseErr != nil {
			return time.Time{}, time.Time{}, false, fmt.Errorf("--from must be an RFC3339 timestamp: %w", parseErr)
		}
		from = parsed.UTC()
	case since != "":
		window, parseErr := time.ParseDuration(since)
		if parseErr != nil {
			return time.Time{}, time.Time{}, false,
				fmt.Errorf("--since must be a duration such as 24h or 30m: %w", parseErr)
		}
		if window <= 0 {
			return time.Time{}, time.Time{}, false, errors.New("--since must be positive")
		}
		from = to.Add(-window)
	default:
		earliest, ok := earliestObservation(inputs)
		if !ok {
			return time.Time{}, time.Time{}, false, errors.New("this dimension holds no observations")
		}
		from = earliest
		fromDefaulted = true
	}

	if !from.Before(to) {
		return time.Time{}, time.Time{}, false, fmt.Errorf(
			"the interval starts at %s and ends at %s, so there is nothing between them; "+
				"pass --from earlier than --to",
			from.Format(time.RFC3339), to.Format(time.RFC3339))
	}
	return from, to, fromDefaulted, nil
}

// observedRange is when this dimension was actually looked at. A diff whose
// interval falls outside it can only report unknowns, and saying so is more use
// than printing five zeroes.
type observedRange struct {
	first, last time.Time
	ok          bool
}

func observedSpan(inputs []epoch.ChunkInput) observedRange {
	var span observedRange
	for _, input := range inputs {
		for _, o := range input.Observations {
			at := o.ObservedAt.UTC()
			if !span.ok {
				span = observedRange{first: at, last: at, ok: true}
				continue
			}
			if at.Before(span.first) {
				span.first = at
			}
			if at.After(span.last) {
				span.last = at
			}
		}
	}
	return span
}

func earliestObservation(inputs []epoch.ChunkInput) (time.Time, bool) {
	span := observedSpan(inputs)
	return span.first, span.ok
}

func printDiff(diff epoch.Diff, span observedRange, fromDefaulted bool, limit int) {
	s := diff.Summary
	fromNote := ""
	if fromDefaulted {
		fromNote = "  (the first observation in this dimension)"
	}

	fmt.Printf("server        %s\n", diff.Server)
	fmt.Printf("dimension     %s\n", diff.Dimension)
	fmt.Printf("from          %s%s\n", diff.From.Format(time.RFC3339), fromNote)
	fmt.Printf("to            %s\n", diff.To.Format(time.RFC3339))
	fmt.Printf("policy        %s\n\n", diff.Policy)

	fmt.Printf("changed       %5d  observed on both sides, and the state differs\n", s.Changed)
	fmt.Printf("unchanged     %5d  observed again in this interval, and the state was the same\n", s.Unchanged)
	fmt.Printf("not revisited %5d  nobody observed these while the interval ran\n", s.NotRevisited)
	fmt.Printf("first seen    %5d  nothing was observed here before the interval\n", s.FirstSeen)
	fmt.Printf("never seen    %5d  observed only after the interval ended\n", s.NeverSeen)
	fmt.Printf("              %5d  chunks in total\n\n", s.Chunks)

	// The line that separates this from a plain comparison of two exports. A
	// chunk nobody revisited carries its old state forward, so it looks
	// unchanged and is not known to be.
	fmt.Printf("This archive can say what happened to %d of %d chunks.\n", s.Covered(), s.Chunks)
	if unobserved := s.NotRevisited + s.NeverSeen; unobserved > 0 {
		fmt.Printf("%d %s not observed while the interval ran, and %s reported as unknown\n",
			unobserved, wasWere(unobserved), isAre(unobserved))
		fmt.Println("rather than unchanged: carrying an old state forward is not the same as")
		fmt.Println("going back and finding it the same.")
	}
	if s.FirstSeen > 0 {
		fmt.Printf("%d %s observed for the first time, so there is nothing to compare %s to.\n",
			s.FirstSeen, wasWere(s.FirstSeen), itThem(s.FirstSeen))
	}
	if len(s.Contributors) > 0 {
		fmt.Printf("\nObserved during this interval by: %s\n", strings.Join(s.Contributors, ", "))
	}
	// Last, so that a diff which could not compare anything ends with the
	// command that would.
	printIntervalAdvice(diff, span, fromDefaulted)

	changed := make([]epoch.ChunkChange, 0, s.Changed)
	for _, change := range diff.Changes {
		if change.Kind == epoch.ChangeChanged {
			changed = append(changed, change)
		}
	}
	if len(changed) == 0 {
		return
	}

	// Most recently changed first: someone reading a list of changes wants the
	// newest, not the chunk that happens to sit at the lowest coordinate.
	sort.SliceStable(changed, func(i, j int) bool {
		return latestRevisit(changed[i]).After(latestRevisit(changed[j]))
	})

	shown := len(changed)
	if limit > 0 && limit < shown {
		shown = limit
	}
	fmt.Printf("\nchanged chunks, most recent first\n")
	for _, change := range changed[:shown] {
		when := latestRevisit(change)
		fmt.Printf("  (%d,%d)  %s -> %s  by %s",
			change.Chunk.X, change.Chunk.Z,
			shortDigest(change.FromDigest), shortDigest(change.ToDigest),
			strings.Join(change.Contributors(), ", "))
		if !when.IsZero() {
			fmt.Printf("  at %s", when.Format(time.RFC3339))
		}
		if change.ToStatus == epoch.StatusConflict {
			fmt.Printf("  [contributors disagree about this state]")
		}
		fmt.Println()
	}
	if shown < len(changed) {
		fmt.Printf("  ... and %d more; pass --limit 0 to list them all, or --json for everything\n",
			len(changed)-shown)
	}
}

// printIntervalAdvice explains a diff that could not compare anything.
//
// The commonest way to get one is to accept the default interval, which starts
// at the first observation: a chunk is first-seen exactly when nothing was
// observed at or before the start, so almost everything lands there. The answer
// is correct and tells the reader nothing, and the fix is a narrower interval
// inside the range that was actually observed.
func printIntervalAdvice(diff epoch.Diff, span observedRange, fromDefaulted bool) {
	if diff.Summary.Covered() > 0 || !span.ok {
		return
	}
	fmt.Printf("\nEvery observation in this dimension falls between\n  %s\n  %s\n",
		span.first.Format(time.RFC3339Nano), span.last.Format(time.RFC3339Nano))
	fmt.Printf("a span of %s.\n", span.last.Sub(span.first).Round(time.Second))

	if fromDefaulted {
		fmt.Println("\nThe interval above started at the first of those, so nothing had been")
		fmt.Println("observed before it and there was nothing to compare against. Pick two")
		fmt.Println("moments inside the span instead:")
	} else {
		fmt.Println("\nThe interval given does not have observations on both sides of its start,")
		fmt.Println("so there was nothing to compare against. Pick two moments inside the span:")
	}
	fmt.Printf("  worldledger diff --archive DIR --server %s --dimension %s \\\n      --from %s --to %s\n",
		diff.Server, diff.Dimension,
		span.first.Add(span.last.Sub(span.first)/2).Format(time.RFC3339),
		span.last.Format(time.RFC3339))
}

// A count of one reads badly with a plural verb, and these lines are the part
// of the output a reader is meant to take a claim from.
func wasWere(n int) string {
	if n == 1 {
		return "was"
	}
	return "were"
}

func isAre(n int) string {
	if n == 1 {
		return "is"
	}
	return "are"
}

func itThem(n int) string {
	if n == 1 {
		return "it"
	}
	return "them"
}

func latestRevisit(change epoch.ChunkChange) time.Time {
	if len(change.Revisits) == 0 {
		return time.Time{}
	}
	return change.Revisits[len(change.Revisits)-1].ObservedAt
}

func shortDigest(digest string) string {
	if len(digest) < 12 {
		return digest
	}
	return digest[:12]
}
