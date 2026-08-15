package bundle

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// openRegularWithin resolves the whole absolute path of every component with
// EvalSymlinks, which walks from the volume root and opens a handle per element.
// The bundle root is already resolved once before the loop, so the part of that
// walk above the bundle is repeated per component.
//
// These measure the claim before anything is changed on the strength of it.
func benchmarkComponentTree(tb testing.TB, count int) (root, realRoot string, names []string) {
	tb.Helper()
	root = filepath.Join(tb.TempDir(), "ready-benchmark-00000000000000000001")
	components := filepath.Join(root, "components")
	if err := os.MkdirAll(components, 0o755); err != nil {
		tb.Fatal(err)
	}
	payload := make([]byte, realisticComponentBytes)
	for index := 0; index < count; index++ {
		name := fmt.Sprintf("components/component-%03d.bin", index)
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(name)), payload, 0o644); err != nil {
			tb.Fatal(err)
		}
		names = append(names, name)
	}
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		tb.Fatal(err)
	}
	realRoot, err = filepath.Abs(resolved)
	if err != nil {
		tb.Fatal(err)
	}
	return root, realRoot, names
}

// The safety check without the shared resolver, which is what every call did
// before and what a single-file call still does.
func BenchmarkOpenComponentsWithin(b *testing.B) {
	root, realRoot, names := benchmarkComponentTree(b, realisticComponentCount)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, name := range names {
			f, _, err := openRegularWithin(root, realRoot, name, nil)
			if err != nil {
				b.Fatal(err)
			}
			_ = f.Close()
		}
	}
}

// The same files opened with no path validation at all, as a floor. The
// difference between this and the benchmark above is what the safety check
// costs, not what it is worth.
func BenchmarkOpenComponentsPlainly(b *testing.B) {
	root, _, names := benchmarkComponentTree(b, realisticComponentCount)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, name := range names {
			f, err := os.Open(filepath.Join(root, filepath.FromSlash(name)))
			if err != nil {
				b.Fatal(err)
			}
			_ = f.Close()
		}
	}
}

// With one resolver shared across the bundle, which is what import now does.
func BenchmarkOpenComponentsWithinShared(b *testing.B) {
	root, realRoot, names := benchmarkComponentTree(b, realisticComponentCount)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resolver := pathResolver{}
		for _, name := range names {
			f, _, err := openRegularWithin(root, realRoot, name, resolver)
			if err != nil {
				b.Fatal(err)
			}
			_ = f.Close()
		}
	}
}

// Just the resolution that the shared resolver removes.
func BenchmarkEvalSymlinksPerComponent(b *testing.B) {
	root, _, names := benchmarkComponentTree(b, realisticComponentCount)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, name := range names {
			if _, err := filepath.EvalSymlinks(filepath.Join(root, filepath.FromSlash(name))); err != nil {
				b.Fatal(err)
			}
		}
	}
}
