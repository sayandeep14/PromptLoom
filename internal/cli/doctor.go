package cli

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/sayandeep14/PromptLoom/internal/doctor"
	"github.com/sayandeep14/PromptLoom/internal/tui"
	"github.com/spf13/cobra"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor [PromptName]",
	Short: "Check prompt health and detect smells",
	Long: `Run structural checks and smell detection on one prompt or the entire library.

Examples:
  loom doctor                   # check all prompts
  loom doctor SecurityReviewer  # check one prompt
  loom doctor --system          # check the installation: project, git, API key, registry`,
	Args: cobra.MaximumNArgs(1),
	RunE: runDoctor,
}

var doctorSystem bool

func init() {
	doctorCmd.Flags().BoolVar(&doctorSystem, "system", false, "check the installation (project, git, model API key, registry) instead of prompts")
}

func runDoctorSystem() error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	checks := doctor.System(doctor.Env{
		Version: version, Dir: cwd, Getenv: os.Getenv, LookPath: exec.LookPath,
		RegistryURL: func(dir string) (string, string) { return resolveRegistryURL(dir) },
	})
	fmt.Println()
	fmt.Println("  " + tui.HeaderStyle.Render("loom doctor --system"))
	fmt.Println()
	for _, c := range checks {
		mark := tui.SuccessStyle.Render("✓")
		switch c.Level {
		case doctor.LevelWarn:
			mark = tui.WarningStyle.Render("⚠")
		case doctor.LevelFail:
			mark = tui.ErrorStyle.Render("✗")
		}
		fmt.Printf("  %s  %-14s %s\n", mark, c.Name, c.Detail)
		if c.Hint != "" {
			fmt.Printf("     %-14s %s\n", "", tui.MutedStyle.Render(c.Hint))
		}
	}
	fmt.Println()
	if doctor.Failed(checks) {
		return fmt.Errorf("the installation has problems (see ✗ above)")
	}
	return nil
}

func runDoctor(cmd *cobra.Command, args []string) error {
	if doctorSystem {
		return runDoctorSystem()
	}
	cwd, err := resolveProjectDir()
	if err != nil {
		return err
	}

	all := len(args) == 0
	name := ""
	if !all {
		name = args[0]
	}

	out, err := tui.RunWithSpinner("checking prompt health…", func() (string, error) {
		return tui.RunDoctor(name, all, cwd)
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, tui.ErrorStyle.Render("Error: "+err.Error()))
		os.Exit(1)
	}
	fmt.Print(out)
	return nil
}
