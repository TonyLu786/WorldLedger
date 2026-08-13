package cas

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/worldledger/worldledger-mc/internal/model"
)

func TestVerifyDetectsObjectCorruption(t *testing.T) {
	store := New(t.TempDir())
	ref, err := store.Put(strings.NewReader("original"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Verify(ref); err != nil {
		t.Fatalf("clean object failed verification: %v", err)
	}
	if err := os.WriteFile(store.Path(ref), []byte("tampered"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := store.Verify(ref); err == nil {
		t.Fatal("expected corrupted object to fail verification")
	}
}

func TestPutVerifiedRejectsMismatchBeforeCommit(t *testing.T) {
	root := t.TempDir()
	store := New(root)
	expectedBytes := []byte("expected")
	sum := sha256.Sum256(expectedBytes)
	expected := model.BlobRef{
		Algorithm: "sha256",
		Digest:    hex.EncodeToString(sum[:]),
		Size:      int64(len(expectedBytes)),
	}
	if _, err := store.PutVerified(strings.NewReader("different"), expected); !errors.Is(err, ErrObjectMismatch) {
		t.Fatalf("expected mismatched object error, got %v", err)
	}
	if _, err := os.Stat(store.Path(expected)); !os.IsNotExist(err) {
		t.Fatalf("mismatched object became visible in CAS: %v", err)
	}
}

func TestPutVerifiedRejectsCorruptExistingObject(t *testing.T) {
	store := New(t.TempDir())
	ref, err := store.Put(strings.NewReader("original"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.Path(ref), []byte("tampered"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := store.PutVerified(strings.NewReader("original"), ref); err == nil {
		t.Fatal("expected corrupt existing object to block import")
	}
}
