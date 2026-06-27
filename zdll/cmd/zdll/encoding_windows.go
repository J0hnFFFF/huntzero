//go:build windows

package main

import (
	"golang.org/x/sys/windows"
)

// EmojiUnsupported is true when the console was not already using UTF-8
// before this process started. It is used to default --no-emoji on Windows
// terminals that are likely to misrender multi-byte characters.
var EmojiUnsupported bool

func init() {
	// Force the Windows console to use UTF-8 so that emoji and other
	// multi-byte characters render correctly instead of being replaced
	// by "�?".
	const utf8CP = 65001
	if cp, err := windows.GetConsoleOutputCP(); err == nil {
		EmojiUnsupported = cp != utf8CP
	}
	_ = windows.SetConsoleOutputCP(utf8CP)
	_ = windows.SetConsoleCP(utf8CP)
}
