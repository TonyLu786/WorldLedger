package ui

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/worldledger/worldledger-mc/desktop/internal/api"
	"github.com/worldledger/worldledger-mc/desktop/internal/health"
)

// The page and the API are one program written in two languages, and nothing
// held the two halves together.
//
// Every defect a walk through this application turned up lived on this seam and
// none of them failed anything. /api/uninstall was registered, was named in its
// own refusal message as "the button on the set up screen", and was called by
// nothing -- so the promise the install dialog made about being undoable had no
// way to be kept. The declaration screen read status.declared_by, a field the
// status has never had, so the default it claimed to offer was always empty.
// Both are the same shape of mistake: one side changed and the other did not,
// and a program that renders is not a program that agrees with itself.
//
// So the page is read as text and checked against the Go it talks to. This is
// coarser than running it and much cheaper than not checking at all, and it
// catches exactly the class of thing that got through.

func pageSource(t *testing.T) string {
	t.Helper()
	source, err := assets.ReadFile("assets/app.js")
	if err != nil {
		t.Fatal(err)
	}
	return string(source)
}

// callsFromPage finds every /api/... path the page asks for. Paths are written
// as literals on purpose -- a path assembled from pieces is one nothing can
// check -- and the query string is dropped because routing does not see it.
func callsFromPage(t *testing.T) map[string]bool {
	t.Helper()
	found := map[string]bool{}
	for _, match := range regexp.MustCompile(`'(/api/[a-z-]+)`).FindAllStringSubmatch(pageSource(t), -1) {
		found[match[1]] = true
	}
	if len(found) == 0 {
		t.Fatal("no API calls were found in the page, so this test is checking nothing")
	}
	return found
}

