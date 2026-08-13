//go:build windows

package cas

import (
	"runtime"
	"syscall"
	"unsafe"
)

const moveFileWriteThrough = 0x8

var moveFileExW = syscall.NewLazyDLL("kernel32.dll").NewProc("MoveFileExW")

func commitObject(source, destination string) error {
	sourcePointer, err := syscall.UTF16PtrFromString(source)
	if err != nil {
		return err
	}
	destinationPointer, err := syscall.UTF16PtrFromString(destination)
	if err != nil {
		return err
	}
	result, _, callErr := moveFileExW.Call(
		uintptr(unsafe.Pointer(sourcePointer)),
		uintptr(unsafe.Pointer(destinationPointer)),
		moveFileWriteThrough,
	)
	runtime.KeepAlive(sourcePointer)
	runtime.KeepAlive(destinationPointer)
	if result != 0 {
		return nil
	}
	if callErr != syscall.Errno(0) {
		return callErr
	}
	return syscall.EINVAL
}
