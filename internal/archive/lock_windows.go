//go:build windows

package archive

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"unsafe"
)

const lockfileExclusiveLock = 0x00000002

var (
	kernel32Locking = syscall.NewLazyDLL("kernel32.dll")
	lockFileEx      = kernel32Locking.NewProc("LockFileEx")
	unlockFileEx    = kernel32Locking.NewProc("UnlockFileEx")
)

type archiveLock struct {
	file       *os.File
	overlapped syscall.Overlapped
}

func acquireArchiveLock(root string) (*archiveLock, error) {
	file, err := os.OpenFile(filepath.Join(root, "archive.lock"), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	lock := &archiveLock{file: file}
	result, _, callErr := lockFileEx.Call(
		file.Fd(),
		lockfileExclusiveLock,
		0,
		1,
		0,
		uintptr(unsafe.Pointer(&lock.overlapped)),
	)
	runtime.KeepAlive(file)
	if result == 0 {
		_ = file.Close()
		if callErr == syscall.Errno(0) {
			callErr = syscall.EINVAL
		}
		return nil, fmt.Errorf("LockFileEx: %w", callErr)
	}
	return lock, nil
}

func (lock *archiveLock) Close() error {
	result, _, callErr := unlockFileEx.Call(
		lock.file.Fd(),
		0,
		1,
		0,
		uintptr(unsafe.Pointer(&lock.overlapped)),
	)
	runtime.KeepAlive(lock.file)
	closeErr := lock.file.Close()
	if result == 0 {
		if callErr == syscall.Errno(0) {
			callErr = syscall.EINVAL
		}
		return fmt.Errorf("UnlockFileEx: %w", callErr)
	}
	return closeErr
}
