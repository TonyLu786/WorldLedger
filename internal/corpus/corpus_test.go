package corpus

import (
	"strings"
	"testing"

	"github.com/worldledger/worldledger-mc/internal/archive"
	"github.com/worldledger/worldledger-mc/internal/mcjava"
)

// The point of this package is to notice when the fixture world has quietly
// become thinner, so the thing worth testing is that it says so. A check that
// only ever passes would be worse than no check: it would be a green light
// nobody had earned.

// The overworld's ends in 26.2, used only to build fixtures. The package reads
// them from each observation rather than knowing them.
var testEnds = buildRange{bottom: -4, top: 19}

func fill(state mcjava.BlockState) []mcjava.BlockState {
	states := make([]mcjava.BlockState, mcjava.BlockCount)
	for i := range states {
		states[i] = state
	}
	return states
}

func mixed(a, b mcjava.BlockState) []mcjava.BlockState {
	states := fill(a)
	states[0] = b
	return states
}

func TestAUniformSectionAtTheEndOfTheRangeIsNotCountedAsMixed(t *testing.T) {
	present := map[string]bool{}
	payload, err := mcjava.EncodeBlockSection(testEnds.bottom, fill(mcjava.BlockState{Name: "minecraft:bedrock"}))
	if err != nil {
		t.Fatal(err)
	}
	inspectBlocks(payload, present, testEnds)
	if present["a mixed section at the bottom of the build range"] {
		t.Error("a section of one block was reported as mixed")
	}
}

func TestAMixedSectionAtEachEndIsFound(t *testing.T) {
	for _, test := range []struct {
		sectionY int32
		shape    string
	}{
		{testEnds.bottom, "a mixed section at the bottom of the build range"},
		{testEnds.top, "a mixed section at the top of the build range"},
	} {
		present := map[string]bool{}
		payload, err := mcjava.EncodeBlockSection(test.sectionY,
			mixed(mcjava.BlockState{Name: "minecraft:air"}, mcjava.BlockState{Name: "minecraft:glass"}))
		if err != nil {
			t.Fatal(err)
		}
		inspectBlocks(payload, present, testEnds)
		if !present[test.shape] {
			t.Errorf("section %d: %q was not found", test.sectionY, test.shape)
		}
	}
}

// A section in the middle is not either end, however mixed it is.
func TestAMixedSectionInTheMiddleIsNeitherEnd(t *testing.T) {
	present := map[string]bool{}
	payload, err := mcjava.EncodeBlockSection(4,
		mixed(mcjava.BlockState{Name: "minecraft:air"}, mcjava.BlockState{Name: "minecraft:stone"}))
	if err != nil {
		t.Fatal(err)
	}
	inspectBlocks(payload, present, testEnds)
	for _, shape := range []string{
		"a mixed section at the bottom of the build range",
		"a mixed section at the top of the build range",
	} {
		if present[shape] {
			t.Errorf("a middle section was reported as %q", shape)
		}
	}
}

func TestPropertyRichAndWaterloggedBlocksAreFound(t *testing.T) {
	present := map[string]bool{}
	fence := mcjava.BlockState{
		Name: "minecraft:oak_fence",
		Properties: []mcjava.Property{
			{Name: "east", Value: "true"},
			{Name: "north", Value: "false"},
			{Name: "waterlogged", Value: "true"},
		},
	}
	payload, err := mcjava.EncodeBlockSection(4, mixed(mcjava.BlockState{Name: "minecraft:air"}, fence))
	if err != nil {
		t.Fatal(err)
	}
	inspectBlocks(payload, present, testEnds)

	if !present["a block state with three or more properties"] {
		t.Error("a block with three properties was not found")
	}
	if !present["a waterlogged block that is not water"] {
		t.Error("a waterlogged fence was not found")
	}
}

