package bundle

import (
	"strings"
	"testing"
)

// The mod and the core are downloaded separately, so somebody holding two
// halves of different ages is an ordinary state rather than a mistake. It was
// already reported; it was not readable. A refusal that names JSON is true
// about the wrong subject.

func refusalFor(t *testing.T, manifest string) string {
	t.Helper()
	_, err := parseManifest([]byte(manifest), DefaultLimits())
	if err == nil {
		t.Fatalf("this manifest was accepted, so the test proves nothing: %s", manifest)
	}
	return err.Error()
}

func TestACaptureFromALaterModSaysToUpdateTheApplication(t *testing.T) {
	message := refusalFor(t, `{"schema":"worldledger.capture-bundle/v2","server_id":"s"}`)
	for _, needed := range []string{"version 2", "version 1", "later WorldLedger mod", "updating WorldLedger"} {
		if !strings.Contains(message, needed) {
			t.Errorf("the refusal does not say %q: %s", needed, message)
		}
	}
	if !strings.Contains(message, "still where it was") {
		t.Errorf("the refusal does not say the capture survived: %s", message)
	}
}

func TestACaptureFromAnEarlierModSaysToUpdateTheMod(t *testing.T) {
	message := refusalFor(t, `{"schema":"worldledger.capture-bundle/v0","server_id":"s"}`)
	if !strings.Contains(message, "earlier WorldLedger mod") {
		t.Errorf("the refusal does not say which way it runs: %s", message)
	}
	if !strings.Contains(message, "updating the mod") {
		t.Errorf("the refusal does not say what to do: %s", message)
	}
}

// The case this reordering was for. A later adapter that adds a field used to
// be refused by the strict decode, which runs before anything reads the schema,
// so the version mismatch never got to explain itself.
func TestALaterCaptureIsExplainedByItsVersionAndNotByItsJSON(t *testing.T) {
	message := refusalFor(t,
		`{"schema":"worldledger.capture-bundle/v2","server_id":"s","capture_note":"something new"}`)
	if strings.Contains(message, "unknown field") {
		t.Errorf("a version mismatch was reported as a JSON problem: %s", message)
	}
	if !strings.Contains(message, "later WorldLedger mod") {
		t.Errorf("the version was not what explained it: %s", message)
	}
}

// And a field that arrives under the version this build does know is a
// different thing, needing a different action.
func TestAFieldAddedWithoutANewVersionIsItsOwnComplaint(t *testing.T) {
	message := refusalFor(t,
		`{"schema":"`+Schema+`","server_id":"s","capture_note":"added quietly"}`)
	if !strings.Contains(message, "capture_note") {
		t.Errorf("the refusal does not name what it did not know: %s", message)
	}
	if !strings.Contains(message, "has to say so with a new version") {
		t.Errorf("the refusal does not say what the writer should have done: %s", message)
	}
}

func TestSomethingThatIsNotACaptureBundleSaysThat(t *testing.T) {
	message := refusalFor(t, `{"schema":"worldledger.transfer-bundle/v1","server_id":"s"}`)
	if !strings.Contains(message, "not a capture bundle") {
		t.Errorf("another family was reported as a version mismatch: %s", message)
	}
}

func TestAManifestWithNoSchemaSaysThatRatherThanGuessing(t *testing.T) {
	message := refusalFor(t, `{"server_id":"s"}`)
	if !strings.Contains(message, "declares no schema") {
		t.Errorf("a manifest with no schema was given a direction it cannot have: %s", message)
	}
}

func TestSplittingASchemaKeepsTheFamilyAndTheNumber(t *testing.T) {
	family, version, ok := splitSchema("worldledger.capture-bundle/v3")
	if !ok || family != "worldledger.capture-bundle" || version != 3 {
		t.Errorf("split = (%q, %d, %v)", family, version, ok)
	}
	if _, _, ok := splitSchema("worldledger.capture-bundle/vbeta"); ok {
		t.Error("a version that is not a number was reported as one")
	}
	if _, _, ok := splitSchema("no-version-here"); ok {
		t.Error("a schema with no version was reported as having one")
	}
}
