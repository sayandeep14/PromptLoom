package tui

import (
	"fmt"
	"os"
	"time"
)

var spinFrames = []string{"⣾", "⣽", "⣻", "⢿", "⡿", "⣟", "⣯", "⣷"}

type spinnerResult struct {
	output string
	err    error
}

// RunWithSpinner displays an animated spinner while fn executes.
// Falls back to direct execution when stdout is not a terminal.
func RunWithSpinner(label string, fn func() (string, error)) (string, error) {
	if !isTerminal(os.Stdout) {
		return fn()
	}

	resultCh := make(chan spinnerResult, 1)
	go func() {
		out, err := fn()
		resultCh <- spinnerResult{out, err}
	}()

	ticker := time.NewTicker(80 * time.Millisecond)
	defer ticker.Stop()
	frame := 0
	for {
		select {
		case r := <-resultCh:
			fmt.Print("\r\033[K") // clear spinner line
			return r.output, r.err
		case <-ticker.C:
			spin := BrightStyle.Render(spinFrames[frame%len(spinFrames)])
			fmt.Printf("\r  %s  %s", spin, MutedStyle.Render(label))
			frame++
		}
	}
}

func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && (fi.Mode()&os.ModeCharDevice) != 0
}
