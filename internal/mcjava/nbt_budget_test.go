package mcjava

import (
	"strings"
	"testing"
)

// The byte limits bounded what was read and not what was produced. One input
// byte becomes a hundred-and-fifty-two-byte value, and block entities are
// decoded one after another out of a single component, so sixty-four mebibytes
// of input reached the far side as gigabytes. The bytes arrive undecoded from
// anybody, sit in the object store, and are decoded much later when somebody
// asks for a world.

// A list of TAG_Byte is the cheapest way to say "make me a value": one byte in,
// one struct out.
func byteListNBT(elements int) []byte {
	var b []byte
	b = append(b, byte(TagList))
	b = append(b, byte(TagByte))
	b = append(b, byte(elements>>24), byte(elements>>16), byte(elements>>8), byte(elements))
	b = append(b, make([]byte, elements)...)
	return b
}

func TestOneComponentCannotMaterialiseUnboundedNBT(t *testing.T) {
	limits := DefaultLimits()

	// Comfortably inside the budget.
	if _, err := DecodeNBTWithLimits(byteListNBT(1000), limits); err != nil {
		t.Fatalf("an ordinary list was refused: %v", err)
	}

	// Past it. The bytes are well under MaxNBTBytes, which is exactly why the
	// byte limit was not a memory limit.
	oversized := limits.MaxNBTValues + 1
	if _, err := DecodeNBTWithLimits(byteListNBT(oversized), limits); err == nil {
		t.Error("a list of more values than the budget allows was decoded")
	} else if !strings.Contains(err.Error(), "NBT values") {
		t.Errorf("the refusal does not say why: %v", err)
	}
}

// The budget is shared across a component. Splitting the same total across many
// payloads was the route that turned a bounded NBT into an unbounded component.
func TestTheBudgetIsSpentAcrossTheWholeComponent(t *testing.T) {
	limits := DefaultLimits()
	limits.MaxNBTValues = 2000

	budget := &nbtBudget{remaining: limits.MaxNBTValues}
	if _, err := decodeNBTSharing(byteListNBT(1500), limits, budget); err != nil {
		t.Fatalf("the first payload was refused: %v", err)
	}
	if _, err := decodeNBTSharing(byteListNBT(1500), limits, budget); err == nil {
		t.Error("a second payload spent a budget the first had already used")
	}
}

// And a fresh component starts with a fresh budget, so a large archive is not
// refused because an earlier component was large.
func TestEachComponentGetsItsOwnBudget(t *testing.T) {
	limits := DefaultLimits()
	limits.MaxNBTValues = 2000

	for i := 0; i < 3; i++ {
		if _, err := DecodeNBTWithLimits(byteListNBT(1500), limits); err != nil {
			t.Fatalf("component %d was refused: %v", i, err)
		}
	}
}
