package main

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// dispatchedCommands reads the command names out of run()'s switch.
//
// The alternative is a hand-kept list, which is the thing that goes stale: a
// command added to the switch and forgotten here would be a command with no
// usage and no --help, and the test meant to catch that would still pass.
func dispatchedCommands(t *testing.T) []string {
	t.Helper()
	source, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	body := string(source)
	start := strings.Index(body, "func run(args []string) error {")
	if start < 0 {
		t.Fatal("run() not found; this test reads it to find the command list")
	}
	end := strings.Index(body[start:], "\n}\n")
	if end < 0 {
		t.Fatal("could not find the end of run()")
	}
	matches := regexp.MustCompile(`case ("[a-z-]+"(?:, "[^"]+")*):`).
		FindAllStringSubmatch(body[start:start+end], -1)

	var names []string
	for _, match := range matches {
		for _, quoted := range strings.Split(match[1], ", ") {
			name := strings.Trim(quoted, `"`)
			// --version and -v are spellings of a command, not commands.
			if strings.HasPrefix(name, "-") {
				continue
			}
			names = append(names, name)
		}
	}
	if len(names) < 15 {
		t.Fatalf("only found %d commands in run(); the parser has drifted from the source", len(names))
	}
	return names
}

func TestEveryDispatchedCommandHasUsage(t *testing.T) {
	for _, name := range dispatchedCommands(t) {
		if _, ok := commandUsage[name]; !ok {
			t.Errorf("command %q is dispatched but has no entry in commandUsage, "+
				"so it answers neither --help nor a missing flag", name)
		}
	}
}

func TestEveryDispatchedCommandAnswersHelp(t *testing.T) {
	for _, name := range dispatchedCommands(t) {
		for _, spelling := range [][]string{
			{name, "--help"},
			{name, "-h"},
			{"help", name},
		} {
			got, ok := helpRequest(spelling)
			if !ok {
				t.Errorf("%v was not recognised as a request for help", spelling)
				continue
			}
			if got != name {
				t.Errorf("%v asked about %q, want %q", spelling, got, name)
			}
		}
	}
}

// Help goes to the writer it is given and reports no error, because a caller
// asking a question did not fail. main relies on this to exit zero.
func TestHelpIsNotAnError(t *testing.T) {
	var out strings.Builder
	if err := printHelp(&out, "export"); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out.String(), "usage: worldledger export ") {
		t.Fatalf("help for export was %q", out.String())
	}
}

// A command with subcommands lists them, because "policy <show|set|list>" tells
// a reader that three things exist and nothing about what they take.
func TestHelpForAGroupListsItsSubcommands(t *testing.T) {
	var out strings.Builder
	if err := printHelp(&out, "policy"); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"policy show", "policy set", "policy list"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("help for policy does not mention %q:\n%s", want, out.String())
		}
	}
}

func TestHelpForASubcommandIsAboutTheSubcommand(t *testing.T) {
	got, ok := helpRequest([]string{"policy", "set", "--help"})
	if !ok || got != "policy set" {
		t.Fatalf("helpRequest = %q, %v; want \"policy set\", true", got, ok)
	}
}

// An operand after "--" is an operand. A file named -h must not silently turn a
// command that would have done work into one that prints usage instead.
func TestAnOperandAfterDoubleDashIsNotAHelpRequest(t *testing.T) {
	if _, ok := helpRequest([]string{"receive", "--archive", "a", "--", "-h"}); ok {
		t.Fatal("an operand after -- was read as a request for help")
	}
}

func TestATypoSuggestsTheCommandItMeant(t *testing.T) {
	for typo, want := range map[string]string{
		"expot":   "export",
		"statu":   "status",
		"redct":   "redact",
		"veriy":   "verify",
		"iniy":    "init",
		"covrage": "coverage",
	} {
		err := unknownCommandError(typo)
		if err == nil || !strings.Contains(err.Error(), `did you mean "`+want+`"`) {
			t.Errorf("%q suggested %v; want %q", typo, err, want)
		}
	}
}

// Something that is not a typo of anything must not be answered with a guess:
// sending a reader off to read about the wrong command is worse than saying
// there is a list.
func TestSomethingUnlikeAnyCommandGetsNoGuess(t *testing.T) {
	err := unknownCommandError("zzzzzzzz")
	if err == nil || strings.Contains(err.Error(), "did you mean") {
		t.Fatalf("err = %v; want no suggestion", err)
	}
	if !strings.Contains(err.Error(), "worldledger help") {
		t.Fatalf("err = %v; want a pointer to the command list", err)
	}
}

// Every command and subcommand, through the routing and the printing together.
//
// The unit tests above check helpRequest and printHelp separately, and a sweep
// over the built binary found a case they both passed: "worldledger help
// --help" routed to a command named "--help", which is not one, and exited 1.
// Composing them here is what would have caught it.
func TestEverySpellingOfHelpForEveryCommandPrintsUsage(t *testing.T) {
	var names []string
	for name := range commandUsage {
		// "help --help" is about the tool rather than about a command, and
		// answering it with the whole listing is right. It has its own test.
		if name == "help" {
			continue
		}
		names = append(names, name)
	}
	for _, name := range names {
		args := append(strings.Split(name, " "), "--help")
		requested, ok := helpRequest(args)
		if !ok {
			t.Errorf("%v was not read as a request for help", args)
			continue
		}
		var out strings.Builder
		if err := printHelp(&out, requested); err != nil {
			t.Errorf("%v: %v", args, err)
			continue
		}
		if !strings.HasPrefix(out.String(), "usage: worldledger ") {
			t.Errorf("%v printed %q", args, out.String())
		}
	}
}

// The three spellings combine, and none of the combinations may be read as a
// command. Each of these has to end at the whole-tool usage.
func TestHelpAboutHelpIsNotAnUnknownCommand(t *testing.T) {
	for _, args := range [][]string{
		{"help"},
		{"--help"},
		{"-h"},
		{"help", "--help"},
		{"help", "-h"},
		{"--help", "help"},
	} {
		requested, ok := helpRequest(args)
		if !ok {
			t.Errorf("%v was not read as a request for help", args)
			continue
		}
		if requested != "" {
			t.Errorf("%v asked about %q; want the whole tool", args, requested)
			continue
		}
		var out strings.Builder
		if err := printHelp(&out, requested); err != nil {
			t.Errorf("%v: %v", args, err)
		}
	}
}

func TestUsageErrorUsesTheSharedText(t *testing.T) {
	err := usageError("export")
	if err == nil || err.Error() != "usage: worldledger "+commandUsage["export"] {
		t.Fatalf("usageError(export) = %v", err)
	}
}
