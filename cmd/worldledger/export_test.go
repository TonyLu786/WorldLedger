package main

import (
	"reflect"
	"testing"

	"github.com/worldledger/worldledger-mc/internal/anvil"
	"github.com/worldledger/worldledger-mc/internal/epoch"
	"github.com/worldledger/worldledger-mc/internal/mcjava"
	"github.com/worldledger/worldledger-mc/internal/model"
)

func observationWithProtocol(protocol string) *model.Observation {
	return &model.Observation{Protocol: protocol}
}

func TestObservedReleasesComeFromTheObservationProtocol(t *testing.T) {
	snapshot := epoch.Snapshot{Selections: []epoch.Selection{
		{Selected: observationWithProtocol("minecraft-java/26.2;canonical=worldledger.minecraft.java.chunk/v1")},
		{Selected: observationWithProtocol("minecraft-java/26.2;canonical=worldledger.minecraft.java.chunk/v1")},
		{Selected: observationWithProtocol("minecraft-java/25.4;canonical=worldledger.minecraft.java.chunk/v1")},
		// A chunk with no observation at the epoch contributes nothing.
		{Selected: nil},
		// An unrelated protocol is not a Minecraft release label.
		{Selected: observationWithProtocol("test/v1")},
	}}

	if got := observedReleases(snapshot); !reflect.DeepEqual(got, []string{"25.4", "26.2"}) {
		t.Fatalf("releases = %#v", got)
	}
}

// A capture can contain blocks a vanilla client has never heard of. Saying so
// before the export is written is the difference between a world that will not
// open and a world the operator knows needs mods.
func TestModNamespacesAreReportedFromEveryComponent(t *testing.T) {
	prepared := []anvil.PreparedChunk{{
		Components: anvil.ChunkComponents{
			Blocks: map[int32]mcjava.BlockSection{
				0: {Palette: []string{
					"minecraft:stone",
					"examplemod:reactor[axis=y]",
					"minecraft:oak_log[axis=x]",
				}},
			},
			Biomes: map[int32]mcjava.BiomeSection{
				0: {Palette: []string{"minecraft:plains", "biomesmod:crystal_fields"}},
			},
			BlockEntities: []mcjava.BlockEntity{
				{Type: "minecraft:sign"},
				{Type: "furnituremod:cabinet"},
			},
		},
	}}

	want := []string{"biomesmod", "examplemod", "furnituremod"}
	if got := modNamespaces(prepared); !reflect.DeepEqual(got, want) {
		t.Fatalf("namespaces = %#v; want %#v", got, want)
	}
}

func TestVanillaOnlyExportReportsNoModNamespaces(t *testing.T) {
	prepared := []anvil.PreparedChunk{{
		Components: anvil.ChunkComponents{
			Blocks: map[int32]mcjava.BlockSection{
				0: {Palette: []string{"minecraft:air", "minecraft:oak_stairs[facing=north,waterlogged=false]"}},
			},
			Biomes: map[int32]mcjava.BiomeSection{0: {Palette: []string{"minecraft:plains"}}},
		},
	}}

	if got := modNamespaces(prepared); len(got) != 0 {
		t.Fatalf("a vanilla export reported mod namespaces %#v", got)
	}
}
