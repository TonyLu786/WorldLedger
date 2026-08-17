package installer

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Applying is bookkeeping first and file writing second.
//
// Every path this touches is written down before it is touched, and anything
// that was already there is copied aside. Undoing is then a replay of that
// record: delete what we made, put back what we moved. Nothing is inferred from
// the state of the disk afterwards, because by then somebody may have installed
// three other mods and the disk no longer says what we did.

// Record is one file this changed, and what it takes to put it back.
type Record struct {
	Path string `json:"path"`
	// Backup is where the previous contents were kept, empty when the file did
	// not exist before. The two cases are undone differently: one is a delete,
	// the other a restore, and treating them alike deletes somebody's file.
	Backup string `json:"backup,omitempty"`
	// Digest is what we wrote, so an uninstall can notice that the file has
	// been changed by something else since and leave it alone.
	Digest string `json:"digest,omitempty"`
}

// Manifest is everything one installation did.
type Manifest struct {
	Schema      string    `json:"schema"`
	InstalledAt time.Time `json:"installed_at"`
	Root        string    `json:"root"`
	Records     []Record  `json:"records"`
	// Directories created, deepest last, so an uninstall can remove the ones it
	// made and only if they are empty.
	Directories []string `json:"directories,omitempty"`
}

const manifestSchema = "worldledger.desktop-install/v1"

// Fetcher gets bytes from a source. It is an interface so the tests can install
// from a directory instead of the network: an installer that can only be tested
// by really downloading things is one that is tested rarely.
type Fetcher interface {
	Fetch(source string) ([]byte, error)
}

// HTTPFetcher is the real one. It also reads a local path, which is how
// somebody working on the project installs a jar they built themselves rather
// than one that has been released.
type HTTPFetcher struct{ Client *http.Client }

func (f HTTPFetcher) Fetch(source string) ([]byte, error) {
	if !strings.HasPrefix(source, "http://") && !strings.HasPrefix(source, "https://") {
		return os.ReadFile(source)
	}
	client := f.Client
	if client == nil {
		client = &http.Client{Timeout: 90 * time.Second}
	}
	response, err := client.Get(source)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s answered %s", source, response.Status)
	}
	// Bounded. A redirect to something enormous should fail rather than fill a
	// disk quietly.
	return io.ReadAll(io.LimitReader(response.Body, 64<<20))
}

// Apply carries out a plan and returns what it did.
//
// A failure part way through is returned with the manifest of everything that
// did happen, so the caller can undo it. Leaving somebody half-installed with
// no record is the one outcome that has no recovery.
func Apply(plan Plan, fetcher Fetcher, backupDir string) (Manifest, error) {
	manifest := Manifest{
		Schema:      manifestSchema,
		InstalledAt: time.Now().UTC(),
		Root:        plan.Root,
	}
	if plan.Refusal != "" {
		return manifest, fmt.Errorf("%s", plan.Refusal)
	}
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		return manifest, fmt.Errorf("could not make a place to keep backups: %w", err)
	}

	for _, step := range plan.Steps {
		var payload []byte
		var err error

		switch step.Kind {
		case WriteLoaderProfile, DownloadMod:
			payload, err = fetcher.Fetch(step.Source)
			if err != nil {
				return manifest, fmt.Errorf("%s: %w", step.Title, err)
			}
			if step.Kind == WriteLoaderProfile && !json.Valid(payload) {
				// A version profile that is not JSON is a version the launcher
				// will refuse, and it fails at the point where somebody is
				// trying to play rather than here.
				return manifest, fmt.Errorf("%s: what arrived is not a version profile", step.Title)
			}
		case WriteContributor:
			payload = captureProperties(plan.Contributor)
		case AddLauncherEntry:
			payload, err = launcherEntry(step.Target, LoaderVersionID())
			if err != nil {
				return manifest, fmt.Errorf("%s: %w", step.Title, err)
			}
		default:
			return manifest, fmt.Errorf("unknown step %q", step.Kind)
		}

		created, err := ensureDir(filepath.Dir(step.Target))
		if err != nil {
			return manifest, fmt.Errorf("%s: %w", step.Title, err)
		}
		manifest.Directories = append(manifest.Directories, created...)

		record, err := writeWithBackup(step.Target, payload, backupDir)
		if err != nil {
			return manifest, fmt.Errorf("%s: %w", step.Title, err)
		}
		manifest.Records = append(manifest.Records, record)
	}
	return manifest, nil
}

