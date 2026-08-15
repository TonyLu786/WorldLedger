package bundle

import (
	"os"
	"path/filepath"
	"testing"
)

// The change that introduced pathResolver claims the cached answer is the same
// answer, on the grounds that resolving a path whose final element is not a
// symlink is the resolution of its parent with that element appended.
//
// The escape test that would exercise this most directly needs privileges to
// create a symlink and skips on an ordinary Windows account, so it cannot be
// the only thing standing behind the claim. These check the equivalence itself,
// which needs no privileges anywhere.

func TestCachedResolutionMatchesUncachedResolution(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "components", "deeper")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}

	var files []string
	for _, name := range []string{
		filepath.Join(root, "bundle.json"),
		filepath.Join(root, "components", "one.bin"),
		filepath.Join(root, "components", "two.bin"),
		filepath.Join(nested, "three.bin"),
	} {
		if err := os.WriteFile(name, []byte("payload"), 0o644); err != nil {
			t.Fatal(err)
		}
		files = append(files, name)
	}

	resolver := pathResolver{}
	for _, file := range files {
		uncached, err := resolvePath(file, nil)
		if err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		cached, err := resolvePath(file, resolver)
		if err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		if cached != uncached {
			t.Fatalf("%s resolved to %q with a shared resolver and %q without", file, cached, uncached)
		}
	}

	// Four files in three directories: two of them share a parent, and that
	// parent is resolved once. Resolution is doing real work rather than
	// returning its input, so a cache that changed the answer would show here;
	// on Windows it expands an 8.3 short name to the long one.
	if len(resolver) != 3 {
		t.Fatalf("expected three directories resolved, got %d: %v", len(resolver), resolver)
	}
}

// A resolver carried across calls must not answer for a directory it was never
// asked about, which is what would make a cached result wrong rather than
// merely stale.
func TestResolverAnswersOnlyForDirectoriesItResolved(t *testing.T) {
	root := t.TempDir()
	components := filepath.Join(root, "components")
	if err := os.MkdirAll(components, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(components, "one.bin")
	if err := os.WriteFile(file, []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}

	resolver := pathResolver{}
	if _, err := resolvePath(file, resolver); err != nil {
		t.Fatal(err)
	}
	if _, cached := resolver[root]; cached {
		t.Fatal("the resolver cached a directory it was not asked to resolve")
	}
	if _, cached := resolver[components]; !cached {
		t.Fatal("the resolver did not cache the directory it did resolve")
	}
}

// A path that does not exist has to fail rather than be answered from a cached
// parent, because the component file itself is what the caller is asking about.
func TestAMissingDirectoryStillFails(t *testing.T) {
	root := t.TempDir()
	resolver := pathResolver{}
	if _, err := resolvePath(filepath.Join(root, "absent", "one.bin"), resolver); err == nil {
		t.Fatal("resolving a file under a directory that does not exist should fail")
	}
	if len(resolver) != 0 {
		t.Fatalf("a failed resolution should cache nothing, got %v", resolver)
	}
}
