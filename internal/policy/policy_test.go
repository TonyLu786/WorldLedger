package policy

import (
	"testing"
	"time"

	"github.com/worldledger/worldledger-mc/internal/model"
)

func chunkSet(coords ...[2]int32) []model.ChunkRef {
	out := make([]model.ChunkRef, 0, len(coords))
	for _, coord := range coords {
		out = append(out, model.ChunkRef{ServerID: "example.org", Dimension: "minecraft:overworld", X: coord[0], Z: coord[1]})
	}
	return out
}

func square(size int32) []model.ChunkRef {
	var out []model.ChunkRef
	for x := int32(0); x < size; x++ {
		for z := int32(0); z < size; z++ {
			out = append(out, model.ChunkRef{ServerID: "example.org", Dimension: "minecraft:overworld", X: x, Z: z})
		}
	}
	return out
}

func TestAbsentPolicyIsNotADefault(t *testing.T) {
	store := NewStore(t.TempDir())

	_, found, err := store.Lookup("example.org")
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Fatal("a server with no declaration reported one")
	}
}

func TestDeclareRoundTripsAndNormalizes(t *testing.T) {
	store := NewStore(t.TempDir())

	if err := store.Declare(ServerPolicy{
		Server:      "Example.ORG",
		Disposition: Private,
		DeclaredBy:  "juntong",
		Note:        "operator has not been asked",
	}); err != nil {
		t.Fatal(err)
	}

	declared, found, err := store.Lookup("example.org")
	if err != nil || !found {
		t.Fatalf("found=%v err=%v", found, err)
	}
	if declared.Server != "example.org" {
		t.Fatalf("server = %q; want normalized", declared.Server)
	}
	if declared.DeclaredBy != "juntong" || declared.DeclaredAt.IsZero() {
		t.Fatalf("attribution missing: %#v", declared)
	}
}

func TestPolicyMustNameWhoDeclaredIt(t *testing.T) {
	store := NewStore(t.TempDir())

	err := store.Declare(ServerPolicy{Server: "example.org", Disposition: Public, DeclaredBy: "  "})
	if err == nil {
		t.Fatal("an unsigned policy was accepted")
	}
}

func TestEmbargoRequiresAnEndDateAndExpires(t *testing.T) {
	store := NewStore(t.TempDir())

	if err := store.Declare(ServerPolicy{Server: "a.example", Disposition: Embargoed, DeclaredBy: "juntong"}); err == nil {
		t.Fatal("an embargo with no end date was accepted")
	}

	until := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	policy := ServerPolicy{Schema: Schema, Server: "a.example", Disposition: Embargoed, EmbargoUntil: &until, DeclaredBy: "juntong", DeclaredAt: time.Now().UTC()}
	if err := policy.Validate(); err != nil {
		t.Fatal(err)
	}
	if allowed, _ := policy.DistributionAllowed(until.Add(-time.Hour)); allowed {
		t.Fatal("distribution allowed before the embargo ended")
	}
	if allowed, _ := policy.DistributionAllowed(until.Add(time.Hour)); !allowed {
		t.Fatal("distribution still refused after the embargo ended")
	}
}

func TestOnlyEmbargoCarriesAnEndDate(t *testing.T) {
	until := time.Now().UTC()
	policy := ServerPolicy{Schema: Schema, Server: "a.example", Disposition: Public, EmbargoUntil: &until, DeclaredBy: "juntong", DeclaredAt: until}
	if err := policy.Validate(); err == nil {
		t.Fatal("a non-embargo policy accepted an end date")
	}
}

func TestDistributionDefaultsToRefusalForEveryNonPublicDisposition(t *testing.T) {
	now := time.Now().UTC()
	for _, disposition := range []Disposition{Private, Research} {
		policy := ServerPolicy{Disposition: disposition}
		if allowed, _ := policy.DistributionAllowed(now); allowed {
			t.Fatalf("%s allowed distribution", disposition)
		}
	}
	if allowed, _ := (ServerPolicy{Disposition: Public}).DistributionAllowed(now); !allowed {
		t.Fatal("public refused distribution")
	}
}

func TestExposureRisesWithContiguousCoverage(t *testing.T) {
	scattered := Assess("example.org", chunkSet([2]int32{0, 0}, [2]int32{100, 100}, [2]int32{-500, 250}))
	if scattered.Exposure != ExposureMinimal {
		t.Fatalf("scattered coverage = %s (%s)", scattered.Exposure, scattered.Reason)
	}
	if scattered.LargestCluster != 1 {
		t.Fatalf("largest cluster = %d; want 1", scattered.LargestCluster)
	}

	moderate := Assess("example.org", square(10))
	if moderate.Exposure != ExposureModerate {
		t.Fatalf("100 contiguous chunks = %s (%s)", moderate.Exposure, moderate.Reason)
	}

	substantial := Assess("example.org", square(32))
	if substantial.Exposure != ExposureSubstantial {
		t.Fatalf("a full region = %s (%s)", substantial.Exposure, substantial.Reason)
	}
	if substantial.LargestCluster != 32*32 {
		t.Fatalf("largest cluster = %d; want %d", substantial.LargestCluster, 32*32)
	}
}

// Contiguity, not raw count, is what the assessment is about: a thousand
// scattered chunks are weaker evidence than a solid block of the same size.
func TestScatteredCoverageRanksBelowContiguousCoverageOfTheSameSize(t *testing.T) {
	var scattered []model.ChunkRef
	for i := int32(0); i < 1024; i++ {
		scattered = append(scattered, model.ChunkRef{ServerID: "example.org", X: i * 4, Z: i * 4})
	}
	loose := Assess("example.org", scattered)
	dense := Assess("example.org", square(32))

	if loose.Chunks != dense.Chunks {
		t.Fatalf("comparison is not size matched: %d vs %d", loose.Chunks, dense.Chunks)
	}
	if loose.Exposure == ExposureSubstantial {
		t.Fatalf("scattered coverage was ranked substantial: %s", loose.Reason)
	}
	if dense.Exposure != ExposureSubstantial {
		t.Fatalf("contiguous coverage was not ranked substantial: %s", dense.Reason)
	}
}

func TestAssessmentReportsBoundsAndRegions(t *testing.T) {
	assessment := Assess("example.org", chunkSet([2]int32{-40, 5}, [2]int32{40, -5}))
	if assessment.MinX != -40 || assessment.MaxX != 40 || assessment.MinZ != -5 || assessment.MaxZ != 5 {
		t.Fatalf("bounds = %#v", assessment)
	}
	if assessment.Regions != 2 {
		t.Fatalf("regions = %d; want 2", assessment.Regions)
	}
}

func TestEmptyArchiveIsMinimal(t *testing.T) {
	assessment := Assess("example.org", nil)
	if assessment.Exposure != ExposureMinimal || assessment.Chunks != 0 {
		t.Fatalf("%#v", assessment)
	}
}

func TestListReturnsEveryDeclaration(t *testing.T) {
	store := NewStore(t.TempDir())
	for _, server := range []string{"b.example", "a.example"} {
		if err := store.Declare(ServerPolicy{Server: server, Disposition: Private, DeclaredBy: "juntong"}); err != nil {
			t.Fatal(err)
		}
	}
	all, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 || all[0].Server != "a.example" || all[1].Server != "b.example" {
		t.Fatalf("list = %#v", all)
	}
}
