//go:build !windows

package archive

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

type archiveLock struct {
	file *os.File
}

func acquireArchiveLock(root string) (*archiveLock, error) {
	file, err := os.OpenFile(filepath.Join(root, "archive.lock"), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("acquire file lock: %w", err)
	}
	return &archiveLock{file: file}, nil
}

func (lock *archiveLock) Close() error {
	unlockErr := syscall.Flock(int(lock.file.Fd()), syscall.LOCK_UN)
	closeErr := lock.file.Close()
	if unlockErr != nil {
		return unlockErr
	}
	return closeErr
}
