package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/worldledger/worldledger-mc/internal/archive"
	"github.com/worldledger/worldledger-mc/internal/model"
)

// An archive stores the namespaced dimension a client reports. "overworld" and
// "minecraft:overworld" are different strings, and inspect and verify used to
// default to the first, so accepting their defaults on a real archive was
// guaranteed to find nothing. A default that cannot match is worse than a
// required flag: it answers confidently and wrongly.
func TestTheDefaultDimensionIsOneAnArchiveActuallyStores(t *testing.T) {
	if defaultDimension != "minecraft:overworld" {
		t.Fatalf("defaultDimension = %q", defaultDimension)
	}
	if model.NormalizeToken(defaultDimension) != defaultDimension {
		t.Fatalf("%q is not already normalized, so it will not match a stored key", defaultDimension)
	}
}

// Every command that defaults the dimension has to default it the same way, or
// the same world reads differently depending on which command asked.
func TestEveryCommandDefaultsTheDimensionIdentically(t *testing.T) {
	pattern := regexp.MustCompile(`String\("dimension", ([^,]+),`)
	sources, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, path := range sources {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, match := range pattern.FindAllStringSubmatch(string(body), -1) {
			value := match[1]
			// redact deliberately defaults to empty, meaning every dimension.
			if value == `""` {
				continue
			}
			found++
			if value != "defaultDimension" {
				t.Errorf(`%s: dimension defaults to %s; use defaultDimension so every command agrees`, path, value)
			}
		}
	}
	if found < 4 {
		t.Fatalf("only found %d dimension defaults; the pattern has drifted", found)
	}
}

// The message for a directory that is not an archive is the first thing a new
// user sees when they mistype a path or forget init. It used to name VERSION,
// an internal file nobody was told about.
func TestOpeningSomethingThatIsNotAnArchiveSaysWhatToRun(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "not-here")
	_, err := archive.Open(missing)
	if err == nil {
		t.Fatal("opening a directory that does not exist succeeded")
	}
	message := err.Error()
	if strings.Contains(message, "VERSION") {
		t.Errorf("the message names an internal file: %s", message)
	}
	if !strings.Contains(message, "worldledger init") {
		t.Errorf("the message does not say how to fix it: %s", message)
	}
	if !strings.Contains(message, missing) {
		t.Errorf("the message does not name the directory: %s", message)
	}
}

// A directory that exists but holds something else has the same fix, and has to
// be told apart from a genuine read failure.
func TestAnEmptyDirectoryIsReportedAsNotAnArchive(t *testing.T) {
	empty := t.TempDir()
	_, err := archive.Open(empty)
	if err == nil {
		t.Fatal("opening an empty directory succeeded")
	}
	if !strings.Contains(err.Error(), "worldledger init") {
		t.Errorf("err = %v; want the init hint", err)
	}
}

func TestAnInitialisedArchiveStillOpens(t *testing.T) {
	root := filepath.Join(t.TempDir(), "archive")
	if _, err := archive.Init(root); err != nil {
		t.Fatal(err)
	}
	if _, err := archive.Open(root); err != nil {
		t.Fatalf("a freshly initialised archive did not open: %v", err)
	}
}
