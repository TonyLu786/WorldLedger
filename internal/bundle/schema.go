package bundle

import (
	"encoding/json"
	"strconv"
	"strings"
)

// What a bundle from the wrong side of a version says about itself.
//
// The mod and the core are downloaded separately. A player updates one and not
// the other, or updates both and the mod arrives first, and either way somebody
// is holding two halves of different ages. That is not an unusual state and it
// is not anybody's mistake.
//
// It was already reported rather than swallowed: the terminal lists every
// bundle that would not import and exits non-zero, and the window puts them on
// the import screen. What it was not was readable. Rejecting the manifest's
// unknown fields happened before anything looked at the schema, so a bundle
// from a later adapter was refused with a sentence about JSON, which is true
// and is about the wrong subject: their actual situation is that their two
// programs are different ages and one of them needs updating.
//
// So the schema is read first and on its own, out of a manifest nothing has
// validated yet, precisely so that a mismatch can be explained before anything
// else has a chance to fail on a symptom of it.

// schemaFamily and schemaVersion split "worldledger.capture-bundle/v1".
func splitSchema(schema string) (family string, version int, ok bool) {
	at := strings.LastIndex(schema, "/v")
	if at < 0 {
		return schema, 0, false
	}
	number, err := strconv.Atoi(schema[at+2:])
	if err != nil || number < 0 {
		return schema[:at], 0, false
	}
	return schema[:at], number, true
}

// declaredSchema reads only the schema string, from a manifest that may be from
// a version this build knows nothing about.
//
// Deliberately tolerant, and tolerant of exactly one thing. It decodes into a
// single field and ignores everything else, so a later manifest with fields
// this build has never heard of still yields the one value needed to say so.
// Nothing read here is trusted for anything except choosing the wording of a
// refusal.
func declaredSchema(data []byte) string {
	var envelope struct {
		Schema string `json:"schema"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return ""
	}
	return envelope.Schema
}

// schemaRefusal says which way a version mismatch runs, and what to do.
//
// Which half is behind is knowable here and nowhere else, and it decides the
// whole of the answer: one of them means update WorldLedger and the other means
// the capture came from something older than this build supports.
func schemaRefusal(found string) error {
	if strings.TrimSpace(found) == "" {
		return invalidf("bundle.json does not say what it is: it declares no schema, " +
			"so it is either not a capture bundle or was not finished being written")
	}

	wantFamily, wantVersion, _ := splitSchema(Schema)
	family, version, ok := splitSchema(found)
	if family != wantFamily || !ok {
		return invalidf("this is not a capture bundle: it declares %q, and a capture bundle declares %q",
			found, Schema)
	}
	if version > wantVersion {
		return invalidf("this capture is version %d and this build of WorldLedger reads version %d. "+
			"It was written by a later WorldLedger mod, so the mod and the application are "+
			"different ages: updating WorldLedger is what reads it. Nothing has been lost, "+
			"and the capture is still where it was",
			version, wantVersion)
	}
	return invalidf("this capture is version %d and this build of WorldLedger reads version %d. "+
		"It was written by an earlier WorldLedger mod than this application expects, "+
		"so updating the mod is what makes new captures import. Nothing has been lost, "+
		"and the capture is still where it was",
		version, wantVersion)
}

// unknownFieldRefusal explains a manifest that is the right version and carries
// something this build does not know.
//
// The schema said one thing and the contents said another, which is a mod that
// added a field without saying so rather than a version this build is behind.
// It is worth separating from an honest version mismatch, because the actions
// are different: one of them is updating something and the other is a bug in
// whatever wrote the bundle.
func unknownFieldRefusal(err error) error {
	message := err.Error()
	if !strings.Contains(message, "unknown field") {
		return invalidf("bundle.json: %v", message)
	}
	return invalidf("bundle.json carries something this build does not know (%v), "+
		"while declaring the version it does know (%s). A capture that adds a field "+
		"has to say so with a new version, so this was written by something that did not",
		message, Schema)
}
