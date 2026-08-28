package translate

import (
	"strings"
	"testing"

	"github.com/worldledger/worldledger-mc/internal/mcprofile"
)

// A rename says the same block under a different name. Two of them arriving at
// one block says something else entirely, and the run said "translated with no
// loss" while the world lost the ability to tell them apart.
//
// It is an easy mistake rather than an exotic one: the shipped rename table has
// the chain kelp_top -> kelp -> kelp_plant, and reversing it for a downgrade
// produces exactly this.
func TestTwoRenamesOntoOneBlockAreRefused(t *testing.T) {
	target := mcprofile.Profile{
		Version: "1.21.11",
		Blocks:  []string{"minecraft:kelp", "minecraft:stone"},
		Biomes:  []string{"minecraft:plains"},
	}
	rules := Rules{
		Schema: RulesSchema,
		Renames: map[string]string{
			"legacy:kelp_top":   "minecraft:kelp",
			"legacy:kelp_plant": "minecraft:kelp",
		},
	}
	err := rules.Validate(target)
	if err == nil {
		t.Fatal("a ruleset merging two blocks into one validated")
	}
	if !strings.Contains(err.Error(), "substitution") {
		t.Errorf("the refusal does not say what to do instead: %v", err)
	}
	// Named, so an author can find them.
	for _, source := range []string{"legacy:kelp_top", "legacy:kelp_plant"} {
		if !strings.Contains(err.Error(), source) {
			t.Errorf("the refusal does not name %s: %v", source, err)
		}
	}
}

// A merge declared as what it is stays allowed: substitutions are counted as
// lossy and do not carry properties unless asked, which is the whole difference.
func TestTheSameMergeDeclaredAsSubstitutionsIsAllowed(t *testing.T) {
	target := mcprofile.Profile{
		Version: "1.21.11",
		Blocks:  []string{"minecraft:kelp"},
		Biomes:  []string{"minecraft:plains"},
	}
	rules := Rules{
		Schema: RulesSchema,
		Substitutions: map[string]Substitution{
			"legacy:kelp_top":   {Block: "minecraft:kelp"},
			"legacy:kelp_plant": {Block: "minecraft:kelp"},
		},
	}
	if err := rules.Validate(target); err != nil {
		t.Fatalf("substitutions onto one block were refused: %v", err)
	}
}

func TestRenamesOntoDistinctBlocksAreStillFine(t *testing.T) {
	target := mcprofile.Profile{
		Version: "1.21.11",
		Blocks:  []string{"minecraft:kelp", "minecraft:stone"},
		Biomes:  []string{"minecraft:plains"},
	}
	rules := Rules{
		Schema: RulesSchema,
		Renames: map[string]string{
			"legacy:kelp_top": "minecraft:kelp",
			"legacy:rock":     "minecraft:stone",
		},
	}
	if err := rules.Validate(target); err != nil {
		t.Fatalf("a ruleset with no merge was refused: %v", err)
	}
}
