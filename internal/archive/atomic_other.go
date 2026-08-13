//go:build !windows

package archive

import "os"

func replaceFile(source, destination string) error {
	return os.Rename(source, destination)
}

func syncDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
