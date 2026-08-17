package app

import (
	"net/http"
	"testing"
	"time"
)

// The watchdog exists so a closed browser tab does not leave a program running
// that its owner has no idea how to stop. It also has to be the case that it
// never stops one that is doing something, because being killed part way
// through an import is a worse outcome than lingering.

func TestNothingHappensBeforeThePageHasEverReportedIn(t *testing.T) {
	// A browser can take several seconds to open. Starting the clock before the
	// page has loaded would quit while somebody was still waiting for it.
	wd := NewWatchdog(20 * time.Millisecond)
	time.Sleep(60 * time.Millisecond)
	if wd.expired() {
		t.Fatal("the timer ran before the page had ever loaded")
	}
}

func TestAQuietPageEndsIt(t *testing.T) {
	wd := NewWatchdog(20 * time.Millisecond)
	wd.beat()
	time.Sleep(60 * time.Millisecond)
	if !wd.expired() {
		t.Fatal("a page that stopped reporting did not end the program")
	}
}

func TestAPageThatKeepsReportingIsLeftAlone(t *testing.T) {
	wd := NewWatchdog(60 * time.Millisecond)
	for i := 0; i < 6; i++ {
		wd.beat()
		time.Sleep(15 * time.Millisecond)
		if wd.expired() {
			t.Fatalf("a page reporting every 15ms was given up on after %d beats", i+1)
		}
	}
}

// The case that matters most. An import of two hundred bundles can take longer
// than the watchdog's patience, and the page waiting for it makes no calls in
// the meantime.
func TestWorkInProgressIsNotGivenUpOnHoweverLongItTakes(t *testing.T) {
	wd := NewWatchdog(20 * time.Millisecond)
	wd.beat()

	release := wd.Hold()
	time.Sleep(80 * time.Millisecond)
	if wd.expired() {
		t.Fatal("the program was ended while work was in progress")
	}

	// Releasing counts as activity, so finishing a long import does not
	// immediately trip a timer that has been held off for minutes.
	release()
	if wd.expired() {
		t.Fatal("finishing a long piece of work immediately ended the program")
	}
	time.Sleep(60 * time.Millisecond)
	if !wd.expired() {
		t.Fatal("after the work finished and the page went quiet, it did not end")
	}
}

func TestHoldsNest(t *testing.T) {
	wd := NewWatchdog(20 * time.Millisecond)
	wd.beat()
	first, second := wd.Hold(), wd.Hold()
	first()
	time.Sleep(60 * time.Millisecond)
	if wd.expired() {
		t.Fatal("one of two pieces of work finishing was treated as both finishing")
	}
	second()
	time.Sleep(60 * time.Millisecond)
	if !wd.expired() {
		t.Fatal("with all work finished and the page quiet, it did not end")
	}
}

func TestReportingInGoesThroughTheServerLikeEverythingElse(t *testing.T) {
	s := newTestServer(t)
	wd := NewWatchdog(time.Minute)
	wd.Mount(s)

	// Behind the token, because an endpoint that keeps the program alive is one
	// any other process on the machine could otherwise hold open.
	if response := do(t, s, http.MethodGet, "/api/alive", nil); response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status %d without a token, want %d", response.StatusCode, http.StatusUnauthorized)
	}
	if response := do(t, s, http.MethodGet, "/api/alive",
		map[string]string{TokenHeader: s.Token()}); response.StatusCode != http.StatusOK {
		t.Fatalf("status %d with the token, want %d", response.StatusCode, http.StatusOK)
	}
}
