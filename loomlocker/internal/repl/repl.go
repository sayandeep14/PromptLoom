package repl

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/sayandeep14/PromptLoom/loomlocker/internal/server"
)

const prompt = "loomlocker> "

const helpText = `
  lock     lock all secrets (replace real values with random tokens)
  unlock   restore original values (requires password)
  status   show current lock state
  stop     safely unlock (if locked) and shut down loomlocker
  help     show this help
`

// Run starts the interactive REPL. It blocks until "stop" is typed or stdin closes.
// srv must already be running.
func Run(srv *server.Server, readPassword func(string) (string, error)) {
	scanner := bufio.NewScanner(os.Stdin)
	fmt.Print(prompt)
	for scanner.Scan() {
		cmd := strings.TrimSpace(scanner.Text())
		quit := handleCommand(cmd, srv, readPassword)
		if quit {
			break
		}
		fmt.Print(prompt)
	}
}

func handleCommand(cmd string, srv *server.Server, readPassword func(string) (string, error)) (quit bool) {
	switch cmd {
	case "lock":
		if err := srv.Lock(); err != nil {
			fmt.Fprintf(os.Stderr, "  ✗  lock failed: %v\n", err)
		} else {
			st := srv.Status()
			fmt.Printf("  ✓  locked — %d secret(s) in %d file(s)\n",
				st.SecretCount, len(st.Files))
		}

	case "unlock":
		password, err := readPassword("  Password")
		if err != nil {
			fmt.Fprintf(os.Stderr, "  ✗  %v\n", err)
			break
		}
		if err := srv.Unlock(password); err != nil {
			fmt.Fprintf(os.Stderr, "  ✗  unlock failed: %v\n", err)
		} else {
			fmt.Printf("  ✓  unlocked — auto-relock in %ds\n",
				srv.UnlockDurationSeconds())
		}

	case "status":
		st := srv.Status()
		if st.Locked {
			fmt.Printf("  state    LOCKED (since %s)\n",
				st.LockedAt.Format("15:04:05"))
		} else {
			fmt.Println("  state    UNLOCKED")
		}
		fmt.Printf("  secrets  %d key(s)\n", st.SecretCount)
		for _, f := range st.Files {
			fmt.Printf("           • %s\n", f)
		}
		fmt.Printf("  port     %s\n", st.Port)

	case "stop":
		locked := srv.IsLocked()
		if locked {
			password, err := readPassword("  Password to unlock before stopping")
			if err != nil {
				fmt.Fprintf(os.Stderr, "  ✗  %v\n", err)
				break
			}
			if err := srv.Unlock(password); err != nil {
				fmt.Fprintf(os.Stderr, "  ✗  could not unlock: %v\n", err)
				break
			}
			fmt.Println("  ✓  secrets unlocked")
		}
		fmt.Println("  ✓  loomlocker stopped")
		srv.Shutdown()
		return true

	case "help", "":
		fmt.Print(helpText)

	default:
		fmt.Printf("  unknown command %q — type 'help'\n", cmd)
	}
	return false
}
