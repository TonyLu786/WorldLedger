// Package corpus checks that a capture still contains the shapes the fixture
// world was built to contain.
//
// The committed capture fingerprint is what notices when a Minecraft upgrade
// changes what the game reports. It can only notice about things the fixture
// world actually has, and it says nothing about what the world was supposed to
// have: a fixture that quietly got thinner produces a fingerprint that is
// smaller and just as green.
//
// That is not hypothetical. The gametest places its world with server commands
// whose results nobody reads. A block name that a future release renames, or a
// command whose syntax changes, would place nothing and fail nothing. This
// looks at what was captured and says which of the shapes are missing.
//
// It deliberately checks for kinds of thing rather than for particular blocks.
// Naming the exact block would make this a second, worse copy of the
// fingerprint; what matters is that a block entity with contents was observed,
// not that it was a chest.
package corpus

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/worldledger/worldledger-mc/internal/archive"
	"github.com/worldledger/worldledger-mc/internal/mcjava"
	"github.com/worldledger/worldledger-mc/internal/model"
)

// Shape is one thing the fixture world is meant to exercise.
type Shape struct {
	Name string
	Why  string
}

// Required lists what a capture of the fixture world has to contain.
//
// Each entry names an upgrade hazard rather than a block. A release can rename
// a block and this should still pass; a release that changes how block entities
// are sent should make it fail.
var Required = []Shape{
	{"a block entity whose payload has nested structure",
		"a payload is the release's network representation, and nesting is the part of its shape most likely to move"},
	{"a container whose contents were not captured",
		"a client is never told what is inside an unopened container, and an upgrade that started sending it would be a privacy change nobody asked for"},
	{"a block state with three or more properties",
		"a change to how properties are ordered or encoded hides behind single-property blocks"},
	{"a waterlogged block that is not water",
		"waterlogging has moved between releases and travels on blocks that are not water"},
	{"a mixed section at the bottom of the build range",
		"the lowest section was uniform, so nothing exercised the palette where the range starts"},
	{"a mixed section at the top of the build range",
		"the highest section was uniform, so nothing exercised the palette where the range ends"},
	{"more than one biome in a chunk",
		"a uniform biome section takes the adapter's single-value path and a mixed one does not"},
}

// Report is what a capture was found to contain.
type Report struct {
	Chunks  int
	Present map[string]bool
	Missing []Shape
}

// Complete reports whether every required shape was observed.
func (r Report) Complete() bool { return len(r.Missing) == 0 }

// Inspect reads every observation of a server and reports which shapes it
// found. Components that will not decode are skipped rather than fatal: a
// corpus check that stops at the first oddity reports one problem where there
// may be seven.
func Inspect(a archive.Archive, server, dimension string) (Report, error) {
	gathered, err := a.DimensionObservations(server, dimension)
	if err != nil {
		return Report{}, err
	}

	report := Report{Present: map[string]bool{}}
	evidence := &containerEvidence{}
	for _, entry := range gathered {
		report.Chunks++
		for _, observation := range entry.Observations {
			inspectObservation(a, observation.Components, report.Present, evidence)
		}
	}
	// Both halves are needed. "Nothing was captured" is true of an archive that
	// never saw a container at all, and asserting it there would be a check that
	// passes because there was nothing to find.
	if evidence.sawContainer && !evidence.sawContents {
		report.Present["a container whose contents were not captured"] = true
	}

	for _, shape := range Required {
		if !report.Present[shape.Name] {
			report.Missing = append(report.Missing, shape)
		}
	}
	return report, nil
}

// containerEvidence is the two halves of the container question, gathered
// across every observation before either is answered.
type containerEvidence struct {
	sawContainer bool
	sawContents  bool
}

// containerBlocks are blocks that hold things. The list is short and named
// rather than guessed, because "does this block have an inventory" is not
// something the block identifier says.
var containerBlocks = []string{
	"minecraft:chest", "minecraft:trapped_chest", "minecraft:barrel",
	"minecraft:furnace", "minecraft:hopper", "minecraft:dispenser", "minecraft:dropper",
	"minecraft:shulker_box",
}

func inspectObservation(a archive.Archive, components map[string]model.BlobRef, present map[string]bool, evidence *containerEvidence) {
	// The ends of the build range are read from the observation rather than
	// assumed. Two constants would have been simpler and would report "the
	// world was not built as intended" for a release that moved the range,
	// which is the one situation where the report has to be about the release.
	ends, known := buildRangeEnds(a, components)

	for name, ref := range components {
		payload, err := readBlob(a, ref)
		if err != nil {
			continue
		}
		switch {
		case name == "mcjava.block_entities":
			inspectBlockEntities(payload, present)
			if carriesContents(payload) {
				evidence.sawContents = true
			}
		case strings.HasPrefix(name, "mcjava.blocks."):
			if known {
				inspectBlocks(payload, present, ends)
			}
			if holdsAContainer(payload) {
				evidence.sawContainer = true
			}
		case strings.HasPrefix(name, "mcjava.biomes."):
			inspectBiomes(payload, present)
		}
	}
}

