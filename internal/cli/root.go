package cli

import (
	"fmt"
	"os"

	"github.com/sayandeep14/PromptLoom/internal/tui"
	"github.com/spf13/cobra"
)

// version is overridden at build time via -ldflags "-X <module>/internal/cli.version=vX.Y.Z".
var version = "dev"

var rootCmd = &cobra.Command{
	Use:     "loom",
	Short:   "PromptLoom — treat prompts like source code",
	Version: version,
	// No subcommand → launch the interactive REPL when connected to a terminal,
	// or print help when piped/non-interactive.
	RunE: func(cmd *cobra.Command, args []string) error {
		fi, err := os.Stdin.Stat()
		if err != nil || (fi.Mode()&os.ModeCharDevice) == 0 {
			return cmd.Help()
		}
		cwd, err := resolveProjectDir()
		if err != nil {
			return err
		}
		return tui.Run(version, cwd)
	},
}

func Execute() error {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, tui.ErrorStyle.Render("Error: "+err.Error()))
		return err
	}
	return nil
}

var themeFlag string

func init() {
	version = resolveVersion(version)
	rootCmd.Version = version
	rootCmd.PersistentFlags().StringVar(&themeFlag, "theme", "", "color theme: light or dark")
	rootCmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		if themeFlag != "" && !tui.SetTheme(themeFlag) {
			return fmt.Errorf("unknown theme %q — use 'light' or 'dark'", themeFlag)
		}
		return nil
	}

	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(weaveCmd)
	rootCmd.AddCommand(copyCmd)
	rootCmd.AddCommand(castCmd)
	rootCmd.AddCommand(threadCmd)
	rootCmd.AddCommand(inspectCmd)
	rootCmd.AddCommand(traceCmd)
	rootCmd.AddCommand(unravelCmd)
	rootCmd.AddCommand(listCmd)
	rootCmd.AddCommand(fmtCmd)
	rootCmd.AddCommand(fingerprintCmd)
	rootCmd.AddCommand(deployCmd)
	rootCmd.AddCommand(diffCmd)
	rootCmd.AddCommand(reviewCmd)
	rootCmd.AddCommand(doctorCmd)
	rootCmd.AddCommand(smellsCmd)
	rootCmd.AddCommand(contractCmd)
	rootCmd.AddCommand(checkOutputCmd)
	rootCmd.AddCommand(lockCmd)
	rootCmd.AddCommand(checkLockCmd)
	rootCmd.AddCommand(ciCmd)
	rootCmd.AddCommand(graphCmd)
	rootCmd.AddCommand(impactCmd)
	rootCmd.AddCommand(statsCmd)
	rootCmd.AddCommand(packCmd)
	rootCmd.AddCommand(testCmd)
	rootCmd.AddCommand(blameCmd)
	rootCmd.AddCommand(changelogCmd)
	rootCmd.AddCommand(auditCmd)
	rootCmd.AddCommand(mcpCmd)
	rootCmd.AddCommand(importCmd)
	rootCmd.AddCommand(lspCmd)
	rootCmd.AddCommand(recipeCmd)
	rootCmd.AddCommand(startCmd)
	rootCmd.AddCommand(summarizeCmd)
	rootCmd.AddCommand(installCmd)
	rootCmd.AddCommand(publishCmd)
	rootCmd.AddCommand(executeCmd)
}
