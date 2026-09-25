// Package diagnose assembles what somebody would need in order to help, and
// nothing else.
//
// A person whose capture is not working has no way to say what is wrong, and
// the maintainer has no way to ask: "send me your archive" is not a request
// anybody should make, because an archive is where somebody went and when, and
// who they play with. Nothing here has been able to be asked for.
//
// So the shape of this is decided by what it must not contain rather than by
// what would be useful. Observation content never appears. Neither do server
// names, contributor names, world names, or the identities of anybody who
// signed anything: those are the archive's subject matter, and a support
// request is not a reason to hand them over. What is left is counts, versions,
// and the kinds of error the integrity check reported, which is enough to tell
// an unreadable spool from a missing mod from a broken index, and is the
// question support is actually being asked.
//
// Paths are the awkward case. They are genuinely useful, because half of what
// goes wrong is a file in the wrong place, and on Windows a path carries the
// person's account name. They are kept with the home directory replaced by a
// placeholder, which keeps the shape and drops the name.
//
// None of that is airtight and it is not meant to be. A directory somebody
// named after themselves, in a shape no rule here predicts, still carries their
// name, and no amount of masking closes that. Which is why the command prints
// the whole thing to the screen before it writes anything: a support file whose
// contents are taken on trust is one nobody should send, and reading it is the
// protection that does not depend on this package having thought of everything.
//
// Nothing here is sent anywhere. It is written to a file the person chooses, so
// that handing it over stays something they do.
package diagnose

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/worldledger/worldledger-mc/internal/archive"
	"github.com/worldledger/worldledger-mc/internal/spool"
)

const Schema = "worldledger.diagnosis/v1"

// Report is the whole of what is written.
//
// Every field here is a count, a version, a kind, or a masked path. If a field
// is ever added that is none of those, it is a field that hands over somebody's
// data, and the test beside this package is written to notice.
type Report struct {
	Schema     string    `json:"schema"`
	TakenAt    time.Time `json:"taken_at"`
	Tool       Tool      `json:"tool"`
	Machine    Machine   `json:"machine"`
	Archive    *Archive  `json:"archive,omitempty"`
	Spool      *Spool    `json:"spool,omitempty"`
	NotesForNo []string  `json:"could_not_be_read,omitempty"`
}

type Tool struct {
	Version string `json:"version"`
}

type Machine struct {
	OS      string `json:"os"`
	Arch    string `json:"arch"`
	Go      string `json:"go"`
	NumCPU  int    `json:"cpus"`
	HasHome bool   `json:"home_directory_found"`
}

// Archive is the shape of what is stored, never any of it.
type Archive struct {
	Path          string `json:"path"`
	FormatVersion string `json:"format_version"`
	Observations  int    `json:"observations"`
	Objects       int    `json:"objects"`
	// Servers and Dimensions are counted and not named. How many a person has
	// is a useful number; which ones is their business and the subject of the
	// archive.
	Servers    int `json:"servers"`
	Dimensions int `json:"dimensions"`
	Chunks     int `json:"chunks"`
	// Policies, Redactions and Identities are counted for the same reason. A
	// declaration that exists is a fact about the setup; who declared what is
	// not.
	Policies   int `json:"policies"`
	Redactions int `json:"redactions"`
	Identities int `json:"identities"`
	// CheckErrors are the kinds the integrity check reported, deduplicated,
	// with the ids and paths taken out. "object %s is corrupt" is what support
	// needs; which object is not.
	CheckErrors []string `json:"check_error_kinds,omitempty"`
	CheckTotal  int      `json:"check_error_total"`
}

// Spool is what the capture adapter has left behind.
type Spool struct {
	Path        string `json:"path"`
	Ready       int    `json:"ready"`
	Imported    int    `json:"imported"`
	Quarantined int    `json:"quarantined"`
	InProgress  int    `json:"in_progress"`
}

