// Command dfurenames extracts Mojang's own rename tables from the DataFixerUpper
// definitions compiled into a Minecraft jar.
//
//	dfurenames --jar <deobf-common.jar> --javap <javap> --out profiles/renames-26.2.json
//
// The jar must be Mojang-mapped: the tool reads class and method names, so an
// obfuscated jar yields nothing.
//
// Renames are recorded in Mojang's direction, old name to new name, together
// with the data version whose schema introduced them. Reversing them for an
// export to an older release is a separate step, because a reversal is only
// well defined where the forward mapping is injective.
//
// Coverage is reported rather than assumed. A rename built by a lambda instead
// of a rename table cannot be read mechanically, and the output names every
// fixer that was seen but not extracted.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const Schema = "worldledger.datafix-renames/v1"

type Rename struct {
	DataVersion int32  `json:"data_version"`
	From        string `json:"from"`
	To          string `json:"to"`
}

type Coverage struct {
	BlockFixers    int      `json:"block_fixers"`
	BlockExtracted int      `json:"block_fixers_extracted"`
	ItemFixers     int      `json:"item_fixers"`
	ItemExtracted  int      `json:"item_fixers_extracted"`
	Unextracted    []string `json:"unextracted,omitempty"`
}

type Tables struct {
	Schema   string   `json:"schema"`
	Source   string   `json:"source"`
	Coverage Coverage `json:"coverage"`
	Blocks   []Rename `json:"blocks"`
	Items    []Rename `json:"items,omitempty"`
}

var (
	stringConstant  = regexp.MustCompile(`^\s*\d+:\s+ldc(?:_w)?\s+#\d+\s+//\s+String\s(.*)$`)
	intConstant     = regexp.MustCompile(`^\s*\d+:\s+(?:sipush|bipush)\s+(-?\d+)\s*$`)
	intFromPool     = regexp.MustCompile(`^\s*\d+:\s+ldc(?:_w)?\s+#\d+\s+//\s+int\s+(-?\d+)\s*$`)
	invocation      = regexp.MustCompile(`^\s*\d+:\s+invoke\w+\s+#[\d,\s]+//\s+(?:Interface)?Method\s+(\S+)`)
	staticFieldRead = regexp.MustCompile(`^\s*\d+:\s+getstatic\s+#\d+\s+//\s+Field\s+(\S+)`)
	storeLocal      = regexp.MustCompile(`^\s*\d+:\s+astore(?:_(\d)|\s+(\d+))\s*$`)
	loadLocal       = regexp.MustCompile(`^\s*\d+:\s+aload(?:_(\d)|\s+(\d+))\s*$`)
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run() error {
	const usage = "usage: dfurenames --jar <jar> --source <release> --out <file.json> [--javap <path>]"
	// The flag package would otherwise answer with the binary's path, which under
	// go run is a temporary build directory, and a bare list of flags.
	fs := flag.NewFlagSet("dfurenames", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	jar := fs.String("jar", "", "Mojang-mapped Minecraft jar")
	javap := fs.String("javap", "javap", "javap executable")
	source := fs.String("source", "", "source release label, for example 26.2")
	out := fs.String("out", "", "output path")
	if err := fs.Parse(os.Args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Println(usage)
			return nil
		}
		return fmt.Errorf("%w\n\n%s", err, usage)
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected argument %q\n\n%s", fs.Arg(0), usage)
	}
	if *jar == "" || *out == "" || *source == "" {
		return errors.New(usage)
	}

	// Static rename tables referenced by the fixer list live in their own
	// classes; they are read first so a getstatic can resolve to real pairs.
	staticTables := map[string][]pair{}
	for _, class := range []string{"net.minecraft.util.datafix.fixes.CavesAndCliffsRenames"} {
		listing, err := disassemble(*javap, *jar, class)
		if err != nil {
			return err
		}
		for field, pairs := range collectStaticTables(listing, class) {
			staticTables[field] = pairs
		}
	}

	listing, err := disassemble(*javap, *jar, "net.minecraft.util.datafix.DataFixers")
	if err != nil {
		return err
	}
	tables := scan(listing, staticTables)
	tables.Schema = Schema
	tables.Source = *source

	data, err := json.MarshalIndent(tables, "", " ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(*out, append(data, '\n'), 0o644); err != nil {
		return err
	}

	fmt.Printf("block renames %d from %d/%d fixers\n", len(tables.Blocks), tables.Coverage.BlockExtracted, tables.Coverage.BlockFixers)
	fmt.Printf("item renames  %d from %d/%d fixers\n", len(tables.Items), tables.Coverage.ItemExtracted, tables.Coverage.ItemFixers)
	for _, missed := range tables.Coverage.Unextracted {
		fmt.Printf("  not extracted: %s\n", missed)
	}
	fmt.Println("wrote", *out)
	return nil
}

func disassemble(javap, jar, class string) ([]string, error) {
	command := exec.Command(javap, "-p", "-c", "-cp", jar, class)
	output, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("javap %s: %w", class, err)
	}
	return strings.Split(string(output), "\n"), nil
}