// writeWithBackup keeps whatever was there, then replaces it atomically.
func writeWithBackup(path string, payload []byte, backupDir string) (Record, error) {
	record := Record{Path: path}

	if existing, err := os.ReadFile(path); err == nil {
		digest := sha256.Sum256(existing)
		backup := filepath.Join(backupDir, hex.EncodeToString(digest[:])[:16]+"-"+filepath.Base(path))
		if err := os.WriteFile(backup, existing, 0o644); err != nil {
			return record, fmt.Errorf("could not keep a copy of the existing %s: %w", filepath.Base(path), err)
		}
		record.Backup = backup
	} else if !os.IsNotExist(err) {
		return record, err
	}

	// Written beside the target and renamed over it, so an interrupted write
	// cannot leave a half a file where a whole one was.
	temporary := path + ".worldledger-tmp"
	if err := os.WriteFile(temporary, payload, 0o644); err != nil {
		return record, err
	}
	if err := os.Rename(temporary, path); err != nil {
		os.Remove(temporary)
		return record, err
	}

	digest := sha256.Sum256(payload)
	record.Digest = hex.EncodeToString(digest[:])
	return record, nil
}

// ensureDir makes a directory and reports which levels it had to create, so an
// uninstall removes only those.
func ensureDir(dir string) ([]string, error) {
	var created []string
	var missing []string
	for current := dir; ; current = filepath.Dir(current) {
		if _, err := os.Stat(current); err == nil {
			break
		}
		missing = append(missing, current)
		if parent := filepath.Dir(current); parent == current {
			break
		}
	}
	// Shallowest first, which is also the order they have to be made in.
	for i := len(missing) - 1; i >= 0; i-- {
		if err := os.Mkdir(missing[i], 0o755); err != nil && !os.IsExist(err) {
			return created, err
		}
		created = append(created, missing[i])
	}
	return created, nil
}

// Uninstall replays a manifest backwards.
//
// A file that has changed since we wrote it is left alone. Somebody may have
// edited their capture.properties, and removing a mod is not a licence to throw
// away what they wrote afterwards.
func Uninstall(manifest Manifest) ([]string, error) {
	var skipped []string
	for i := len(manifest.Records) - 1; i >= 0; i-- {
		record := manifest.Records[i]

		if current, err := os.ReadFile(record.Path); err == nil && record.Digest != "" {
			digest := sha256.Sum256(current)
			if hex.EncodeToString(digest[:]) != record.Digest {
				skipped = append(skipped, record.Path+" (changed since it was installed)")
				continue
			}
		}

		if record.Backup == "" {
			if err := os.Remove(record.Path); err != nil && !os.IsNotExist(err) {
				return skipped, err
			}
			continue
		}
		previous, err := os.ReadFile(record.Backup)
		if err != nil {
			skipped = append(skipped, record.Path+" (the kept copy could not be read)")
			continue
		}
		if err := os.WriteFile(record.Path, previous, 0o644); err != nil {
			return skipped, err
		}
	}

	// Deepest first, and only when empty: a directory somebody has put their
	// own mods in is not ours to remove.
	for i := len(manifest.Directories) - 1; i >= 0; i-- {
		os.Remove(manifest.Directories[i])
	}
	return skipped, nil
}

// captureProperties is the file the adapter would otherwise write on first run,
// with the one value it cannot work out on its own already filled in.
func captureProperties(contributor string) []byte {
	return []byte("# WorldLedger capture adapter\n" +
		"# Written by the WorldLedger desktop application.\n" +
		"contributor=" + contributor + "\n")
}

// launcherEntry adds an installation without disturbing the ones already there.
//
// The file belongs to the launcher. It is read, added to, and written back
// whole, and the previous contents are kept by the caller, so the worst case is
// a restore rather than a lost list of somebody's installations.
func launcherEntry(path, versionID string) ([]byte, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("the launcher's own settings could not be read: %w", err)
	}
	var document map[string]any
	if err := json.Unmarshal(raw, &document); err != nil {
		return nil, fmt.Errorf("the launcher's own settings are not readable as JSON: %w", err)
	}

	profiles, _ := document["profiles"].(map[string]any)
	if profiles == nil {
		profiles = map[string]any{}
		document["profiles"] = profiles
	}
	now := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	profiles["worldledger-"+versionID] = map[string]any{
		"name":          "WorldLedger (" + versionID + ")",
		"type":          "custom",
		"lastVersionId": versionID,
		"created":       now,
		"lastUsed":      now,
	}
	return json.MarshalIndent(document, "", "  ")
}
