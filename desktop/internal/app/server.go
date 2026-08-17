// Package app is the desktop application's local server.
//
// The window is a shell around a page, and the page talks to this. Putting the
// logic behind HTTP rather than inside a widget toolkit is what lets the same
// application run in a native window and, when that window cannot be created,
// in the browser instead -- with nothing missing in the second case. A person
// whose machine lacks a web view runtime should get a working program, not an
// apology.
//
// Being reachable over a socket at all is a risk that has to be paid for. Any
// process on the machine can connect to a loopback port, and a page open in the
// user's browser can try to as well. Three things answer that, and all three
// are needed:
//
//   - the listener binds 127.0.0.1 only, so nothing off the machine can reach it
//   - every request must carry a token generated at startup and never written
//     to disk
//   - the token travels in a header, which a browser will not attach to a
//     cross-origin request without a preflight this server refuses
//
// The Host check is the fourth. A name that resolves to 127.0.0.1 today can
// resolve elsewhere tomorrow, which is how a page in a browser gets to speak to
// a loopback service it was never meant to reach.
package app

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"
)

// TokenHeader carries the startup token on every call under /api/.
const TokenHeader = "X-WorldLedger-Token"

// Server is the application's local endpoint.
type Server struct {
	token    string
	listener net.Listener
	mux      *http.ServeMux
	http     *http.Server
}

// New binds a loopback port chosen by the operating system and mints a token.
//
// The port is not fixed. A fixed port is one more thing that can already be in
// use on somebody's machine, and there is nothing for a second program to find
// here by knowing the number.
func New() (*Server, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("could not open a local port: %w", err)
	}

	token, err := mintToken()
	if err != nil {
		listener.Close()
		return nil, err
	}

	s := &Server{token: token, listener: listener, mux: http.NewServeMux()}
	s.http = &http.Server{
		Handler: s.guard(s.mux),
		// A local page cannot justify a slow request. These bound the damage a
		// stuck or hostile client can do rather than describing normal use.
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	return s, nil
}

func mintToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("could not generate a session token: %w", err)
	}
	return hex.EncodeToString(raw), nil
}

// Addr is the host:port the server is listening on.
func (s *Server) Addr() string { return s.listener.Addr().String() }

// Token is the value a caller must present. It exists only in memory: writing
// it down would outlive the session that it is meant to be scoped to.
func (s *Server) Token() string { return s.token }

// URL is the address to open, carrying the token so the first page load can
// pick it up. The page removes it from the address bar immediately, so it does
// not survive into history or a copied link.
func (s *Server) URL() string {
	return fmt.Sprintf("http://%s/?token=%s", s.Addr(), s.token)
}

// Handle registers a handler. Paths under /api/ are guarded; everything else is
// the page itself.
func (s *Server) Handle(pattern string, handler http.Handler) {
	s.mux.Handle(pattern, handler)
}

func (s *Server) HandleFunc(pattern string, handler func(http.ResponseWriter, *http.Request)) {
	s.mux.HandleFunc(pattern, handler)
}

// Serve runs until Close. It returns nil on a normal shutdown, because a server
// that was asked to stop did not fail.
func (s *Server) Serve() error {
	err := s.http.Serve(s.listener)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (s *Server) Close() error { return s.http.Close() }

// guard applies the checks that make a loopback listener safe to have open.
func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// A request whose Host is not the address we are listening on reached us
		// under some other name, which is the shape of a rebinding attempt.
		if !s.hostIsOurs(r.Host) {
			http.Error(w, "unexpected host", http.StatusMisdirectedRequest)
			return
		}
		// No cross-origin use is ever legitimate here, so the preflight that
		// would make one possible is refused rather than configured.
		if r.Method == http.MethodOptions {
			http.Error(w, "not allowed", http.StatusMethodNotAllowed)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" && !s.originIsOurs(origin) {
			http.Error(w, "not allowed", http.StatusForbidden)
			return
		}

		if strings.HasPrefix(r.URL.Path, "/api/") {
			// Compared in constant time. The comparison is cheap and the habit is
			// worth more than reasoning about whether this particular one leaks.
			presented := r.Header.Get(TokenHeader)
			if subtle.ConstantTimeCompare([]byte(presented), []byte(s.token)) != 1 {
				http.Error(w, "not authorised", http.StatusUnauthorized)
				return
			}
		}

		// The page is entirely local and pulls in nothing, so it can say so.
		w.Header().Set("Content-Security-Policy",
			"default-src 'self'; img-src 'self' data:; style-src 'self'; script-src 'self'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) hostIsOurs(host string) bool {
	if host == s.Addr() {
		return true
	}
	// A browser may present the address without the port when it is the default
	// one, which cannot happen here, but localhost is worth accepting by name.
	hostname, port, err := net.SplitHostPort(host)
	if err != nil {
		return false
	}
	_, ourPort, err := net.SplitHostPort(s.Addr())
	if err != nil || port != ourPort {
		return false
	}
	return hostname == "127.0.0.1" || hostname == "localhost" || hostname == "[::1]" || hostname == "::1"
}

func (s *Server) originIsOurs(origin string) bool {
	const prefix = "http://"
	if !strings.HasPrefix(origin, prefix) {
		return false
	}
	return s.hostIsOurs(strings.TrimPrefix(origin, prefix))
}

// WriteJSON is the one way a handler answers, so that a failure to encode
// cannot leave a half-written body that the page has to guess about.
func WriteJSON(w http.ResponseWriter, status int, payload any) {
	body, err := json.Marshal(payload)
	if err != nil {
		http.Error(w, "could not encode the response", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	w.Write(body)
}

// Failure is what the page receives when something did not work.
//
// Next is the field that matters. An error a person cannot act on is a dead
// end, and this application is for people who will not read a log, so every
// failure has to carry the thing to do about it. The command line settled this
// already: its empty-selection errors name the real servers rather than saying
// nothing was selected.
type Failure struct {
	Problem string `json:"problem"`
	Next    string `json:"next,omitempty"`
}

func WriteFailure(w http.ResponseWriter, status int, problem, next string) {
	WriteJSON(w, status, Failure{Problem: problem, Next: next})
}
