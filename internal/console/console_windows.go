//go:build windows

package console

import (
	"syscall"
	"unsafe"
)

var (
	kernel32           = syscall.NewLazyDLL("kernel32.dll")
	setConsoleOutputCP = kernel32.NewProc("SetConsoleOutputCP")
	setConsoleCP       = kernel32.NewProc("SetConsoleCP")
	getConsoleCP       = kernel32.NewProc("GetConsoleCP")
)

const CP_UTF8 = 65001

// SetupUTF8 configures the Windows console to use UTF-8
func SetupUTF8() error {
	// Set input code page to UTF-8
	r1, _, err := setConsoleCP.Call(uintptr(CP_UTF8))
	if r1 == 0 {
		return err
	}

	// Set output code page to UTF-8
	r1, _, err = setConsoleOutputCP.Call(uintptr(CP_UTF8))
	if r1 == 0 {
		return err
	}

	return nil
}

// GetConsoleCodePage returns the current console input code page
func GetConsoleCodePage() uint32 {
	r1, _, _ := getConsoleCP.Call()
	return uint32(r1)
}

// EnableVirtualTerminal enables virtual terminal processing for better unicode support
func EnableVirtualTerminal() error {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	getStdHandle := kernel32.NewProc("GetStdHandle")
	getConsoleMode := kernel32.NewProc("GetConsoleMode")
	setConsoleMode := kernel32.NewProc("SetConsoleMode")

	const (
		STD_OUTPUT_HANDLE               = ^uintptr(10) // -11
		STD_INPUT_HANDLE                = ^uintptr(9)  // -10
		ENABLE_VIRTUAL_TERMINAL_INPUT   = 0x0200
		ENABLE_VIRTUAL_TERMINAL_PROCESSING = 0x0004
	)

	// Enable for stdout
	handle, _, _ := getStdHandle.Call(STD_OUTPUT_HANDLE)
	var mode uint32
	getConsoleMode.Call(handle, uintptr(unsafe.Pointer(&mode)))
	mode |= ENABLE_VIRTUAL_TERMINAL_PROCESSING
	setConsoleMode.Call(handle, uintptr(mode))

	// Enable for stdin
	handle, _, _ = getStdHandle.Call(STD_INPUT_HANDLE)
	getConsoleMode.Call(handle, uintptr(unsafe.Pointer(&mode)))
	mode |= ENABLE_VIRTUAL_TERMINAL_INPUT
	setConsoleMode.Call(handle, uintptr(mode))

	return nil
}