// Take assembles a report. Either path may be empty, in which case that part is
// left out rather than guessed at.
func Take(toolVersion, archivePath, spoolPath string) Report {
	report := Report{
		Schema:  Schema,
		TakenAt: time.Now().UTC(),
		Tool:    Tool{Version: toolVersion},
		Machine: machine(),
	}
	if archivePath != "" {
		if described, err := describeArchive(archivePath); err != nil {
			report.NotesForNo = append(report.NotesForNo, "archive: "+maskPath(err.Error()))
		} else {
			report.Archive = described
		}
	}
	if spoolPath != "" {
		if described, err := describeSpool(spoolPath); err != nil {
			report.NotesForNo = append(report.NotesForNo, "spool: "+maskPath(err.Error()))
		} else {
			report.Spool = described
		}
	}
	return report
}

func machine() Machine {
	home, err := os.UserHomeDir()
	return Machine{
		OS:      runtime.GOOS,
		Arch:    runtime.GOARCH,
		Go:      runtime.Version(),
		NumCPU:  runtime.NumCPU(),
		HasHome: err == nil && home != "",
	}
}

func describeArchive(path string) (*Archive, error) {
	a, err := archive.Open(path)
	if err != nil {
		return nil, err
	}
	out := &Archive{Path: maskPath(path), FormatVersion: archive.FormatVersion}

	manifest, err := a.Manifest()
	if err != nil {
		return nil, err
	}
	out.Observations = manifest.Observations
	out.Objects = manifest.Objects
	out.Servers = len(manifest.Servers)
	for _, server := range manifest.Servers {
		out.Dimensions += len(server.Dimensions)
		for _, dimension := range server.Dimensions {
			out.Chunks += dimension.Chunks
		}
	}

	out.Policies = countFiles(filepath.Join(path, "policy"), ".json")
	out.Redactions = countFiles(filepath.Join(path, "policy", "redactions"), ".json")
	out.Identities = countFiles(filepath.Join(path, "identities"), ".json")

	check := a.Check()
	out.CheckTotal = len(check.Errors)
	out.CheckErrors = kindsOf(check.Errors)
	return out, nil
}

func describeSpool(path string) (*Spool, error) {
	contents, err := spool.Read(path)
	if err != nil {
		return nil, err
	}
	return &Spool{
		Path:        maskPath(path),
		Ready:       len(contents.Ready),
		Imported:    len(contents.Imported),
		Quarantined: contents.Quarantined,
		InProgress:  contents.InProgress,
	}, nil
}

func countFiles(dir, suffix string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	count := 0
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), suffix) {
			count++
		}
	}
	return count
}

