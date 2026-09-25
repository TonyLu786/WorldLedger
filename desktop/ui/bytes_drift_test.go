package ui

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Both front ends turn a byte count into words, and they did it differently.
//
// humanBytes in cmd/worldledger divides by 1024 and writes KiB, MiB, GiB. The
// window divided by 1024 and wrote KB, MB, GB. The same archive read 8.4 KiB on
// the command line and 8.4 KB in the window: one quantity, two names, and the
// window's was wrong for the arithmetic it had just done.
//
// Nobody would report that as a bug. It looks like the window using the unit
// people say out loud, and it is the same shape as every other thing in this
// file: two places deciding one thing, and no one counting.

var (
	goUnitRunes = regexp.MustCompile(`"([A-Z]+)"\[exp\]`)
	jsUnitList  = regexp.MustCompile(`const units = \[([^\]]*)\]`)
)

func TestBothFrontEndsNameAByteCountTheSameWay(t *testing.T) {
	terminal := readFile(t, filepath.Join("..", "..", "cmd", "worldledger", "manifest.go"))
	window := readFile(t, filepath.Join("assets", "app.js"))

	// The command line builds its unit from a rune of "KMGTPE" plus a literal
	// "iB", so the prefixes are in that string and the suffix beside it.
	runes := goUnitRunes.FindStringSubmatch(terminal)
	if runes == nil {
		t.Fatal("humanBytes no longer names its units from a rune string; this check needs rewriting")
	}
	if !strings.Contains(terminal, `iB"`) {
		t.Fatal("humanBytes no longer writes an iB suffix; this check needs rewriting")
	}
	var wanted []string
	for _, prefix := range runes[1] {
		wanted = append(wanted, string(prefix)+"iB")
	}

	list := jsUnitList.FindStringSubmatch(window)
	if list == nil {
		t.Fatal("the window's bytes() no longer names its units in a units array; this check needs rewriting")
	}
	var got []string
	for _, quoted := range strings.Split(list[1], ",") {
		trimmed := strings.TrimSpace(quoted)
		if trimmed == "" {
			continue
		}
		got = append(got, strings.Trim(trimmed, `'"`))
	}

	if len(got) != len(wanted) {
		t.Fatalf("the window names %d units %v and the command line names %d %v",
			len(got), got, len(wanted), wanted)
	}
	for i := range wanted {
		if got[i] != wanted[i] {
			t.Errorf("unit %d: the window says %q and the command line says %q", i, got[i], wanted[i])
		}
	}
}

// And both divide by the same thing, which is what makes the names above
// correct rather than merely matching.
func TestBothFrontEndsDivideByTheSameThing(t *testing.T) {
	terminal := readFile(t, filepath.Join("..", "..", "cmd", "worldledger", "manifest.go"))
	window := readFile(t, filepath.Join("assets", "app.js"))

	if !strings.Contains(terminal, "const unit = 1024") {
		t.Error("humanBytes no longer divides by a named 1024")
	}
	body := window[strings.Index(window, "function bytes("):]
	if end := strings.Index(body, "\n}"); end > 0 {
		body = body[:end]
	}
	if !strings.Contains(body, "1024") {
		t.Error("the window's bytes() no longer divides by 1024, so its iB units would be wrong")
	}
	if strings.Contains(body, "1000") {
		t.Error("the window's bytes() divides by 1000 while naming its units iB")
	}
}
