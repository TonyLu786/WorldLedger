package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

// These guard against a class of damage that shipped once and was found by
// reading output rather than by any test.
//
// A source file was edited through a tool that decoded UTF-8 as a legacy
// codepage and wrote the result back. An em dash became a CJK character
// followed by a literal question mark, and because the file still parsed and
// every test still passed, the corrupted bytes reached a published binary and
// printed as the first line of its help output.
//
// Two rules follow. Terminal output should not depend on the reader's console
// codepage, which on Windows is frequently not UTF-8, so Go sources hold ASCII
// only. And any file that is meant to be text must still be text, because the
// failure mode above leaves a file that looks fine to a compiler.

// nonTextExtensions are the tracked files that are not expected to decode as
// text at all.
var nonTextExtensions = map[string]bool{
	".jar": true, ".bin": true, ".png": true, ".gz": true, ".zip": true,
	".exe": true, ".class": true,
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("expected the module root at %s: %v", root, err)
	}
	return root
}

func walkRepository(t *testing.T, visit func(path string, data []byte)) {
	t.Helper()
	root := repositoryRoot(t)
	skip := map[string]bool{
		".git": true, "build": true, "bin": true, "dist": true, ".gradle": true, "validation": true,
	}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if skip[entry.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		visit(path, data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// Go sources are ASCII. The prose in docs may use whatever typography it likes;
// what a program prints to a terminal may not.
func TestGoSourcesAreASCII(t *testing.T) {
	var problems []string
	walkRepository(t, func(path string, data []byte) {
		if filepath.Ext(path) != ".go" {
			return
		}
		line := 1
		for _, b := range data {
			if b == '\n' {
				line++
				continue
			}
			if b > 0x7f {
				problems = append(problems, filepath.ToSlash(path)+":"+strconv.Itoa(line))
				return
			}
		}
	})
	if len(problems) != 0 {
		t.Fatalf("Go sources must hold ASCII only, because what they print must not depend on the "+
			"reader's console codepage. Non-ASCII found at:\n  %s", strings.Join(problems, "\n  "))
	}
}

// A file that was decoded as the wrong codepage and written back leaves marks.
// U+FFFD is what a decoder substitutes when it gives up, and U+9225 is what a
// UTF-8 em dash becomes when it is read as GBK and re-encoded, which is the
// exact damage that shipped.
func TestTextFilesAreNotDoubleEncoded(t *testing.T) {
	markers := map[string]string{
		"\ufffd": "a replacement character, left by a decoder that gave up",
		"\u9225": "the residue of UTF-8 read as a legacy codepage",
	}
	var problems []string
	walkRepository(t, func(path string, data []byte) {
		if nonTextExtensions[strings.ToLower(filepath.Ext(path))] {
			return
		}
		if !utf8.Valid(data) {
			problems = append(problems, filepath.ToSlash(path)+": not valid UTF-8")
			return
		}
		for marker, why := range markers {
			if strings.Contains(string(data), marker) {
				problems = append(problems, filepath.ToSlash(path)+": contains "+why)
			}
		}
	})
	if len(problems) != 0 {
		t.Fatalf("text files show signs of an encoding round trip:\n  %s", strings.Join(problems, "\n  "))
	}
}