// kindsOf reduces the integrity check's messages to their shapes.
//
// "object 4f2a... is corrupt" and "object 9c11... is corrupt" are one thing
// support needs to know about and two strings carrying digests. Removing what
// varies leaves the kind, which is the part anybody can act on, and drops the
// identifiers, which name what somebody observed.
func kindsOf(errors []string) []string {
	seen := map[string]struct{}{}
	for _, message := range errors {
		seen[maskPath(hexMasked(message))] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for kind := range seen {
		out = append(out, kind)
	}
	sort.Strings(out)
	return out
}

// hexMasked replaces every run of hexadecimal long enough to be an identifier.
//
// Twelve characters, because that is longer than any word and shorter than the
// shortest thing this program abbreviates an id to. A chunk coordinate, a
// count, and an ordinary English word all survive.
func hexMasked(message string) string {
	const identifierLength = 12
	var out strings.Builder
	start := -1
	for i := 0; i <= len(message); i++ {
		if i < len(message) && isHexDigit(message[i]) {
			if start < 0 {
				start = i
			}
			continue
		}
		if start >= 0 {
			if i-start >= identifierLength {
				out.WriteString("<id>")
			} else {
				out.WriteString(message[start:i])
			}
			start = -1
		}
		if i < len(message) {
			out.WriteByte(message[i])
		}
	}
	return out.String()
}

func isHexDigit(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

// maskPath takes the account name out of a path and leaves the shape.
//
// A path is genuinely useful, because half of what goes wrong is a file in the
// wrong place, and on Windows it carries the person's account name. Keeping the
// shape and dropping the name is what both of those want.
//
// Replacing the home directory as a string is not enough, and running this
// found out why: a temporary directory arrived as C:/Users/JUNTON~1/..., the
// short form Windows keeps for a name with a space in it. It is the same
// directory and a different string, so the substring never matched and the name
// went out anyway.
//
// So the account name is masked wherever it stands, rather than only as part of
// the one path this process happens to know. The segment after Users is an
// account name whoever it belongs to, which also covers a second person's
// directory appearing in somebody's error message.
// accountVariants is the account name as it is, and as a directory names it.
//
// The first real run of this command leaked one. The account was "Juntong Lu"
// and the path held C--Users-Juntong-Lu-Desktop-..., which is the same name
// with the space written as a hyphen, so the substring never matched and a
// real person's real name went into the file the command opens by saying
// "this is everything it would hand over". The segment after Users was masked
// correctly; that was never the only place a name appears.
//
// Only whole-name spellings are covered. Masking the parts on their own would
// catch a directory called after somebody's first name, and would also take
// out every ordinary word that happens to be somebody's surname, in a report
// whose value is that it can still be read. Where the line falls is worth
// saying rather than leaving to be discovered: this catches the name, not the
// pieces of it.
func accountVariants(account string) []string {
	separators := func(r rune) bool {
		return r == ' ' || r == '-' || r == '_' || r == '.'
	}
	parts := strings.FieldsFunc(account, separators)
	if len(parts) < 2 {
		return []string{account}
	}
	// Longest first, so a spelling that contains another is taken out whole
	// rather than in pieces.
	variants := []string{account}
	for _, separator := range []string{" ", "-", "_", ".", ""} {
		if joined := strings.Join(parts, separator); joined != account {
			variants = append(variants, joined)
		}
	}
	sort.SliceStable(variants, func(i, j int) bool {
		return len(variants[i]) > len(variants[j])
	})
	return variants
}

func maskPath(text string) string {
	masked := text
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		masked = replaceFold(masked, home, "<home>")
		// Both separators, because a path can arrive either way on Windows.
		if other := strings.ReplaceAll(home, string(filepath.Separator), "/"); other != home {
			masked = replaceFold(masked, other, "<home>")
		}
		// And the account name wherever it stands, which is how it survives a
		// directory somebody named after themselves, in any of the spellings a
		// directory gives it.
		for _, variant := range accountVariants(filepath.Base(home)) {
			if len(variant) > 2 {
				masked = replaceFold(masked, variant, "<user>")
			}
		}
	}
	return maskAccountSegments(masked)
}

// maskAccountSegments replaces whatever follows a Users or home component.
//
// One pass, left to right. The first version searched repeatedly and rewrote
// what it had found in order to step over it, which is a loop that does not
// terminate the moment the search is case-insensitive and the rewrite only
// changes case. It ran for sixty-five seconds before a test with a deadline
// said so.
func maskAccountSegments(text string) string {
	lower := strings.ToLower(text)
	var out strings.Builder
	i := 0
	for i < len(text) {
		marker, found := accountMarkerAt(lower, i)
		if !found {
			out.WriteByte(text[i])
			i++
			continue
		}
		out.WriteString(text[i : i+len(marker)])
		i += len(marker)
		start := i
		for i < len(text) && text[i] != '\\' && text[i] != '/' {
			i++
		}
		if i == start {
			// Nothing follows, so there is no name here to take out.
			continue
		}
		if segment := text[start:i]; segment == "<home>" || segment == "<user>" {
			out.WriteString(segment)
			continue
		}
		out.WriteString("<user>")
	}
	return out.String()
}

// accountMarkerAt reports whether one of the directories that holds accounts
// begins at this position, and how long it is.
func accountMarkerAt(lower string, at int) (string, bool) {
	for _, name := range []string{"users", "home"} {
		for _, separator := range []string{"\\", "/"} {
			marker := name + separator
			if strings.HasPrefix(lower[at:], marker) {
				// Only when it is a whole path component, so "homework/" is not
				// mistaken for somebody's home.
				if at == 0 || lower[at-1] == '\\' || lower[at-1] == '/' {
					return marker, true
				}
			}
		}
	}
	return "", false
}

func indexFold(haystack, needle string) int {
	return strings.Index(strings.ToLower(haystack), strings.ToLower(needle))
}

func replaceFold(text, old, replacement string) string {
	if old == "" {
		return text
	}
	var out strings.Builder
	for {
		at := indexFold(text, old)
		if at < 0 {
			out.WriteString(text)
			return out.String()
		}
		out.WriteString(text[:at])
		out.WriteString(replacement)
		text = text[at+len(old):]
	}
}
