package ui

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// The command line and the window are two front ends over one core, and they
// decide the same things twice.
//
// Every one of those decisions is a place they can drift apart, and drift here
// does not look like a bug in either of them: it looks like the window doing
// something the terminal does not, which is exactly what somebody would report
// as "it works in the app but not on the command line". Two were already known
// when this was written. Nothing was counting them.
//
// This lives in the desktop module because it is the only one that can see
// both. The core module cannot import the desktop, and the desktop imports the
// core, so a check that reads both has one possible home.
//
// It reads source rather than running anything, which is coarse and cheap and
// catches the thing that actually happens: one side gains a field or changes a
// default and the other is not touched.

// sharedDecision is one type both front ends fill in, and where each does it.
type sharedDecision struct {
	// literal is the composite literal to look for, as written.
	literal string
	// terminal and window are the files that construct it.
	terminal string
	window   string
	// agreed names the fields that must carry the same expression on both
	// sides. A field left out of this is one the two are allowed to differ on,
	// and every one of those needs a reason recorded beside it.
	agreed []string
	// differs names fields the two deliberately set differently, with why.
	// Stating it is the point: a difference that is written down is a decision,
	// and one that is not is an accident waiting to be found by somebody else.
	differs map[string]string
	// defaultedBy maps a field to the terminal flag that supplies it. The
	// terminal writes a variable where the window writes a value, so comparing
	// the source text would compare a name against a number. What the two have
	// in common is the default, and the default is the decision: one side can
	// be told otherwise and the other cannot, which is a difference in reach
	// rather than in what either of them believes.
	defaultedBy map[string]string
}

func sharedDecisions() []sharedDecision {
	return []sharedDecision{{
		literal:  "anvil.ExportRequest",
		terminal: filepath.Join("..", "..", "cmd", "worldledger", "export.go"),
		window:   filepath.Join("..", "internal", "api", "exporting.go"),
		agreed:   []string{"DataVersion"},
		defaultedBy: map[string]string{
			"DataVersion": "data-version",
		},
		differs: map[string]string{
			"WorldDir": "each front end has its own way of being told which world, " +
				"and neither is a default the other could share",
			"Dimension": "the same",
			"Overwrite": "the window offers a world it listed itself, so it knows the " +
				"file is there and that replacing chunks in it is what was asked for. " +
				"The terminal is handed a path it cannot make that assumption about, " +
				"so it refuses until it is told. This is the difference being a " +
				"decision rather than an oversight",
		},
	}}
}

// fieldsOfLiteral reads one composite literal out of a file and returns each
// field with the expression assigned to it, as written.
func fieldsOfLiteral(t *testing.T, file, literal string) map[string]string {
	t.Helper()
	fileSet := token.NewFileSet()
	parsed, err := parser.ParseFile(fileSet, file, nil, 0)
	if err != nil {
		t.Fatalf("%s: %v", file, err)
	}

	found := map[string]string{}
	seen := false
	ast.Inspect(parsed, func(node ast.Node) bool {
		composite, ok := node.(*ast.CompositeLit)
		if !ok {
			return true
		}
		selector, ok := composite.Type.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pkg, ok := selector.X.(*ast.Ident)
		if !ok || pkg.Name+"."+selector.Sel.Name != literal {
			return true
		}
		seen = true
		for _, element := range composite.Elts {
			pair, ok := element.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			key, ok := pair.Key.(*ast.Ident)
			if !ok {
				continue
			}
			start := fileSet.Position(pair.Value.Pos()).Offset
			end := fileSet.Position(pair.Value.End()).Offset
			source := readFile(t, file)
			found[key.Name] = strings.TrimSpace(source[start:end])
		}
		return true
	})
	if !seen {
		t.Fatalf("%s no longer constructs %s, so this check is watching nothing", file, literal)
	}
	return found
}

// flagDefault reads the default a flag is declared with, as written.
//
// It is the second argument of fs.String/Int/Bool and so on, which is where a
// terminal keeps the answer it uses when nobody says otherwise. That answer,
// and not the variable it lands in, is what the window can be compared against.
func flagDefault(t *testing.T, file, flagName string) (string, bool) {
	t.Helper()
	fileSet := token.NewFileSet()
	parsed, err := parser.ParseFile(fileSet, file, nil, 0)
	if err != nil {
		t.Fatalf("%s: %v", file, err)
	}
	source := readFile(t, file)

	found := ""
	ok := false
	ast.Inspect(parsed, func(node ast.Node) bool {
		call, isCall := node.(*ast.CallExpr)
		if !isCall || len(call.Args) < 2 {
			return true
		}
		selector, isSelector := call.Fun.(*ast.SelectorExpr)
		if !isSelector || !flagDeclarators[selector.Sel.Name] {
			return true
		}
		name, isLiteral := call.Args[0].(*ast.BasicLit)
		if !isLiteral || name.Kind != token.STRING {
			return true
		}
		declared, err := strconv.Unquote(name.Value)
		if err != nil || declared != flagName {
			return true
		}
		start := fileSet.Position(call.Args[1].Pos()).Offset
		end := fileSet.Position(call.Args[1].End()).Offset
		found = strings.TrimSpace(source[start:end])
		ok = true
		return true
	})
	return found, ok
}

