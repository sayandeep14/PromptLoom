package cli

import (
	"fmt"

	"github.com/sayandeepgiri/promptloom/internal/tui"
	"github.com/spf13/cobra"
)

var packCmd = &cobra.Command{
	Use:   "pack <subcommand>",
	Short: "Manage prompt packs",
	Long: `Build, install, list, and remove versioned bundles of prompts and blocks.

Subcommands:
  init                create a pack.toml scaffold in the current project
  build               bundle prompts/ and blocks/ into a .lpack archive
  install <path>      unpack a .lpack into prompts/<name>/ and blocks/<name>/
  list                list installed packs
  remove <name>       remove an installed pack`,
}

var packInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Create a pack.toml scaffold",
	RunE:  runPackInit,
}

var packBuildCmd = &cobra.Command{
	Use:   "build",
	Short: "Bundle the project into a .lpack archive",
	RunE:  runPackBuild,
}

var packInstallCmd = &cobra.Command{
	Use:   "install <path>",
	Short: "Install a .lpack archive into the project",
	Args:  cobra.ExactArgs(1),
	RunE:  runPackInstall,
}

var packListCmd = &cobra.Command{
	Use:   "list",
	Short: "List installed packs",
	RunE:  runPackList,
}

var packRemoveCmd = &cobra.Command{
	Use:   "remove <name>",
	Short: "Remove an installed pack",
	Args:  cobra.ExactArgs(1),
	RunE:  runPackRemove,
}

func init() {
	packCmd.AddCommand(packInitCmd, packBuildCmd, packInstallCmd, packListCmd, packRemoveCmd)
}

func runPackInit(cmd *cobra.Command, args []string) error {
	cwd, err := resolveProjectDir()
	if err != nil {
		return err
	}
	out, hasErr := tui.RunPackInit(cwd)
	fmt.Print(out)
	if hasErr {
		return fmt.Errorf("pack init failed")
	}
	return nil
}

func runPackBuild(cmd *cobra.Command, args []string) error {
	cwd, err := resolveProjectDir()
	if err != nil {
		return err
	}
	out, hasErr := tui.RunPackBuild(cwd)
	fmt.Print(out)
	if hasErr {
		return fmt.Errorf("pack build failed")
	}
	return nil
}

func runPackInstall(cmd *cobra.Command, args []string) error {
	cwd, err := resolveProjectDir()
	if err != nil {
		return err
	}
	out, hasErr := tui.RunPackInstall(args[0], cwd)
	fmt.Print(out)
	if hasErr {
		return fmt.Errorf("pack install failed")
	}
	return nil
}

func runPackList(cmd *cobra.Command, args []string) error {
	cwd, err := resolveProjectDir()
	if err != nil {
		return err
	}
	fmt.Print(tui.RunPackList(cwd))
	return nil
}

func runPackRemove(cmd *cobra.Command, args []string) error {
	cwd, err := resolveProjectDir()
	if err != nil {
		return err
	}
	out, hasErr := tui.RunPackRemove(args[0], cwd)
	fmt.Print(out)
	if hasErr {
		return fmt.Errorf("pack remove failed")
	}
	return nil
}
