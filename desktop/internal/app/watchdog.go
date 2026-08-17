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

// Watchdog ends the program when the page stops reporting in.
type Watchdog struct {
	timeout time.Duration
	mu      sync.Mutex
	last    time.Time
	// started is false until the first report, so a browser that is slow to
	// open does not trip the timer before it has loaded.
	started bool
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

func (wd *Watchdog) beat() {
	wd.mu.Lock()
	defer wd.mu.Unlock()
	wd.last = time.Now()
	wd.started = true
}

func (wd *Watchdog) expired() bool {
	wd.mu.Lock()
	defer wd.mu.Unlock()
	if !wd.started || wd.held > 0 {
		return false
	}
	return time.Since(wd.last) > wd.timeout
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
