package app

import (
	"net"
	"net/http"
	"strings"
	"testing"
)

// The server is reachable by anything running on the machine, and by any page
// the person has open in a browser. Each check below is the only thing standing
// between one of those and the archive, so each is tested on its own: a suite
// that only tries the happy path would pass with every guard removed.

func newTestServer(t *testing.T) *Server {
	t.Helper()
	s, err := New()
	if err != nil {
		t.Fatal(err)
	}
	s.HandleFunc("/api/ping", func(w http.ResponseWriter, r *http.Request) {
		WriteJSON(w, http.StatusOK, map[string]string{"pong": "yes"})
	})
	s.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("page"))
	})
	go s.Serve()
	t.Cleanup(func() { s.Close() })
	return s
}

func do(t *testing.T, s *Server, method, path string, headers map[string]string) *http.Response {
	t.Helper()
	request, err := http.NewRequest(method, "http://"+s.Addr()+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	for name, value := range headers {
		if name == "Host" {
			request.Host = value
			continue
		}
		request.Header.Set(name, value)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { response.Body.Close() })
	return response
}

func TestTheListenerIsReachableOnlyFromThisMachine(t *testing.T) {
	s := newTestServer(t)
	host, _, err := net.SplitHostPort(s.Addr())
	if err != nil {
		t.Fatal(err)
	}
	if host != "127.0.0.1" {
		t.Fatalf("listening on %q; anything other than loopback is reachable from the network", host)
	}
}

func TestAnApiCallWithoutTheTokenIsRefused(t *testing.T) {
	s := newTestServer(t)
	response := do(t, s, http.MethodGet, "/api/ping", nil)
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status %d without a token, want %d", response.StatusCode, http.StatusUnauthorized)
	}
}

func TestAnApiCallWithTheWrongTokenIsRefused(t *testing.T) {
	s := newTestServer(t)
	response := do(t, s, http.MethodGet, "/api/ping",
		map[string]string{TokenHeader: strings.Repeat("0", len(s.Token()))})
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status %d with a wrong token, want %d", response.StatusCode, http.StatusUnauthorized)
	}
}

func TestAnApiCallWithTheTokenIsAllowed(t *testing.T) {
	s := newTestServer(t)
	response := do(t, s, http.MethodGet, "/api/ping", map[string]string{TokenHeader: s.Token()})
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status %d with the right token, want %d", response.StatusCode, http.StatusOK)
	}
}

// The token has to be in a header rather than in the query string. A browser
// will not attach a custom header to a cross-origin request without a preflight,
// which is refused; it will happily be navigated to a URL.
func TestTheTokenInTheQueryStringDoesNotOpenTheApi(t *testing.T) {
	s := newTestServer(t)
	response := do(t, s, http.MethodGet, "/api/ping?token="+s.Token(), nil)
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status %d for a token passed in the query string, want %d",
			response.StatusCode, http.StatusUnauthorized)
	}
}

// A name that resolves to 127.0.0.1 today can resolve somewhere else tomorrow,
// which is how a page in a browser comes to speak to a loopback service.
func TestARequestArrivingUnderAnotherNameIsRefused(t *testing.T) {
	s := newTestServer(t)
	_, port, err := net.SplitHostPort(s.Addr())
	if err != nil {
		t.Fatal(err)
	}
	response := do(t, s, http.MethodGet, "/api/ping", map[string]string{
		"Host":      "attacker.example:" + port,
		TokenHeader: s.Token(),
	})
	if response.StatusCode != http.StatusMisdirectedRequest {
		t.Fatalf("status %d for a request under another host, want %d",
			response.StatusCode, http.StatusMisdirectedRequest)
	}
}

func TestLocalhostByNameIsStillUs(t *testing.T) {
	s := newTestServer(t)
	_, port, err := net.SplitHostPort(s.Addr())
	if err != nil {
		t.Fatal(err)
	}
	response := do(t, s, http.MethodGet, "/api/ping", map[string]string{
		"Host":      "localhost:" + port,
		TokenHeader: s.Token(),
	})
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status %d for localhost by name, want %d", response.StatusCode, http.StatusOK)
	}
}

// Configuring cross-origin access would be answering a question that should not
// be asked. Refusing the preflight means the browser never makes the call.
func TestThePreflightIsRefusedRatherThanConfigured(t *testing.T) {
	s := newTestServer(t)
	response := do(t, s, http.MethodOptions, "/api/ping", nil)
	if response.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status %d for a preflight, want %d", response.StatusCode, http.StatusMethodNotAllowed)
	}
	if allow := response.Header.Get("Access-Control-Allow-Origin"); allow != "" {
		t.Errorf("the server offered cross-origin access: %q", allow)
	}
}

func TestARequestFromAnotherOriginIsRefused(t *testing.T) {
	s := newTestServer(t)
	response := do(t, s, http.MethodGet, "/api/ping", map[string]string{
		"Origin":    "http://attacker.example",
		TokenHeader: s.Token(),
	})
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("status %d for another origin, want %d", response.StatusCode, http.StatusForbidden)
	}
}

// The page itself is not behind the token, because the browser has to be able
// to load it before any script can present one.
func TestThePageLoadsWithoutTheToken(t *testing.T) {
	s := newTestServer(t)
	response := do(t, s, http.MethodGet, "/", nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status %d for the page, want %d", response.StatusCode, http.StatusOK)
	}
}

func TestTwoServersDoNotShareAToken(t *testing.T) {
	first, second := newTestServer(t), newTestServer(t)
	if first.Token() == second.Token() {
		t.Fatal("two sessions minted the same token")
	}
	if first.Addr() == second.Addr() {
		t.Fatal("two servers claimed the same port")
	}
	// A token is only good for the session that minted it.
	response := do(t, second, http.MethodGet, "/api/ping", map[string]string{TokenHeader: first.Token()})
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status %d for another session's token, want %d",
			response.StatusCode, http.StatusUnauthorized)
	}
}

// A failure the page cannot act on is a dead end, and this application is for
// people who will not read a log.
func TestEveryFailureCarriesSomethingToDoAboutIt(t *testing.T) {
	s := newTestServer(t)
	s.HandleFunc("/api/broken", func(w http.ResponseWriter, r *http.Request) {
		WriteFailure(w, http.StatusConflict, "the archive is busy", "wait for the import to finish, then try again")
	})
	response := do(t, s, http.MethodGet, "/api/broken", map[string]string{TokenHeader: s.Token()})
	if response.StatusCode != http.StatusConflict {
		t.Fatalf("status %d, want %d", response.StatusCode, http.StatusConflict)
	}
	var failure Failure
	if err := decode(t, response, &failure); err != nil {
		t.Fatal(err)
	}
	if failure.Problem == "" {
		t.Error("the failure does not say what went wrong")
	}
	if failure.Next == "" {
		t.Error("the failure does not say what to do about it")
	}
}
