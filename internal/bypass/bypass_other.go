//go:build !windows

package bypass

// bind does nothing: Set is only called on Windows.
func bind(uintptr, string, int) error { return nil }