// Water is waterlogged by nature and proves nothing about the property
// travelling on other blocks.
func TestWaterItselfDoesNotSatisfyTheWaterloggedShape(t *testing.T) {
	present := map[string]bool{}
	water := mcjava.BlockState{
		Name:       "minecraft:water",
		Properties: []mcjava.Property{{Name: "waterlogged", Value: "true"}},
	}
	payload, err := mcjava.EncodeBlockSection(4, mixed(mcjava.BlockState{Name: "minecraft:air"}, water))
	if err != nil {
		t.Fatal(err)
	}
	inspectBlocks(payload, present, testEnds)
	if present["a waterlogged block that is not water"] {
		t.Error("water was accepted as a waterlogged block that is not water")
	}
}

func TestOneBiomeIsNotMoreThanOne(t *testing.T) {
	biomes := make([]string, mcjava.BiomeCount)
	for i := range biomes {
		biomes[i] = "minecraft:desert"
	}
	payload, err := mcjava.EncodeBiomeSection(4, biomes)
	if err != nil {
		t.Fatal(err)
	}
	present := map[string]bool{}
	inspectBiomes(payload, present)
	if present["more than one biome in a chunk"] {
		t.Error("a uniform biome section was reported as mixed")
	}

	biomes[0] = "minecraft:plains"
	payload, err = mcjava.EncodeBiomeSection(4, biomes)
	if err != nil {
		t.Fatal(err)
	}
	present = map[string]bool{}
	inspectBiomes(payload, present)
	if !present["more than one biome in a chunk"] {
		t.Error("two biomes were not reported as more than one")
	}
}

// A modern sign carries front_text and back_text compounds, so nesting is what
// a client can actually be asked about. The first version of this asked whether
// a block entity had contents and failed against a correct capture, because a
// client is never sent the inside of an unopened container.
func TestNestedPayloadsAreFoundAndFlatOnesAreNot(t *testing.T) {
	flat := mcjava.BlockEntity{
		LocalX: 1, BlockY: 65, LocalZ: 1, Type: "minecraft:something",
		NBT: mcjava.NBTValue{Type: mcjava.TagCompound, Compound: []mcjava.NamedNBT{
			{Name: "is_waxed", Value: mcjava.NBTValue{Type: mcjava.TagByte}},
		}},
	}
	payload, err := mcjava.EncodeBlockEntities([]mcjava.BlockEntity{flat})
	if err != nil {
		t.Fatal(err)
	}
	present := map[string]bool{}
	inspectBlockEntities(payload, present)
	if present["a block entity whose payload has nested structure"] {
		t.Error("a payload of one scalar was called nested")
	}

	sign := mcjava.BlockEntity{
		LocalX: 1, BlockY: 65, LocalZ: 1, Type: "minecraft:sign",
		NBT: mcjava.NBTValue{Type: mcjava.TagCompound, Compound: []mcjava.NamedNBT{
			{Name: "is_waxed", Value: mcjava.NBTValue{Type: mcjava.TagByte}},
			{Name: "front_text", Value: mcjava.NBTValue{Type: mcjava.TagCompound, Compound: []mcjava.NamedNBT{
				{Name: "color", Value: mcjava.NBTValue{Type: mcjava.TagString, String: "black"}},
			}}},
		}},
	}
	payload, err = mcjava.EncodeBlockEntities([]mcjava.BlockEntity{sign})
	if err != nil {
		t.Fatal(err)
	}
	present = map[string]bool{}
	inspectBlockEntities(payload, present)
	if !present["a block entity whose payload has nested structure"] {
		t.Error("a payload with a compound inside it was not called nested")
	}
}

// The invariant is that a client is not told what is inside a container. It is
// only worth asserting where a container was actually observed, or it passes
// for an archive that never saw one.
func TestTheContainerInvariantNeedsAContainerToHaveBeenSeen(t *testing.T) {
	withContainer := containerEvidence{sawContainer: true, sawContents: false}
	withoutAnything := containerEvidence{sawContainer: false, sawContents: false}
	leaked := containerEvidence{sawContainer: true, sawContents: true}

	for _, test := range []struct {
		name     string
		evidence containerEvidence
		want     bool
	}{
		{"a container seen and nothing inside it captured", withContainer, true},
		{"no container seen at all", withoutAnything, false},
		{"a container seen and its contents captured", leaked, false},
	} {
		got := test.evidence.sawContainer && !test.evidence.sawContents
		if got != test.want {
			t.Errorf("%s: satisfied = %v, want %v", test.name, got, test.want)
		}
	}
}

