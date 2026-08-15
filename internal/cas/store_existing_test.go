package cas

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/worldledger/worldledger-mc/internal/model"
)

// PutVerified gained a path for objects the store already holds, which skips
// writing but must not skip checking. It is the path that could accept bytes
// nobody looked at, because the answer it returns was already on disk.

func TestPutVerifiedRejectsWrongBytesWhenTheObjectAlreadyExists(t *testing.T) {
	store := New(t.TempDir())
	ref, err := store.Put(strings.NewReader("the real bytes"))
	if err != nil {
		t.Fatal(err)
	}

	// A second contributor offers a component declaring the same digest while
	// its file holds something else. The store already has the right object, so
	// nothing needs writing, and that is exactly when a missing check would go
	// unnoticed.
	_, err = store.PutVerified(strings.NewReader("different bytes entirely"), ref)
	if err == nil {
		t.Fatal("bytes that do not match the declared digest were accepted because the object already existed")
	}
	if !errors.Is(err, ErrObjectMismatch) {
		t.Fatalf("expected a mismatch, got %v", err)
	}

	// The stored object is untouched by the rejected attempt.
	if err := store.Verify(ref); err != nil {
		t.Fatalf("a rejected put damaged the object already stored: %v", err)
	}
}

func TestPutVerifiedAcceptsMatchingBytesWhenTheObjectAlreadyExists(t *testing.T) {
	store := New(t.TempDir())
	payload := []byte("the real bytes")
	ref, err := store.Put(bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	again, err := store.PutVerified(bytes.NewReader(payload), ref)
	if err != nil {
		t.Fatal(err)
	}
	if again != ref {
		t.Fatalf("a repeated put returned %+v, want %+v", again, ref)
	}
}

// A truncated stream hashes to something else, so it must be refused even
// though the digest it claims is one the store holds.
func TestPutVerifiedRejectsAShortStreamWhenTheObjectAlreadyExists(t *testing.T) {
	store := New(t.TempDir())
	ref, err := store.Put(strings.NewReader("the real bytes"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.PutVerified(strings.NewReader("the real"), ref); !errors.Is(err, ErrObjectMismatch) {
		t.Fatalf("expected a mismatch for a truncated stream, got %v", err)
	}
}

// An oversized stream must not be read past the size it promised, and must not
// be accepted on the strength of a prefix that hashes correctly up to a point.
func TestPutVerifiedRejectsALongStreamWhenTheObjectAlreadyExists(t *testing.T) {
	store := New(t.TempDir())
	ref, err := store.Put(strings.NewReader("the real bytes"))
	if err != nil {
		t.Fatal(err)
	}
	longer := model.BlobRef{Algorithm: ref.Algorithm, Digest: ref.Digest, Size: ref.Size}
	if _, err := store.PutVerified(strings.NewReader("the real bytes and more"), longer); !errors.Is(err, ErrObjectMismatch) {
		t.Fatalf("expected a mismatch for an overlong stream, got %v", err)
	}
}
