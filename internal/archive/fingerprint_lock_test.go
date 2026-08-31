package archive

import (
	"testing"
	"time"
)

// A fingerprint is compared byte for byte against another machine's, and it is
// what a mirror is told to send from. Both of those need it to describe one
// state of the archive rather than several stitched together, and it used to
// take and give back the lock once per server and once per dimension, leaving
// the archive open in between.
//
// The archive lock is not reentrant on either platform, so this also fails by
// hanging if the walk ever goes back to calling the exported enumerators.
func TestAFingerprintWaitsForTheArchiveRatherThanReadingItPiecemeal(t *testing.T) {
	dir := t.TempDir()
	a, err := Init(dir)
	if err != nil {
		t.Fatal(err)
	}
	storedObservation(t, a, "minecraft:overworld", 0, 0, "alice", 1, "one")

	held, err := acquireArchiveLock(dir)
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := a.Fingerprint("")
		done <- err
	}()

	// Long enough to be sure it is waiting rather than slow. If the walk did
	// not take the lock it would have finished this small archive in
	// microseconds.
	select {
	case err := <-done:
		held.Close()
		t.Fatalf("a fingerprint was taken while the archive was locked by somebody else (err=%v)", err)
	case <-time.After(250 * time.Millisecond):
	}

	if err := held.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("the fingerprint failed once the archive was free: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the fingerprint never completed after the archive was unlocked")
	}
}