func TestAContainerBlockIsRecognisedInAPalette(t *testing.T) {
	chest := mcjava.BlockState{
		Name:       "minecraft:chest",
		Properties: []mcjava.Property{{Name: "facing", Value: "north"}},
	}
	payload, err := mcjava.EncodeBlockSection(4, mixed(mcjava.BlockState{Name: "minecraft:air"}, chest))
	if err != nil {
		t.Fatal(err)
	}
	if !holdsAContainer(payload) {
		t.Error("a chest in the palette was not recognised as a container")
	}

	payload, err = mcjava.EncodeBlockSection(4, fill(mcjava.BlockState{Name: "minecraft:stone"}))
	if err != nil {
		t.Fatal(err)
	}
	if holdsAContainer(payload) {
		t.Error("a section of stone was reported as holding a container")
	}
}

// If a release ever started sending container contents, this is what has to
// notice.
func TestContentsArrivingWouldBeSeen(t *testing.T) {
	holding := mcjava.BlockEntity{
		LocalX: 1, BlockY: 65, LocalZ: 1, Type: "minecraft:chest",
		NBT: mcjava.NBTValue{Type: mcjava.TagCompound, Compound: []mcjava.NamedNBT{
			{Name: "Items", Value: mcjava.NBTValue{Type: mcjava.TagString, String: "minecraft:diamond"}},
		}},
	}
	payload, err := mcjava.EncodeBlockEntities([]mcjava.BlockEntity{holding})
	if err != nil {
		t.Fatal(err)
	}
	if !carriesContents(payload) {
		t.Error("a block entity carrying an Items key was not noticed")
	}
}

// What a caller reads when something has gone missing.
func TestTheReportNamesWhatIsMissingAndWhyItMatters(t *testing.T) {
	report := Report{Chunks: 3, Present: map[string]bool{"more than one biome in a chunk": true}}
	for _, shape := range Required {
		if !report.Present[shape.Name] {
			report.Missing = append(report.Missing, shape)
		}
	}
	if report.Complete() {
		t.Fatal("a report missing six shapes called itself complete")
	}

	described := report.Describe()
	if !strings.Contains(described, "3 chunk(s)") {
		t.Errorf("the report does not say how much it looked at:\n%s", described)
	}
	for _, shape := range report.Missing {
		if !strings.Contains(described, shape.Name) {
			t.Errorf("the report does not name the missing %q", shape.Name)
		}
		if !strings.Contains(described, shape.Why) {
			t.Errorf("the report does not say why %q matters", shape.Name)
		}
	}
}

func TestACompleteReportIsComplete(t *testing.T) {
	report := Report{Chunks: 1, Present: map[string]bool{}}
	for _, shape := range Required {
		report.Present[shape.Name] = true
	}
	if !report.Complete() {
		t.Fatalf("a report with everything present was not complete: %+v", report.Missing)
	}
}

// An archive with nothing in it must not pass. "Every required shape was
// found" and "there was nothing to look at" are the same sentence to a build
// log, and only one of them means the fixture is intact.
func TestAnArchiveWithNoObservationsIsNotComplete(t *testing.T) {
	dir := t.TempDir()
	a, err := archive.Init(dir)
	if err != nil {
		t.Fatal(err)
	}
	report, err := Inspect(a, "", "minecraft:overworld")
	if err != nil {
		t.Fatal(err)
	}
	if report.Complete() {
		t.Fatal("an empty archive was reported as containing every shape")
	}
	if len(report.Missing) != len(Required) {
		t.Errorf("missing = %d, want all %d", len(report.Missing), len(Required))
	}
	if report.Chunks != 0 {
		t.Errorf("chunks = %d, want 0", report.Chunks)
	}
}
