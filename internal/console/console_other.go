//go:build !windows

package console

// SetupUTF8 is a no-op on non-Windows systems
func SetupUTF8() error {
	return nil
}

// GetConsoleCodePage returns 0 on non-Windows systems
func GetConsoleCodePage() uint32 {
	return 0
}

// EnableVirtualTerminal is a no-op on non-Windows systems
func EnableVirtualTerminal() error {
	return nil
}

