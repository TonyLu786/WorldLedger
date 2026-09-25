package diagnose

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// An account name spelled the way a directory spells it.
//
// The first real run of `worldledger diagnose` on the machine this was written
// on leaked one. The account was "Juntong Lu" and the archive path ran through
// C--Users-Juntong-Lu-Desktop-..., which is that name with the space written as
// a hyphen. The segment after Users was masked correctly and the name went out
// anyway, in the file the command introduces by saying "this is everything it
// would hand over".

func TestAnAccountNameIsMaskedHoweverADirectorySpellsIt(t *testing.T) {
	for _, spelling := range []string{
		"Juntong Lu", "Juntong-Lu", "juntong_lu", "JuntongLu", "juntong.lu",
	} {
		variants := accountVariants("Juntong Lu")
		found := false
		for _, variant := range variants {
			if strings.EqualFold(variant, spelling) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%q is not among the spellings of \"Juntong Lu\": %v", spelling, variants)
		}
	}
}

// Longest first, so a spelling that contains a shorter one is taken out whole.
func TestTheSpellingsAreTriedLongestFirst(t *testing.T) {
	variants := accountVariants("Ada B Lovelace")
	for i := 1; i < len(variants); i++ {
		if len(variants[i]) > len(variants[i-1]) {
			t.Fatalf("%q comes after the shorter %q: %v", variants[i], variants[i-1], variants)
		}
	}
}

// A one-word account has nothing to respell, and must not gain variants that
// would mask more than the name.
func TestAOneWordAccountKeepsItsOneSpelling(t *testing.T) {
	if got := accountVariants("alice"); len(got) != 1 || got[0] != "alice" {
		t.Errorf("accountVariants(\"alice\") = %v; want just the name", got)
	}
}

// And the whole of it, against this machine's real account, through the
// function the report actually calls. This is the test that would have caught
// the leak.
func TestThisMachinesNameDoesNotSurviveMasking(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("no home directory on this machine, which is the case masking is for")
	}
	account := filepath.Base(home)
	parts := strings.FieldsFunc(account, func(r rune) bool {
		return r == ' ' || r == '-' || r == '_' || r == '.'
	})
	if len(parts) < 2 {
		t.Skipf("the account here is one word (%q), so there is no respelling to test", account)
	}

	for _, separator := range []string{"-", "_", ".", ""} {
		spelled := strings.Join(parts, separator)
		sample := filepath.Join("C:", "work", "C--Users-"+spelled+"-Desktop", "archive")
		masked := maskPath(sample)
		if strings.Contains(strings.ToLower(masked), strings.ToLower(spelled)) {
			t.Errorf("the account name survived as %q: %s", spelled, masked)
		}
	}
}

// The masking must not swallow the path. A report nobody can read is not a
// safer report, it is a useless one, and "everything is <user>" would pass
// every test above.
func TestMaskingLeavesThePathReadable(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("the sample below is a Windows path")
	}
	masked := maskPath(`C:\Users\Someone Else\AppData\Roaming\.minecraft\config\worldledger\spool`)
	for _, wanted := range []string{"AppData", "Roaming", ".minecraft", "worldledger", "spool"} {
		if !strings.Contains(masked, wanted) {
			t.Errorf("masking removed %q, which is the part that makes the path useful: %s", wanted, masked)
		}
	}
	if strings.Contains(masked, "Someone Else") {
		t.Errorf("another person's account name survived: %s", masked)
	}
}
