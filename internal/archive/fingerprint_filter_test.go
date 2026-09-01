package archive

import (
	"strings"
	"testing"
)

// Every command that takes a server normalizes it. This one compared the raw
// flag against names that are always normalized on disk, so a differently-cased
// id produced an empty fingerprint with the root of an empty tree and no error,
// which is indistinguishable from an archive that holds nothing and is an
// invitation to a peer to send everything.
func TestTheServerFilterIsNormalizedLikeEverywhereElse(t *testing.T) {
	dir := t.TempDir()
	a, err := Init(dir)
	if err != nil {
		t.Fatal(err)
	}
	storedObservation(t, a, "minecraft:overworld", 3, 4, "alice", 1, "one")

	servers, err := a.Servers()
	if err != nil || len(servers) == 0 {
		t.Fatalf("the fixture stored nothing: %v", err)
	}
	stored := servers[0]

	exact, err := a.Fingerprint(stored)
	if err != nil {
		t.Fatal(err)
	}
	if len(exact.States) == 0 {
		t.Fatal("the stored server id fingerprinted nothing, so this test would prove nothing")
	}

	for _, spelling := range []string{
		strings.ToUpper(stored),
		"  " + stored + "  ",
	} {
		got, err := a.Fingerprint(spelling)
		if err != nil {
			t.Fatalf("%q: %v", spelling, err)
		}
		if got.Root != exact.Root {
			t.Errorf("%q fingerprinted %d state(s) with root %s; the same server spelled exactly gave %d and %s",
				spelling, len(got.States), got.Root[:12], len(exact.States), exact.Root[:12])
		}
	}
}

// A line that is not empty and holds no fields. Any editor or transport can
// introduce one, and this took the program down with a stack trace on a file a
// peer supplied.
func TestALineOfSpacesDoesNotCrashTheParser(t *testing.T) {
	text := FingerprintSchema + "\n   \n\t \nroot " + strings.Repeat("0", 64) + "\n"
	if _, err := ParseFingerprint(strings.NewReader(text)); err == nil {
		t.Error("a fingerprint whose root does not match its entries was accepted")
	}
}
