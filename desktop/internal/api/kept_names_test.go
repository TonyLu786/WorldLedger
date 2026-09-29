package api

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// keptSizes remembers a bundle's size against its path and never measures it
// again, and that is exact only because a path is never reused. Nothing in Go
// decides that. The capture adapter names the bundles, in Java, in a different
// build, and a change there that looked harmless -- a shorter name, a counter
// without the session in it, so the folder is easier to read -- would make a
// new capture land on an old one's name. The memo would then report the old
// size for the new bundle, for as long as the window stayed open, and nothing
// would say so.
//
// So the three facts the memo stands on are read out of the source that
// decides them, the way drift_test.go in ui reads the command line and the
// window side by side.

func readSource(t *testing.T, parts ...string) string {
	t.Helper()
	path := filepath.Join(append([]string{"..", "..", ".."}, parts...)...)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s: %v; the adapter moved, and this check has to move with it", path, err)
	}
	return string(data)
}

func TestTheNameAKeptBundleIsRememberedByIsNeverReused(t *testing.T) {
	writer := readSource(t, "adapters", "fabric", "src", "main", "java",
		"org", "worldledger", "fabric", "capture", "BundleSpoolWriter.java")
	coordinator := readSource(t, "adapters", "fabric", "src", "client", "java",
		"org", "worldledger", "fabric", "CaptureCoordinator.java")
	spoolSource := readSource(t, "internal", "spool", "spool.go")

	for _, fact := range []struct {
		source, text, why string
	}{
		{writer, `"ready-" + sessionId + "-" + String.format(Locale.ROOT, "%020d", sequence)`,
			"a bundle is named by its session and its place in that session"},
		{writer, `UUID.fromString(sessionId).toString().equals(sessionId)`,
			"the writer refuses a session that is not a UUID, so the session part cannot be a short counter"},
		{coordinator, `private final String id = UUID.randomUUID().toString();`,
			"every session is a fresh random UUID, so two sessions never share a name"},
		{spoolSource, `ImportedPrefix+strings.TrimPrefix(name, readyPrefix)`,
			"bringing a bundle in renames it and keeps the rest of its name"},
	} {
		if !strings.Contains(fact.source, fact.text) {
			t.Errorf("no longer true: %s.\n  expected to find: %s\n  keptSizes in kept.go remembers sizes by path on the strength of it; "+
				"if bundle names can now repeat, it has to stop doing that", fact.why, fact.text)
		}
	}
}
