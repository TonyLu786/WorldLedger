package archive

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/worldledger/worldledger-mc/internal/cas"
	"github.com/worldledger/worldledger-mc/internal/model"
)

const FormatVersion = "1"

const transactionDirectory = "transactions"

type Archive struct {
	Root string
	CAS  cas.Store
}

func Init(root string) (Archive, error) {
	if root == "" {
		return Archive{}, errors.New("archive path is required")
	}
	versionPath := filepath.Join(root, "VERSION")
	if _, err := os.Stat(versionPath); err == nil {
		return Open(root)
	} else if !os.IsNotExist(err) {
		return Archive{}, fmt.Errorf("inspect archive version: %w", err)
	}
	for _, dir := range []string{"objects", "observations", "index/chunks", transactionDirectory, purgeDirectory} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			return Archive{}, err
		}
	}
	versionFile, err := os.OpenFile(versionPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if os.IsExist(err) {
		return Open(root)
	}
	if err != nil {
		return Archive{}, fmt.Errorf("create archive version: %w", err)
	}
	removeIncompleteVersion := true
	defer func() {
		if removeIncompleteVersion {
			_ = os.Remove(versionPath)
		}
	}()
	if _, err := versionFile.WriteString(FormatVersion + "\n"); err != nil {
		_ = versionFile.Close()
		return Archive{}, fmt.Errorf("write archive version: %w", err)
	}
	if err := versionFile.Sync(); err != nil {
		_ = versionFile.Close()
		return Archive{}, fmt.Errorf("sync archive version: %w", err)
	}
	if err := versionFile.Close(); err != nil {
		return Archive{}, fmt.Errorf("close archive version: %w", err)
	}
	removeIncompleteVersion = false
	if err := syncDirectory(root); err != nil {
		return Archive{}, fmt.Errorf("sync archive directory: %w", err)
	}
	return Open(root)
}

// ErrNotAnArchive is returned for a directory that is missing, or that exists
// and holds something other than an archive.
var ErrNotAnArchive = errors.New("not a WorldLedger archive")

func Open(root string) (Archive, error) {
	b, err := os.ReadFile(filepath.Join(root, "VERSION"))
	if err != nil {
		// The raw error names VERSION, which is an internal file nobody was told
		// about, and reads as a missing file rather than as the two things that
		// actually happened: the wrong directory, or one that was never
		// initialised. Both have the same fix and it is worth naming.
		if os.IsNotExist(err) {
			return Archive{}, fmt.Errorf(
				"%w: %s\ncreate one with: worldledger init %s", ErrNotAnArchive, root, root)
		}
		return Archive{}, fmt.Errorf("open archive %s: %w", root, err)
	}
	if strings.TrimSpace(string(b)) != FormatVersion {
		return Archive{}, fmt.Errorf("unsupported archive format %q", strings.TrimSpace(string(b)))
	}
	a := Archive{Root: root, CAS: cas.New(filepath.Join(root, "objects"))}
	lock, err := acquireArchiveLock(root)
	if err != nil {
		return Archive{}, fmt.Errorf("lock archive: %w", err)
	}
	defer lock.Close()
	if err := a.recoverTransactions(); err != nil {
		return Archive{}, fmt.Errorf("recover archive transactions: %w", err)
	}
	// An interrupted purge leaves observations the index no longer lists, or
	// index entries pointing at nothing. Both fail the integrity check, so the
	// journal is finished before the archive is handed to anyone.
	if err := a.recoverPurges(); err != nil {
		return Archive{}, fmt.Errorf("recover archive purges: %w", err)
	}
	return a, nil
}