var flagDeclarators = map[string]bool{
	"String": true, "Int": true, "Int64": true, "Bool": true,
	"Duration": true, "Float64": true, "Uint": true, "Uint64": true,
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return string(data)
}

// TestBothFrontEndsFillInTheSameDecisions is the counting this never had.
//
// Every field of a shared request has to be accounted for on both sides: either
// named as one they agree on, in which case the expressions must match, or
// named as one they differ on with the reason written down. A field that is
// neither is the case this exists to catch, because that is what drift looks
// like on the day it appears.
func TestBothFrontEndsFillInTheSameDecisions(t *testing.T) {
	for _, decision := range sharedDecisions() {
		terminal := fieldsOfLiteral(t, decision.terminal, decision.literal)
		window := fieldsOfLiteral(t, decision.window, decision.literal)

		accounted := map[string]bool{}
		for _, field := range decision.agreed {
			accounted[field] = true
			left, inTerminal := terminal[field]
			right, inWindow := window[field]
			if flagName, viaFlag := decision.defaultedBy[field]; viaFlag {
				declared, found := flagDefault(t, decision.terminal, flagName)
				if !found {
					t.Errorf("%s.%s is said to come from --%s, and the terminal declares no such flag",
						decision.literal, field, flagName)
					continue
				}
				left, inTerminal = declared, true
			}
			switch {
			case !inTerminal:
				t.Errorf("%s: the terminal no longer sets %s", decision.literal, field)
			case !inWindow:
				t.Errorf("%s: the window no longer sets %s", decision.literal, field)
			case left != right:
				t.Errorf("%s.%s has drifted: the terminal writes %s and the window writes %s",
					decision.literal, field, left, right)
			}
		}
		for field, why := range decision.differs {
			accounted[field] = true
			if strings.TrimSpace(why) == "" {
				t.Errorf("%s.%s is allowed to differ with no reason recorded", decision.literal, field)
			}
		}

		var unaccounted []string
		for field := range terminal {
			if !accounted[field] {
				unaccounted = append(unaccounted, field)
			}
		}
		for field := range window {
			if !accounted[field] && terminal[field] == "" {
				unaccounted = append(unaccounted, field)
			}
		}
		sort.Strings(unaccounted)
		for _, field := range unaccounted {
			t.Errorf("%s.%s is set by a front end and is neither agreed nor recorded as differing. "+
				"Add it to agreed if both should say the same thing, or to differs with why they should not",
				decision.literal, field)
		}
	}
}

// The other drift that was already known, and which is not a field of anything.
//
// verify buckets a chunk's timeline at ten seconds; epoch decides conflict from
// change at thirty, with no flag. They are different mechanisms and they answer
// the same question for whoever reads the output, so a chunk one clears is a
// chunk the other flags. Neither number was written down anywhere, which is how
// it survived.
//
// Reconciling them is a decision about what verify is for and is not taken
// here. What is taken here is that neither number can move again without
// somebody seeing this.
func TestTheTwoComparisonWindowsAreBothWrittenDown(t *testing.T) {
	const (
		verifyDefault = "10*time.Second"
		epochFixed    = "30 * time.Second"
	)

	declared, found := flagDefault(t, filepath.Join("..", "..", "cmd", "worldledger", "main.go"), "window")
	if !found {
		t.Fatal("verify no longer declares --window")
	}
	if declared != verifyDefault {
		t.Errorf("verify --window now defaults to %s, not %s. If that was to reconcile it with "+
			"epoch, this check and the note in architecture.md are what need updating with it",
			declared, verifyDefault)
	}

	epochSource := readFile(t, filepath.Join("..", "..", "internal", "epoch", "epoch.go"))
	if !strings.Contains(epochSource, "DefaultSimultaneityWindow = "+epochFixed) {
		t.Errorf("epoch.DefaultSimultaneityWindow is no longer %s, and verify still uses %s",
			epochFixed, verifyDefault)
	}

	// And the document that describes the split has to still describe it.
	architecture := readFile(t, filepath.Join("..", "..", "docs", "architecture.md"))
	for _, needed := range []string{"ten seconds by default", "thirty seconds"} {
		if !strings.Contains(architecture, needed) {
			t.Errorf("architecture.md no longer states %q, so the two windows are unwritten again", needed)
		}
	}
}