// routesFromMount reads the handler paths out of api.Mount's source. Calling
// Mount for real would need a server and would answer a different question:
// what is registered, not what the code says it registers.
func routesFromMount(t *testing.T) map[string]bool {
	t.Helper()
	fileSet := token.NewFileSet()
	parsed, err := parser.ParseFile(fileSet, "../internal/api/api.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}

	routes := map[string]bool{}
	ast.Inspect(parsed, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || len(call.Args) == 0 {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != "HandleFunc" {
			return true
		}
		literal, ok := call.Args[0].(*ast.BasicLit)
		if !ok || literal.Kind != token.STRING {
			return true
		}
		path, err := strconv.Unquote(literal.Value)
		if err == nil && strings.HasPrefix(path, "/api/") {
			routes[path] = true
		}
		return true
	})
	if len(routes) == 0 {
		t.Fatal("no routes were found in api.Mount, so this test is checking nothing")
	}
	return routes
}

// alwaysMounted are answered outside api.Mount, so the page may call them and
// no route in that file will name them.
var alwaysMounted = map[string]bool{"/api/alive": true}

func TestEveryPathThePageAsksForIsAnswered(t *testing.T) {
	routes := routesFromMount(t)
	var missing []string
	for path := range callsFromPage(t) {
		if !routes[path] && !alwaysMounted[path] {
			missing = append(missing, path)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("the page calls %v, which nothing answers", missing)
	}
}

// The half that actually bit. An endpoint nobody calls is not dead weight when
// the application has told somebody it exists: the install dialog offers a
// Remove, and for a while the only way to reach one was to know the endpoint.
func TestEveryEndpointIsReachableFromThePage(t *testing.T) {
	calls := callsFromPage(t)
	var unreachable []string
	for path := range routesFromMount(t) {
		if !calls[path] {
			unreachable = append(unreachable, path)
		}
	}
	sort.Strings(unreachable)
	if len(unreachable) > 0 {
		t.Errorf("nothing on the page reaches %v; either the page should offer it or it should go", unreachable)
	}
}

// fieldsRead finds what the page expects to be on an object it was handed. The
// receiver names are the ones the page uses for a status, which is the response
// every screen depends on and the one that grew a field the page was reading
// before it existed.
// handlesRoute reports whether one file registers one path.
func handlesRoute(t *testing.T, file, path string) bool {
	t.Helper()
	fileSet := token.NewFileSet()
	parsed, err := parser.ParseFile(fileSet, file, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	ast.Inspect(parsed, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || len(call.Args) == 0 {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != "HandleFunc" {
			return true
		}
		literal, ok := call.Args[0].(*ast.BasicLit)
		if !ok || literal.Kind != token.STRING {
			return true
		}
		if value, err := strconv.Unquote(literal.Value); err == nil && value == path {
			found = true
		}
		return true
	})
	return found
}

func fieldsRead(source string, receivers ...string) map[string]bool {
	found := map[string]bool{}
	for _, receiver := range receivers {
		pattern := regexp.MustCompile(regexp.QuoteMeta(receiver) + `\.([a-z_][a-z0-9_]*)`)
		for _, match := range pattern.FindAllStringSubmatch(source, -1) {
			found[match[1]] = true
		}
	}
	return found
}

// jsonNames is what a value actually serialises as, taken from the tags rather
// than from the field names, because the tags are what the page sees.
func jsonNames(value any) map[string]bool {
	names := map[string]bool{}
	structure := reflect.TypeOf(value)
	for i := 0; i < structure.NumField(); i++ {
		tag := structure.Field(i).Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		names[strings.Split(tag, ",")[0]] = true
	}
	return names
}

func TestThePageOnlyReadsFieldsTheStatusHas(t *testing.T) {
	has := jsonNames(api.Status{})
	for name := range fieldsRead(pageSource(t), "status", "lastStatus") {
		if !has[name] {
			t.Errorf("the page reads status.%s, which the status does not have", name)
		}
	}

	// The check is only worth anything if it is looking at something. These are
	// the fields the screens are built on; if the page stops reading them the
	// screens have changed shape and this test should be revisited with them.
	for _, expected := range []string{"servers", "spool", "observations", "capturing", "contributor", "next"} {
		if !fieldsRead(pageSource(t), "status", "lastStatus")[expected] {
			t.Errorf("the page no longer reads status.%s; this test may be checking nothing", expected)
		}
	}
}

func TestThePageOnlyReadsSpoolFieldsThatExist(t *testing.T) {
	has := jsonNames(api.SpoolState{})
	for name := range fieldsRead(pageSource(t), "status.spool") {
		if !has[name] {
			t.Errorf("the page reads status.spool.%s, which the spool state does not have", name)
		}
	}
}

// The two overlays are deliberately one thing wearing two ids, and something
// has to keep them that way.
//
// It is not tidiness. The native window is expensive to exercise -- doing it
// takes over somebody's screen -- so what has been seen of the notice rendering
// and answering there is what stands behind the confirmation sheet as well.
// That transfer is only honest while the two are the same element with the same
// class, which is a fact a test can hold rather than an argument that quietly
// stops being true.
func TestTheTwoOverlaysAreTheSameThing(t *testing.T) {
	page, err := assets.ReadFile("assets/index.html")
	if err != nil {
		t.Fatal(err)
	}
	markup := string(page)
	for _, want := range []string{
		`<div class="notice" id="notice"`,
		`<div class="notice" id="ask"`,
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("the overlays no longer share their outer class: %q is not in the page", want)
		}
	}
	if sheets := strings.Count(markup, `class="notice-sheet"`); sheets != 2 {
		t.Errorf("%d element(s) use the sheet class, want the notice and the confirmation", sheets)
	}
}

// The notice is the one response whose content is the point rather than a
// number, so what it carries is checked rather than only that it is answered.
func TestTheNoticeSaysTheThingsItExistsToSay(t *testing.T) {
	raw, err := json.Marshal(struct {
		Paragraphs any `json:"paragraphs"`
	}{api.NoticeText()})
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ToLower(string(raw))
	for _, must := range []string{"responsib", "nothing is sent anywhere", "no warranty", "permission"} {
		if !strings.Contains(text, must) {
			t.Errorf("the notice no longer mentions %q", must)
		}
	}
	if len(api.NoticeText()) < 3 {
		t.Errorf("the notice is down to %d part(s)", len(api.NoticeText()))
	}
}

// The page now says three things on its own, rather than rendering whatever the
// other side supplied. That is a departure from the rule this file exists to
// hold, so what it depends on is held here instead.
//
// It was arrived at by killing the process with the page open: the whole screen
// became the words "Failed to fetch", because every sentence in this
// application is minted in Go and the one failure that is not had nothing to
// render. These check the parts of that fix which can silently stop working.

func TestThePageCanSayTheApplicationHasStopped(t *testing.T) {
	page := pageSource(t)
	markup, err := assets.ReadFile("assets/index.html")
	if err != nil {
		t.Fatal(err)
	}

	// The banner the heartbeat raises, and the element it raises it into. A
	// rename on either side puts the page back to failing silently.
	if !strings.Contains(string(markup), `id="halted"`) {
		t.Error("the page has no element for the application having stopped")
	}
	for _, needed := range []string{"getElementById('halted')", "function running(", "function stopped("} {
		if !strings.Contains(page, needed) {
			t.Errorf("app.js no longer has %s, so nothing raises or clears that banner", needed)
		}
	}

	// A failed fetch has to become a sentence rather than reach the screen as
	// whatever the browser called it.
	if !strings.Contains(page, "function unreachable(") {
		t.Error("app.js no longer classifies a failed fetch")
	}
	if !strings.Contains(page, "throw unreachable(err)") {
		t.Error("call() no longer routes a failed fetch through unreachable()")
	}
}

// The heartbeat is what makes the page notice on its own, and it is also what
// keeps the application alive. Pointing it at an endpoint that does not exist
// would end the program every time somebody left the page open.
func TestTheHeartbeatPointsAtAnEndpointThatExists(t *testing.T) {
	page := pageSource(t)
	if !strings.Contains(page, "call('/api/alive')") {
		t.Fatal("the page no longer reports in on /api/alive")
	}
	// Checked against the file that actually registers it rather than against
	// the allow-list above, which exists precisely because this one route is
	// mounted somewhere else and therefore was not being checked at all.
	if !handlesRoute(t, "../internal/app/watchdog.go", "/api/alive") {
		t.Error("the watchdog no longer registers /api/alive, so every page would be given up on")
	}
}

// Which WorldLedger this is, and which Minecraft it was made for, are the two
// facts somebody needs in order to tell a setup problem from an out-of-date
// program. A window has no --version, so if these stop reaching the page there
// is nowhere else to read them.
func TestTheReportCarriesWhichBuildThisIs(t *testing.T) {
	carried := jsonNames(health.Report{})
	for _, field := range []string{"build", "supports"} {
		if !carried[field] {
			t.Errorf("health.Report no longer carries %q", field)
		}
	}
}
