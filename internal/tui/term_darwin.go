//go:build darwin

package tui

import (
	"syscall"
	"unsafe"
)

// isTTY reports whether the file descriptor refers to a terminal.
func isTTY(fd uintptr) bool {
	return IsTTY(fd)
}

// IsTTY reports whether the file descriptor refers to a terminal (exported).
func IsTTY(fd uintptr) bool {
	var t syscall.Termios
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, syscall.TIOCGETA, uintptr(unsafe.Pointer(&t)))
	return errno == 0
}

// setRaw puts the terminal into raw input mode and returns the original state.
func setRaw(fd uintptr) (syscall.Termios, error) {
	var old syscall.Termios
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, syscall.TIOCGETA, uintptr(unsafe.Pointer(&old))); errno != 0 {
		return old, errno
	}
	raw := old
	raw.Lflag &^= syscall.ICANON | syscall.ECHO | syscall.ISIG | syscall.IEXTEN
	raw.Iflag &^= syscall.ICRNL | syscall.IXON
	raw.Cc[syscall.VMIN] = 1
	raw.Cc[syscall.VTIME] = 0
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, syscall.TIOCSETA, uintptr(unsafe.Pointer(&raw))); errno != 0 {
		return old, errno
	}
	return old, nil
}

func restoreTermios(fd uintptr, t syscall.Termios) error {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, syscall.TIOCSETA, uintptr(unsafe.Pointer(&t)))
	if errno != 0 {
		return errno
	}
	return nil
}
