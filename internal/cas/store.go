package cas

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/worldledger/worldledger-mc/internal/model"
)

var ErrObjectMismatch = errors.New("object does not match expected reference")

type Store struct {
	root string
}

func New(root string) Store {
	return Store{root: root}
}

func (s Store) Put(r io.Reader) (model.BlobRef, error) {
	return s.put(r, nil)
}

// PutVerified stores r only when its exact bytes match expected. A mismatch is
// rejected before the temporary object is made visible in CAS.
func (s Store) PutVerified(r io.Reader, expected model.BlobRef) (model.BlobRef, error) {
	if expected.Algorithm != "sha256" || len(expected.Digest) != sha256.Size*2 || expected.Digest != strings.ToLower(expected.Digest) {
		return model.BlobRef{}, fmt.Errorf("invalid expected SHA-256 reference")
	}
	if _, err := hex.DecodeString(expected.Digest); err != nil {
		return model.BlobRef{}, fmt.Errorf("invalid expected SHA-256 digest: %w", err)
	}
	if expected.Size < 0 || expected.Size == math.MaxInt64 {
		return model.BlobRef{}, fmt.Errorf("invalid expected object size %d", expected.Size)
	}
	return s.put(r, &expected)
}

func (s Store) put(r io.Reader, expected *model.BlobRef) (model.BlobRef, error) {
	tmpDir := filepath.Join(s.root, "tmp")
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return model.BlobRef{}, err
	}

	f, err := os.CreateTemp(tmpDir, "object-*")
	if err != nil {
		return model.BlobRef{}, err
	}
	tmpName := f.Name()
	defer os.Remove(tmpName)

	h := sha256.New()
	input := r
	if expected != nil {
		input = io.LimitReader(r, expected.Size+1)
	}
	n, copyErr := io.Copy(io.MultiWriter(f, h), input)
	syncErr := f.Sync()
	closeErr := f.Close()
	if copyErr != nil {
		return model.BlobRef{}, copyErr
	}
	if syncErr != nil {
		return model.BlobRef{}, syncErr
	}
	if closeErr != nil {
		return model.BlobRef{}, closeErr
	}

	digest := hex.EncodeToString(h.Sum(nil))
	ref := model.BlobRef{Algorithm: "sha256", Digest: digest, Size: n}
	if expected != nil && ref != *expected {
		return model.BlobRef{}, fmt.Errorf(
			"%w: have sha256:%s size %d, want sha256:%s size %d",
			ErrObjectMismatch,
			ref.Digest, ref.Size, expected.Digest, expected.Size,
		)
	}
	dst := s.Path(ref)
	if _, err := os.Stat(dst); err == nil {
		if err := s.Verify(ref); err != nil {
			return model.BlobRef{}, fmt.Errorf("existing object is corrupt: %w", err)
		}
		return ref, nil
	} else if !os.IsNotExist(err) {
		return model.BlobRef{}, err
	}

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return model.BlobRef{}, err
	}
	if err := commitObject(tmpName, dst); err != nil {
		if _, statErr := os.Stat(dst); statErr == nil {
			if verifyErr := s.Verify(ref); verifyErr == nil {
				return ref, nil
			}
		}
		return model.BlobRef{}, fmt.Errorf("commit object: %w", err)
	}
	return ref, nil
}

func (s Store) Path(ref model.BlobRef) string {
	if len(ref.Digest) < 4 {
		return ""
	}
	return filepath.Join(s.root, "sha256", ref.Digest[:2], ref.Digest[2:4], ref.Digest)
}

func (s Store) Open(ref model.BlobRef) (*os.File, error) {
	return os.Open(s.Path(ref))
}

// Remove deletes an object. It reports whether anything was there, because a
// caller sweeping objects that no observation references any longer needs to
// distinguish a deletion from an object that was already gone, and neither is
// an error.
//
// The store has no idea how many observations point at an object, so it cannot
// refuse a removal that would break one. Deciding that is the caller's job.
func (s Store) Remove(ref model.BlobRef) (bool, error) {
	path := s.Path(ref)
	if path == "" {
		return false, fmt.Errorf("refusing to remove an object with digest %q", ref.Digest)
	}
	err := os.Remove(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (s Store) Verify(ref model.BlobRef) error {
	f, err := s.Open(ref)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return err
	}
	if n != ref.Size {
		return fmt.Errorf("size mismatch: have %d want %d", n, ref.Size)
	}
	digest := hex.EncodeToString(h.Sum(nil))
	if digest != ref.Digest {
		return fmt.Errorf("digest mismatch: have %s want %s", digest, ref.Digest)
	}
	return nil
}
