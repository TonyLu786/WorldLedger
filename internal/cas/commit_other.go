//go:build !windows

package cas

import (
	"os"
	"path/filepath"
)

func commitObject(source, destination string) error {
	if err := os.Rename(source, destination); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(destination))
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
