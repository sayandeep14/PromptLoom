package tui

import "strings"

var topLevelCommands = []string{
	"weave", "deploy", "inspect", "list", "trace", "unravel", "thread", "fmt", "fingerprint", "help", "exit",
}

var threadSubcommands = []string{"prompt", "block"}

// Completions returns the set of completions for the current partial input.
func Completions(input string, promptNames []string) []string {
	parts := strings.Fields(input)
	trailing := len(input) > 0 && input[len(input)-1] == ' '

	switch {
	// No input yet, or typing first word.
	case len(parts) == 0 || (len(parts) == 1 && !trailing):
		prefix := ""
		if len(parts) == 1 {
			prefix = parts[0]
		}
		return filter(topLevelCommands, prefix)

	// "weave " or "trace " or "unravel " + optional partial name.
	case len(parts) >= 1 && (parts[0] == "weave" || parts[0] == "trace" || parts[0] == "unravel" || parts[0] == "fingerprint"):
		if len(parts) == 1 && trailing {
			return promptNames
		}
		if len(parts) == 2 && !trailing {
			return filter(promptNames, parts[1])
		}
		// weave --all has no name arg
		if parts[0] == "weave" && len(parts) == 2 && trailing {
			return nil
		}

	// "thread " → complete subcommand.
	case parts[0] == "thread":
		if len(parts) == 1 && trailing {
			return threadSubcommands
		}
		if len(parts) == 2 && !trailing {
			return filter(threadSubcommands, parts[1])
		}
	}
	return nil
}

func filter(candidates []string, prefix string) []string {
	if prefix == "" {
		return candidates
	}
	var out []string
	for _, c := range candidates {
		if strings.HasPrefix(c, prefix) {
			out = append(out, c)
		}
	}
	return out
}
