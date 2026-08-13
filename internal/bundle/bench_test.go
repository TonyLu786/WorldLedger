package bundle

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/worldledger/worldledger-mc/internal/archive"
)

// The spool half of the guardrails in docs/test-strategy.md. Import is what a
// contributor waits on after a session, and it is the operation whose cost
// grows with how much was captured.
//
// The input is the committed capture bundle, which came off a real client, so
// these numbers describe real component sizes and a real manifest rather than a
// shape chosen to look fast.

func realBundleDir(tb testing.TB) string {
	tb.Helper()
	spool := filepath.Join("..", "..", "testdata", "e2e-capture-bundle", "spool")
	entries, err := os.ReadDir(spool)
	if err != nil {
		tb.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			return filepath.Join(spool, entry.Name())
		}
	}
	tb.Fatalf("no bundle under %s", spool)
	return ""
}

func bundleBytes(tb testing.TB, dir string) int64 {
	tb.Helper()
	var total int64
	err := filepath.Walk(dir, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			total += info.Size()
		}
		return nil
	})
	if err != nil {
		tb.Fatal(err)
	}
	return total
}

// A fresh archive per iteration, because importing into an archive that already
// holds the observation takes the idempotent path instead and would measure the
// wrong thing.
func BenchmarkImportIntoFreshArchive(b *testing.B) {
	dir := realBundleDir(b)
	b.SetBytes(bundleBytes(b, dir))
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		b.StopTimer()
		a, err := archive.Init(b.TempDir())
		if err != nil {
			b.Fatal(err)
		}
		b.StartTimer()

		if _, err := Import(a, dir, Options{Limits: DefaultLimits()}); err != nil {
			b.Fatal(err)
		}
	}
}

// Re-importing an observation the archive already holds is the common case when
// a contributor retries or when two mirrors overlap. It must be cheap, and it
// must not be cheap by skipping verification.
func BenchmarkImportAlreadyPresent(b *testing.B) {
	dir := realBundleDir(b)
	a, err := archive.Init(b.TempDir())
	if err != nil {
		b.Fatal(err)
	}
	if _, err := Import(a, dir, Options{Limits: DefaultLimits()}); err != nil {
		b.Fatal(err)
	}

	b.SetBytes(bundleBytes(b, dir))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Import(a, dir, Options{Limits: DefaultLimits()}); err != nil {
			b.Fatal(err)
		}
	}
}