type pair struct{ from, to string }

// scanner follows the bytecode that builds Mojang's fixer list. Rename tables
// are assembled on the operand stack, stored in a local, and then handed to a
// fixer constructor, so string constants are consumed where they are used and
// renamers are tracked by the local slot that holds them.
type scanner struct {
	strings []string
	lastInt int32
	version int32
	// pendingMap is any string map just built on the stack. Many fixers besides
	// block and item renames are constructed from one, so a map only becomes a
	// rename table once createRenamer consumes it.
	pendingMap     []pair
	pendingRenamer []pair
	bySlot         map[int][]pair
	loaded         []pair
	loadedOK       bool
	statics        map[string][]pair

	tables Tables
}

func scan(listing []string, statics map[string][]pair) Tables {
	s := &scanner{bySlot: map[int][]pair{}, statics: statics}
	for index, line := range listing {
		s.line(line, listing, index)
	}
	sortRenames(s.tables.Blocks)
	sortRenames(s.tables.Items)
	sort.Strings(s.tables.Coverage.Unextracted)
	return s.tables
}

func (s *scanner) line(line string, listing []string, index int) {
	if match := stringConstant.FindStringSubmatch(line); match != nil {
		s.strings = append(s.strings, strings.TrimSpace(match[1]))
		return
	}
	if match := intConstant.FindStringSubmatch(line); match != nil {
		s.lastInt = parseInt32(match[1])
		return
	}
	if match := intFromPool.FindStringSubmatch(line); match != nil {
		s.lastInt = parseInt32(match[1])
		return
	}
	if match := staticFieldRead.FindStringSubmatch(line); match != nil {
		if pairs, exists := s.statics[strings.TrimSuffix(match[1], ":")]; exists {
			s.pendingMap = pairs
		}
		return
	}
	if match := storeLocal.FindStringSubmatch(line); match != nil && len(s.pendingRenamer) > 0 {
		s.bySlot[parseSlot(match)] = s.pendingRenamer
		return
	}
	if match := loadLocal.FindStringSubmatch(line); match != nil {
		if pairs, exists := s.bySlot[parseSlot(match)]; exists {
			s.loaded, s.loadedOK = pairs, true
		}
		return
	}
	match := invocation.FindStringSubmatch(line)
	if match == nil {
		return
	}
	s.invoke(strings.TrimSpace(match[1]), listing, index)
}

func (s *scanner) invoke(target string, listing []string, index int) {
	name, descriptor := splitInvocation(target)
	switch {
	case strings.HasSuffix(name, "DataFixerBuilder.addSchema"):
		s.version = s.lastInt
		s.strings = nil
	case strings.HasSuffix(name, "Map.of"):
		s.pendingMap = s.takePairs(countObjectParameters(descriptor) / 2)
	case strings.HasSuffix(name, "ImmutableMap$Builder.put"):
		s.pendingMap = append(s.pendingMap, s.takePairs(1)...)
	// javap prints no owner for a call inside the same class, so the renamer
	// helper appears as a bare method name.
	case name == "createRenamer" || strings.HasSuffix(name, ".createRenamer"):
		if strings.Count(descriptor, "Ljava/lang/String;") == 2 {
			s.pendingRenamer = s.takePairs(1)
			break
		}
		s.pendingRenamer, s.pendingMap = s.pendingMap, nil
	case strings.HasSuffix(name, "BlockRenameFix.create"):
		s.tables.Coverage.BlockFixers++
		s.emit(&s.tables.Blocks, &s.tables.Coverage.BlockExtracted, listing, index)
	case strings.HasSuffix(name, "ItemRenameFix.create"):
		s.tables.Coverage.ItemFixers++
		s.emit(&s.tables.Items, &s.tables.Coverage.ItemExtracted, listing, index)
	}
}

