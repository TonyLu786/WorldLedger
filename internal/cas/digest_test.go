package cas

import (
	"strings"
	"testing"

	"github.com/worldledger/worldledger-mc/internal/model"
)

// Path is where every one of this store's filesystem paths is built, and a
// digest is not always something this program computed: it arrives from a
// journal recovered off disk, from an observation record, and from a bundle a
// stranger assembled. The check used to be that the string had four characters.
func TestNothingButADigestBecomesAPath(t *testing.T) {
	store := New("/archive/objects")
	valid := strings.Repeat("ab", 32)

	if path := store.Path(model.BlobRef{Digest: valid}); path == "" {
		t.Fatal("a real digest was refused")
	}

	for _, digest := range []string{
		"../../../../victim.txt",
		"aa/../../../../victim.txt",
		`..\..\..\..\victim.txt`,
		strings.Repeat("AB", 32),        // upper case is not what the store writes
		strings.Repeat("ab", 31),        // too short
		strings.Repeat("ab", 33),        // too long
		strings.Repeat("ab", 31) + "gg", // hex only
		"",
	} {
		if path := store.Path(model.BlobRef{Digest: digest}); path != "" {
			t.Errorf("digest %q became the path %q", digest, path)
		}
	}
}

// Remove and Open both build a path, and both have to refuse rather than act on
// whatever Join makes of a string that is not a digest.
func TestRemoveAndOpenRefuseWhatIsNotADigest(t *testing.T) {
	store := New(t.TempDir())
	ref := model.BlobRef{Algorithm: "sha256", Digest: "../../../../victim.txt", Size: 1}

	if _, err := store.Remove(ref); err == nil {
		t.Error("Remove accepted a path in place of a digest")
	}
	if _, err := store.Open(ref); err == nil {
		t.Error("Open accepted a path in place of a digest")
	}
}
