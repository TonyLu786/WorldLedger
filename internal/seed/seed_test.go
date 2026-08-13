package seed

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// The vectors in testdata were produced by a real JVM running java.util.Random,
// by testdata/Gen.java. Structure placement depends on reproducing that sequence
// exactly, so agreement is checked against the real implementation rather than
// against this package's own idea of what the algorithm is.
func loadVectors(t *testing.T) ([]string, []string) {
	t.Helper()
	file, err := os.Open(filepath.Join("testdata", "java-random-vectors.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	var nextInt, placement []string
	section := ""
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") {
			switch {
			case strings.Contains(line, "nextInt"):
				section = "nextInt"
			case strings.Contains(line, "placement"):
				section = "placement"
			}
			continue
		}
		if section == "nextInt" {
			nextInt = append(nextInt, line)
		} else if section == "placement" {
			placement = append(placement, line)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if len(nextInt) == 0 || len(placement) == 0 {
		t.Fatal("vector file is missing a section")
	}
	return nextInt, placement
}

func TestNextIntMatchesJavaUtilRandom(t *testing.T) {
	vectors, _ := loadVectors(t)

	for _, line := range vectors {
		fields := strings.Fields(line)
		if len(fields) != 3 {
			t.Fatalf("malformed vector %q", line)
		}
		seedValue, err := strconv.ParseInt(fields[0], 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		bound, err := strconv.ParseInt(fields[1], 10, 32)
		if err != nil {
			t.Fatal(err)
		}
		want := strings.Split(fields[2], ",")

		random := NewLegacyRandomSource(seedValue)
		for index, expected := range want {
			got := random.NextInt(int32(bound))
			if strconv.Itoa(int(got)) != expected {
				t.Fatalf("seed %d bound %d draw %d: got %d, want %s", seedValue, bound, index, got, expected)
			}
		}
	}
}

func TestPotentialChunkMatchesTheDisassembledPlacement(t *testing.T) {
	_, vectors := loadVectors(t)

	for _, line := range vectors {
		fields := strings.Fields(line)
		if len(fields) != 9 {
			t.Fatalf("malformed vector %q", line)
		}
		numbers := make([]int64, len(fields))
		for index, field := range fields {
			value, err := strconv.ParseInt(field, 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			numbers[index] = value
		}

		spread := SpreadLinear
		if numbers[4] == 1 {
			spread = SpreadTriangular
		}
		placement := Placement{
			Spacing:    int32(numbers[1]),
			Separation: int32(numbers[2]),
			Salt:       int32(numbers[3]),
			SpreadType: spread,
		}
		got, err := placement.PotentialChunk(numbers[0], int32(numbers[5]), int32(numbers[6]))
		if err != nil {
			t.Fatal(err)
		}
		want := ChunkPos{X: int32(numbers[7]), Z: int32(numbers[8])}
		if got != want {
			t.Fatalf("seed %d %+v chunk (%d,%d): got %+v, want %+v",
				numbers[0], placement, numbers[5], numbers[6], got, want)
		}
	}
}

func TestSetSeedScramblesLikeJava(t *testing.T) {
	// java.util.Random(0) has internal state (0 ^ 0x5DEECE66D).
	random := NewLegacyRandomSource(0)
	if got := random.State(); got != 0x5DEECE66D {
		t.Fatalf("state = %#x; want %#x", got, uint64(0x5DEECE66D))
	}
}

func TestSetLargeFeatureWithSaltUsesTheRegionSalts(t *testing.T) {
	random := &LegacyRandomSource{}
	random.SetLargeFeatureWithSalt(12345, 3, -7, 14357617)

	want := &LegacyRandomSource{}
	want.SetSeed(3*341873128712 + -7*132897987541 + 12345 + 14357617)
	if random.State() != want.State() {
		t.Fatal("positional seeding does not match the disassembled formula")
	}
}

func TestObservationsConstrainCandidates(t *testing.T) {
	placement := Placement{Spacing: 32, Separation: 8, Salt: 14357617, SpreadType: SpreadLinear}
	const truth = 987654321

	chunk, err := placement.PotentialChunk(truth, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	observation := Observation{Placement: placement, Chunk: chunk}

	if !observation.Matches(truth) {
		t.Fatal("the seed that produced the placement does not match it")
	}
	if !Consistent(truth, []Observation{observation}) {
		t.Fatal("Consistent disagrees with Matches")
	}

	// A single region constrains but does not determine the seed, which is why
	// the package reports candidates rather than an answer.
	collisions := 0
	for candidate := int64(0); candidate < 200000; candidate++ {
		if observation.Matches(candidate) {
			collisions++
		}
	}
	if collisions == 0 {
		t.Fatal("expected at least one unrelated seed to satisfy one observation")
	}
	t.Logf("%d of 200000 scanned seeds satisfy a single-structure constraint", collisions)
}

func TestPlacementValidationRejectsImpossibleConfigurations(t *testing.T) {
	cases := []Placement{
		{Spacing: 0, Separation: 0, SpreadType: SpreadLinear},
		{Spacing: 8, Separation: 8, SpreadType: SpreadLinear},
		{Spacing: 8, Separation: 9, SpreadType: SpreadLinear},
		{Spacing: 8, Separation: -1, SpreadType: SpreadLinear},
		{Spacing: 8, Separation: 1, SpreadType: "guess"},
	}
	for _, placement := range cases {
		if err := placement.Validate(); err == nil {
			t.Fatalf("accepted %+v", placement)
		}
	}
}

func TestFloorDivMatchesJavaForNegativeChunks(t *testing.T) {
	cases := [][3]int32{
		{0, 32, 0}, {31, 32, 0}, {32, 32, 1},
		{-1, 32, -1}, {-32, 32, -1}, {-33, 32, -2},
	}
	for _, test := range cases {
		if got := floorDiv(test[0], test[1]); got != test[2] {
			t.Fatalf("floorDiv(%d,%d) = %d; want %d", test[0], test[1], got, test[2])
		}
	}
}
