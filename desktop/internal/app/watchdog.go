package app

import (
	"net/http"
	"sync"
	"time"
)

// A window that is closed ends the program. A browser tab that is closed says
// nothing to anybody, and without this the application would sit in the process
// list forever -- which for the person this is built for means learning what
// Task Manager is.
//
// So the page says it is still there, and when it stops saying so the program
// stops. The interval is generous: a machine that pauses for ten seconds under
// load has not been abandoned, and quitting on somebody mid-import would be a
// worse failure than lingering.
//
// There are two silences, not one. A page that reported and then stopped has
// been closed. A page that never reported at all may never be coming, and until
// ExpectPage that case was waited on forever.

// Watchdog ends the program when the page stops reporting in.
type Watchdog struct {
	timeout time.Duration
	mu      sync.Mutex
	last    time.Time
	// started is false until the first report, so a browser that is slow to
	// open does not trip the timer before it has loaded.
	started bool
	// expectedBy is when to stop waiting for a page that has never reported.
	// Zero until somebody says one is on its way: a program that has not been
	// shown to anybody yet is not being ignored by anybody.
	expectedBy time.Time
	// held is a count of work in progress. An import that takes longer than the
	// timeout must not be killed by it.
	held int
}

func NewWatchdog(timeout time.Duration) *Watchdog {
	return &Watchdog{timeout: timeout, last: time.Now()}
}

// Mount registers the endpoint the page calls, and returns a channel that
// closes once the page has gone quiet for longer than the timeout.
func (wd *Watchdog) Mount(server *Server) <-chan struct{} {
	server.HandleFunc("/api/alive", func(w http.ResponseWriter, r *http.Request) {
		wd.beat()
		WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
	})

	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(wd.timeout / 4)
		defer ticker.Stop()
		for range ticker.C {
			if wd.expired() {
				return
			}
		}
	}()
	return done
}

// ExpectPage says a page has been asked for, and gives up on it if none
// arrives.
//
// Asking the system to open a browser succeeds as soon as the request has been
// handed over. Whether a browser then starts, and whether it reaches this
// server, is not something the answer says. When it does not, because there is
// no browser installed, or one that fails to start, or somebody who closes the
// tab before it loads, the program waits for a first report that is not coming.
//
// That state is the one this whole file exists to prevent, reached by the one
// route it did not cover. The Windows build is linked with -H=windowsgui so
// that starting it does not put a console behind the application, so what is
// left is a process with no window, no console and no page: nothing to close,
// and nothing to tell anybody it is there.
func (wd *Watchdog) ExpectPage(within time.Duration) {
	wd.mu.Lock()
	defer wd.mu.Unlock()
	// A page that has already reported is being watched by the ordinary timer,
	// and a deadline for its arrival would only be a second way to kill it.
	if wd.started {
		return
	}
	wd.expectedBy = time.Now().Add(within)
}

// NeverAppeared reports whether this is ending because no page ever loaded,
// rather than because one was closed. The two are the same silence from in
// here, and only one of them is somebody's program not working.
func (wd *Watchdog) NeverAppeared() bool {
	wd.mu.Lock()
	defer wd.mu.Unlock()
	return !wd.started
}

func (wd *Watchdog) beat() {
	wd.mu.Lock()
	defer wd.mu.Unlock()
	wd.last = time.Now()
	wd.started = true
}

func (wd *Watchdog) expired() bool {
	wd.mu.Lock()
	defer wd.mu.Unlock()
	if wd.held > 0 {
		return false
	}
	if wd.started {
		return time.Since(wd.last) > wd.timeout
	}
	if wd.expectedBy.IsZero() {
		return false
	}
	return time.Now().After(wd.expectedBy)
}

// Hold marks work in progress that must outlive a quiet page, and returns the
// function that releases it. An import of two hundred bundles can take longer
// than the timeout, and a page busy waiting for it is not a page nobody is
// looking at.
func (wd *Watchdog) Hold() func() {
	wd.mu.Lock()
	wd.held++
	wd.mu.Unlock()
	return func() {
		wd.mu.Lock()
		wd.held--
		wd.last = time.Now()
		wd.mu.Unlock()
	}
}