func readBlob(a archive.Archive, ref model.BlobRef) ([]byte, error) {
	file, err := a.CAS.Open(ref)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return io.ReadAll(file)
}

// inspectBlockEntities looks at what the client was actually told.
//
// The first version of this asked whether a block entity had contents, on the
// assumption that a chest holding an item would arrive with them. It does not:
// a client is never sent the inside of an unopened container, which is a thing
// the manual fixture procedure has asserted for a long time and this had to
// learn by finding nothing. Asking the wrong question produced a check that
// failed against a correct capture.
//
// So the two questions are the ones a client can answer. Nesting is the part of
// a payload's shape most likely to move between releases. And the container is
// still worth placing, because "no contents were captured" is only worth
// asserting where there were contents to capture.
func inspectBlockEntities(payload []byte, present map[string]bool) {
	entities, err := mcjava.DecodeBlockEntities(payload)
	if err != nil {
		return
	}
	for _, entity := range entities {
		if nested(entity.NBT) {
			present["a block entity whose payload has nested structure"] = true
		}
	}
}

// carriesContents reports whether any block entity arrived holding an
// inventory. Looked for by several names rather than one, because which key a
// release uses is exactly the kind of thing that changes, and this is the check
// that would have to notice if it started arriving.
func carriesContents(payload []byte) bool {
	entities, err := mcjava.DecodeBlockEntities(payload)
	if err != nil {
		return false
	}
	for _, entity := range entities {
		for _, named := range entity.NBT.Compound {
			switch strings.ToLower(named.Name) {
			case "items", "item", "inventory", "contents":
				return true
			}
		}
	}
	return false
}

// holdsAContainer reports whether a section's palette names a block with an
// inventory.
func holdsAContainer(payload []byte) bool {
	section, err := mcjava.DecodeBlockSection(payload)
	if err != nil {
		return false
	}
	for _, state := range section.Palette {
		for _, container := range containerBlocks {
			if strings.HasPrefix(state, container) {
				return true
			}
		}
	}
	return false
}

// nested reports whether a payload holds a compound or list inside itself,
// rather than only scalars at the top.
func nested(value mcjava.NBTValue) bool {
	for _, named := range value.Compound {
		switch named.Value.Type {
		case mcjava.TagCompound, mcjava.TagList:
			return true
		}
	}
	return false
}

func inspectBlocks(payload []byte, present map[string]bool, ends buildRange) {
	section, err := mcjava.DecodeBlockSection(payload)
	if err != nil {
		return
	}

	distinct := map[string]struct{}{}
	for _, state := range section.States() {
		distinct[state] = struct{}{}
		properties := strings.Count(state, ",")
		if strings.Contains(state, "[") && properties >= 2 {
			present["a block state with three or more properties"] = true
		}
		if strings.Contains(state, "waterlogged=true") && !strings.HasPrefix(state, "minecraft:water") {
			present["a waterlogged block that is not water"] = true
		}
	}

	if len(distinct) < 2 {
		return
	}
	switch section.SectionY {
	case ends.bottom:
		present["a mixed section at the bottom of the build range"] = true
	case ends.top:
		present["a mixed section at the top of the build range"] = true
	}
}

// buildRange is the lowest and highest section an observation says its
// dimension has.
type buildRange struct{ bottom, top int32 }

// buildRangeEnds reads the shape component. Without it there is no way to know
// which sections are the ends, and guessing would turn a release that moved the
// range into a report about the fixture.
func buildRangeEnds(a archive.Archive, components map[string]model.BlobRef) (buildRange, bool) {
	ref, ok := components["mcjava.shape"]
	if !ok {
		return buildRange{}, false
	}
	payload, err := readBlob(a, ref)
	if err != nil {
		return buildRange{}, false
	}
	shape, err := mcjava.DecodeShape(payload)
	if err != nil || shape.SectionCount == 0 {
		return buildRange{}, false
	}
	return buildRange{
		bottom: shape.MinSectionY,
		top:    shape.MinSectionY + int32(shape.SectionCount) - 1,
	}, true
}

func inspectBiomes(payload []byte, present map[string]bool) {
	section, err := mcjava.DecodeBiomeSection(payload)
	if err != nil {
		return
	}
	distinct := map[string]struct{}{}
	for _, biome := range section.Biomes() {
		distinct[biome] = struct{}{}
	}
	if len(distinct) > 1 {
		present["more than one biome in a chunk"] = true
	}
}

// Describe renders a report for somebody reading a build log.
func (r Report) Describe() string {
	var out strings.Builder
	fmt.Fprintf(&out, "%d chunk(s) examined\n", r.Chunks)

	found := make([]string, 0, len(r.Present))
	for name := range r.Present {
		found = append(found, name)
	}
	sort.Strings(found)
	for _, name := range found {
		fmt.Fprintf(&out, "  present  %s\n", name)
	}
	for _, shape := range r.Missing {
		fmt.Fprintf(&out, "  MISSING  %s\n           %s\n", shape.Name, shape.Why)
	}
	return out.String()
}
