//go:build !windows

package main

// EmojiUnsupported is false on platforms where UTF-8 emoji typically work
// without extra console setup.
var EmojiUnsupported = false
