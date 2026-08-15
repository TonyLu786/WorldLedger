package main

import (
	"strings"
	"testing"
	"time"

	"github.com/worldledger/worldledger-mc/internal/epoch"
	"github.com/worldledger/worldledger-mc/internal/model"
)

func moment(t *testing.T, text string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339Nano, text)
	if err != nil {
		t.Fatal(err)
	}
	return parsed.UTC()
}

func inputsObservedAt(t *testing.T, times ...string) []epoch.ChunkInput {
	t.Helper()
	chunk := model.ChunkRef{ServerID: "example.org", Dimension: "minecraft:overworld"}
	var observations []model.Observation
	for _, text := range times {
		observations = append(observations, model.Observation{
			Chunk:      chunk,
			ObservedAt: moment(t, text),
			Source:     model.Source{Contributor: "alice"},
		})
	}
	return []epoch.ChunkInput{{Chunk: chunk, Observations: observations}}
}

func TestSinceMeasuresBackFromTheEndOfTheInterval(t *testing.T) {
	inputs := inputsObservedAt(t, "2026-08-01T00:00:00Z")
	from, to, defaulted, err := resolveInterval("", "2026-08-10T12:00:00Z", "24h", inputs)
	if err != nil {
		t.Fatal(err)
	}
	if want := moment(t, "2026-08-09T12:00:00Z"); !from.Equal(want) {
		t.Fatalf("from = %s; want %s", from, want)
	}
	if want := moment(t, "2026-08-10T12:00:00Z"); !to.Equal(want) {
		t.Fatalf("to = %s; want %s", to, want)
	}
	if defaulted {
		t.Fatal("--since sets the start, so it was not defaulted")
	}
}

func TestTheStartDefaultsToTheFirstObservation(t *testing.T) {
	inputs := inputsObservedAt(t, "2026-08-05T00:00:00Z", "2026-08-01T00:00:00Z", "2026-08-09T00:00:00Z")
	from, _, defaulted, err := resolveInterval("", "", "", inputs)
	if err != nil {
		t.Fatal(err)
	}
	if want := moment(t, "2026-08-01T00:00:00Z"); !from.Equal(want) {
		t.Fatalf("from = %s; want the earliest observation %s", from, want)
	}
	if !defaulted {
		t.Fatal("the start was defaulted and the caller has to be told, or the output invents a boundary")
	}
}

// An interval that ends before it begins compares nothing, and the numbers it
// would print are not obviously wrong to a reader. Refuse it instead.
func TestAnIntervalThatEndsBeforeItStartsIsRefused(t *testing.T) {
	inputs := inputsObservedAt(t, "2026-08-01T00:00:00Z")
	_, _, _, err := resolveInterval("2026-08-10T00:00:00Z", "2026-08-09T00:00:00Z", "", inputs)
	if err == nil {
		t.Fatal("an interval running backwards was accepted")
	}
	if !strings.Contains(err.Error(), "--from earlier than --to") {
		t.Fatalf("the error does not say how to fix it: %v", err)
	}
}

func TestAnEmptyIntervalIsRefused(t *testing.T) {
	inputs := inputsObservedAt(t, "2026-08-01T00:00:00Z")
	if _, _, _, err := resolveInterval("2026-08-09T00:00:00Z", "2026-08-09T00:00:00Z", "", inputs); err == nil {
		t.Fatal("an interval of zero length was accepted")
	}
}

func TestABadTimestampSaysWhichFlagItCameFrom(t *testing.T) {
	inputs := inputsObservedAt(t, "2026-08-01T00:00:00Z")
	_, _, _, err := resolveInterval("yesterday", "", "", inputs)
	if err == nil || !strings.Contains(err.Error(), "--from") {
		t.Fatalf("err = %v; want one naming --from", err)
	}
	_, _, _, err = resolveInterval("", "tomorrow", "", inputs)
	if err == nil || !strings.Contains(err.Error(), "--to") {
		t.Fatalf("err = %v; want one naming --to", err)
	}
	_, _, _, err = resolveInterval("", "", "a fortnight", inputs)
	if err == nil || !strings.Contains(err.Error(), "--since") {
		t.Fatalf("err = %v; want one naming --since", err)
	}
}

func TestANegativeSinceIsRefused(t *testing.T) {
	inputs := inputsObservedAt(t, "2026-08-01T00:00:00Z")
	if _, _, _, err := resolveInterval("", "", "-24h", inputs); err == nil {
		t.Fatal("a negative --since was accepted, which would put the start after the end")
	}
}

func TestTheObservedSpanCoversTheOldestAndNewestObservation(t *testing.T) {
	inputs := inputsObservedAt(t,
		"2026-08-05T00:00:00Z", "2026-08-01T00:00:00Z", "2026-08-09T00:00:00Z")
	span := observedSpan(inputs)
	if !span.ok {
		t.Fatal("span not found")
	}
	if want := moment(t, "2026-08-01T00:00:00Z"); !span.first.Equal(want) {
		t.Fatalf("first = %s; want %s", span.first, want)
	}
	if want := moment(t, "2026-08-09T00:00:00Z"); !span.last.Equal(want) {
		t.Fatalf("last = %s; want %s", span.last, want)
	}
}

func TestAnEmptySpanIsReportedRatherThanGuessed(t *testing.T) {
	if span := observedSpan(nil); span.ok {
		t.Fatal("a span was reported for no observations at all")
	}
}

func TestShortDigestLeavesShortInputAlone(t *testing.T) {
	if got := shortDigest("abc"); got != "abc" {
		t.Fatalf("shortDigest(abc) = %q", got)
	}
	if got := shortDigest(strings.Repeat("a", 64)); len(got) != 12 {
		t.Fatalf("shortDigest of a full digest = %q; want 12 characters", got)
	}
}
