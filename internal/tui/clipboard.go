package tui

import "github.com/atotto/clipboard"

// CopyToClipboard writes text to the system clipboard.
func CopyToClipboard(text string) error {
	return clipboard.WriteAll(text)
}
