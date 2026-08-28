package api

import (
	"testing"
	"time"

	"github.com/worldledger/worldledger-mc/desktop/internal/installer"
)

// A plan only contains steps for what is missing, so a second set-up is usually
// one step. Writing that over the record is how Remove came to remove a single
// jar and report "Your Minecraft is back to what it was" -- and the play screen
// sends people back to Set up for exactly the case that causes it, a launcher
// that replaced the mods folder.

func recordAt(path, backup, digest string) installer.Record {
	return installer.Record{Path: path, Backup: backup, Digest: digest}
}

func TestASecondSetUpKeepsWhatTheFirstOneRecorded(t *testing.T) {
	first := installer.Manifest{
		InstalledAt: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		Records: []installer.Record{
			recordAt("versions/fabric.json", "", "aaa"),
			recordAt("launcher_profiles.json", "backups/launcher.json", "bbb"),
			recordAt("mods/fabric-api.jar", "", "ccc"),
			recordAt("mods/worldledger.jar", "", "ddd"),
		},
		Directories: []string{"mods"},
	}
	second := installer.Manifest{
		InstalledAt: time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC),
		Records:     []installer.Record{recordAt("mods/worldledger.jar", "", "eee")},
	}

	merged := mergeManifests(first, second)

	if len(merged.Records) != 4 {
		t.Fatalf("the merged record holds %d file(s), want the four the first set-up wrote", len(merged.Records))
	}
	byPath := map[string]installer.Record{}
	for _, record := range merged.Records {
		byPath[record.Path] = record
	}
	for _, path := range []string{"versions/fabric.json", "launcher_profiles.json", "mods/fabric-api.jar"} {
		if _, kept := byPath[path]; !kept {
			t.Errorf("%s was dropped, so Remove could no longer undo it", path)
		}
	}
	// The digest has to be the new one: an uninstall compares it against what is
	// on disk and leaves anything that does not match alone, so keeping the old
	// digest would make the reinstalled file look like somebody else's.
	if got := byPath["mods/worldledger.jar"].Digest; got != "eee" {
		t.Errorf("the reinstalled file carries digest %q, want the one just written", got)
	}
	if merged.Directories[0] != "mods" {
		t.Errorf("directories were lost: %v", merged.Directories)
	}
	if !merged.InstalledAt.Equal(first.InstalledAt) {
		t.Errorf("installed at %s, want the first time this touched that Minecraft", merged.InstalledAt)
	}
}

// The backup is the other half, and it goes the other way. A later backup is a
// copy of our own previous install; restoring that would leave the player with
// our file rather than the one they had before this application existed.
func TestTheOldestBackupIsTheOneKept(t *testing.T) {
	first := installer.Manifest{
		Records: []installer.Record{recordAt("config/capture.properties", "backups/theirs.properties", "aaa")},
	}
	second := installer.Manifest{
		Records: []installer.Record{recordAt("config/capture.properties", "backups/ours.properties", "bbb")},
	}
	merged := mergeManifests(first, second)
	if len(merged.Records) != 1 {
		t.Fatalf("%d record(s) for one file", len(merged.Records))
	}
	if merged.Records[0].Backup != "backups/theirs.properties" {
		t.Errorf("backup is %q, want the one taken before this application first wrote there",
			merged.Records[0].Backup)
	}
	if merged.Records[0].Digest != "bbb" {
		t.Errorf("digest is %q, want what is on disk now", merged.Records[0].Digest)
	}
}

func TestMergingWithNothingHeldIsTheFreshManifest(t *testing.T) {
	fresh := installer.Manifest{Records: []installer.Record{recordAt("a", "", "x")}}
	merged := mergeManifests(installer.Manifest{}, fresh)
	if len(merged.Records) != 1 || merged.Records[0].Path != "a" {
		t.Errorf("a first install was altered by merging: %+v", merged.Records)
	}
}