func (a Archive) AddObservation(o model.Observation) error {
	if err := o.Finalize(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(o, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	lock, err := acquireArchiveLock(a.Root)
	if err != nil {
		return fmt.Errorf("lock archive: %w", err)
	}
	defer lock.Close()

	transactionPath := a.transactionPath(o.ID)
	if _, err := os.Stat(transactionPath); err == nil {
		if err := a.recoverTransaction(transactionPath); err != nil {
			return fmt.Errorf("recover existing transaction for %s: %w", o.ID, err)
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(transactionPath), 0o755); err != nil {
		return err
	}
	if err := writeAtomic(transactionPath, data); err != nil {
		return fmt.Errorf("write observation transaction: %w", err)
	}
	if err := a.commitObservation(o, data); err != nil {
		return err
	}
	if err := a.commitIndex(o); err != nil {
		return err
	}
	if err := os.Remove(transactionPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove completed transaction: %w", err)
	}
	if err := syncDirectory(filepath.Dir(transactionPath)); err != nil {
		return err
	}
	return nil
}

func (a Archive) commitObservation(o model.Observation, data []byte) error {
	obsDir := filepath.Join(a.Root, "observations", o.ID[:2])
	if err := os.MkdirAll(obsDir, 0o755); err != nil {
		return err
	}
	obsPath := filepath.Join(obsDir, o.ID+".json")
	if _, err := os.Stat(obsPath); os.IsNotExist(err) {
		if err := writeAtomic(obsPath, data); err != nil {
			return fmt.Errorf("write observation %s: %w", o.ID, err)
		}
		return nil
	} else if err != nil {
		return err
	}
	existing, err := a.readObservation(o.ID)
	if err != nil {
		return fmt.Errorf("read existing observation: %w", err)
	}
	if err := existing.ValidateStored(); err != nil {
		return fmt.Errorf("existing observation %s is invalid: %w", o.ID, err)
	}
	if existing.ID != o.ID {
		return fmt.Errorf("observation file %s contains id %s", o.ID, existing.ID)
	}
	return nil
}

func (a Archive) commitIndex(o model.Observation) error {
	indexPath := a.chunkIndexPath(o.Chunk)
	if err := os.MkdirAll(filepath.Dir(indexPath), 0o755); err != nil {
		return err
	}
	ids, err := readIndexIDs(indexPath)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if id == o.ID {
			return nil
		}
	}
	ids = append(ids, o.ID)
	data := []byte(strings.Join(ids, "\n") + "\n")
	if err := writeAtomic(indexPath, data); err != nil {
		return fmt.Errorf("write chunk index: %w", err)
	}
	return nil
}

func (a Archive) recoverTransactions() error {
	dir := filepath.Join(a.Root, transactionDirectory)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || strings.HasPrefix(entry.Name(), ".tmp-") {
			continue
		}
		if filepath.Ext(entry.Name()) != ".json" {
			return fmt.Errorf("unexpected transaction file %q", entry.Name())
		}
		if err := a.recoverTransaction(filepath.Join(dir, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

func (a Archive) recoverTransaction(transactionPath string) error {
	data, err := os.ReadFile(transactionPath)
	if err != nil {
		return err
	}
	var o model.Observation
	if err := json.Unmarshal(data, &o); err != nil {
		return fmt.Errorf("%s: invalid transaction json: %w", transactionPath, err)
	}
	if err := o.ValidateStored(); err != nil {
		return fmt.Errorf("%s: invalid transaction: %w", transactionPath, err)
	}
	wantName := o.ID + ".json"
	if filepath.Base(transactionPath) != wantName {
		return fmt.Errorf("transaction file %q contains observation %s", filepath.Base(transactionPath), o.ID)
	}
	if err := a.commitObservation(o, data); err != nil {
		return err
	}
	if err := a.commitIndex(o); err != nil {
		return err
	}
	if err := os.Remove(transactionPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return syncDirectory(filepath.Dir(transactionPath))
}

func (a Archive) Observations(chunk model.ChunkRef) ([]model.Observation, error) {
	lock, err := acquireArchiveLock(a.Root)
	if err != nil {
		return nil, fmt.Errorf("lock archive: %w", err)
	}
	defer lock.Close()
	return a.observationsLocked(chunk)
}

// observationsLocked requires the caller to already hold the archive lock. The
// lock is not reentrant: every acquisition opens a separate handle, so a nested
// acquisition blocks against itself on both Windows and POSIX.
func (a Archive) observationsLocked(chunk model.ChunkRef) ([]model.Observation, error) {
	path := a.chunkIndexPath(chunk)
	ids, err := readIndexIDs(path)
	if err != nil {
		return nil, err
	}
	var out []model.Observation
	for _, id := range ids {
		o, err := a.readObservation(id)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ObservedAt.Equal(out[j].ObservedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].ObservedAt.Before(out[j].ObservedAt)
	})
	return out, nil
}

func (a Archive) readObservation(id string) (model.Observation, error) {
	if len(id) < 2 {
		return model.Observation{}, errors.New("invalid observation id")
	}
	data, err := os.ReadFile(filepath.Join(a.Root, "observations", id[:2], id+".json"))
	if err != nil {
		return model.Observation{}, err
	}
	var o model.Observation
	if err := json.Unmarshal(data, &o); err != nil {
		return model.Observation{}, err
	}
	return o, nil
}

func (a Archive) chunkIndexPath(c model.ChunkRef) string {
	return filepath.Join(
		a.Root,
		"index", "chunks",
		safe(c.ServerID),
		safe(c.Dimension),
		strconv.FormatInt(int64(c.X), 10),
		strconv.FormatInt(int64(c.Z), 10)+".idx",
	)
}

func (a Archive) transactionPath(id string) string {
	return filepath.Join(a.Root, transactionDirectory, id+".json")
}

func safe(s string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(model.NormalizeToken(s)))
}

func readIndexIDs(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(data), "\n")
	ids := make([]string, 0, len(lines))
	seen := make(map[string]struct{}, len(lines))
	for lineNumber, line := range lines {
		id := strings.TrimSpace(line)
		if id == "" {
			continue
		}
		if len(id) != 64 || id != strings.ToLower(id) {
			return nil, fmt.Errorf("%s:%d: invalid observation id %q", path, lineNumber+1, id)
		}
		if _, err := hex.DecodeString(id); err != nil {
			return nil, fmt.Errorf("%s:%d: invalid observation id %q", path, lineNumber+1, id)
		}
		if _, exists := seen[id]; exists {
			return nil, fmt.Errorf("%s:%d: duplicate observation id %s", path, lineNumber+1, id)
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids, nil
}

func writeAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Chmod(0o644); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := replaceFile(name, path); err != nil {
		return err
	}
	return syncDirectory(dir)
}