func (s *scanner) emit(into *[]Rename, extracted *int, listing []string, index int) {
	pairs := s.pendingRenamer
	if s.loadedOK {
		pairs = s.loaded
	}
	// The renamer is consumed here. Every fixer loads its own renamer
	// immediately before the call, so carrying one over would silently attribute
	// a previous fixer's table to a fixer whose own table could not be read.
	s.pendingRenamer, s.loaded, s.loadedOK = nil, nil, false

	if len(pairs) == 0 {
		s.tables.Coverage.Unextracted = append(s.tables.Coverage.Unextracted, describeFixer(listing, index))
		return
	}
	*extracted++
	for _, item := range pairs {
		*into = append(*into, Rename{DataVersion: s.version, From: item.from, To: item.to})
	}
}

// takePairs consumes the most recent string constants as key and value pairs.
func (s *scanner) takePairs(count int) []pair {
	needed := count * 2
	if needed <= 0 || len(s.strings) < needed {
		return nil
	}
	window := s.strings[len(s.strings)-needed:]
	s.strings = s.strings[:len(s.strings)-needed]
	pairs := make([]pair, 0, count)
	for offset := 0; offset < needed; offset += 2 {
		pairs = append(pairs, pair{from: window[offset], to: window[offset+1]})
	}
	return pairs
}

// collectStaticTables reads a class whose only job is to declare rename maps.
func collectStaticTables(listing []string, class string) map[string][]pair {
	field := ""
	for _, line := range listing {
		if strings.Contains(line, "ImmutableMap<java.lang.String, java.lang.String>") {
			parts := strings.Fields(strings.TrimSuffix(strings.TrimSpace(line), ";"))
			field = parts[len(parts)-1]
		}
	}
	if field == "" {
		return nil
	}

	s := &scanner{bySlot: map[int][]pair{}}
	for _, line := range listing {
		s.line(line, listing, 0)
	}
	if len(s.pendingMap) == 0 {
		return nil
	}
	return map[string][]pair{strings.ReplaceAll(class, ".", "/") + "." + field: s.pendingMap}
}

func describeFixer(listing []string, index int) string {
	for offset := index - 1; offset >= 0 && offset > index-20; offset-- {
		if match := stringConstant.FindStringSubmatch(listing[offset]); match != nil {
			return strings.TrimSpace(match[1])
		}
	}
	return "unnamed fixer"
}

func splitInvocation(target string) (string, string) {
	if separator := strings.Index(target, ":("); separator >= 0 {
		return strings.ReplaceAll(target[:separator], "/", "."), target[separator+1:]
	}
	return strings.ReplaceAll(target, "/", "."), ""
}

func countObjectParameters(descriptor string) int {
	return strings.Count(descriptor, "Ljava/lang/Object;")
}

func parseSlot(match []string) int {
	value := match[1]
	if value == "" {
		value = match[2]
	}
	slot, _ := strconv.Atoi(value)
	return slot
}

func parseInt32(value string) int32 {
	parsed, _ := strconv.ParseInt(value, 10, 32)
	return int32(parsed)
}

func sortRenames(renames []Rename) {
	sort.Slice(renames, func(i, j int) bool {
		if renames[i].DataVersion != renames[j].DataVersion {
			return renames[i].DataVersion < renames[j].DataVersion
		}
		if renames[i].From != renames[j].From {
			return renames[i].From < renames[j].From
		}
		return renames[i].To < renames[j].To
	})
}
