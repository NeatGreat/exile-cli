package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"exileagent/internal/config"
	"exileagent/internal/container"
	"exileagent/internal/project"
)

var removeGlobal bool

var removeCmd = &cobra.Command{
	Use:     "remove PATH...",
	Aliases: []string{"rm"},
	Short:   "Revoke access granted with exile add",
	Long: `Remove paths added with "exile add". Removing a hidden path makes it
visible again; removing the project directory's entry restores the default
read-write mount.`,
	Example: `  exile remove ../shared-lib
  exile remove --global ~/.gitconfig`,
	Args: cobra.MinimumNArgs(1),
	RunE: runRemove,
}

func init() {
	removeCmd.Flags().BoolVarP(&removeGlobal, "global", "g", false, "remove from the mounts of all projects")
	rootCmd.AddCommand(removeCmd)
}

func runRemove(cmd *cobra.Command, args []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	dir, err := projectDir()
	if err != nil {
		return err
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	scope := scopeFor(cfg, dir, removeGlobal)

	var removed []string
	for _, arg := range args {
		src, err := container.HostPath(arg, cwd)
		if err != nil {
			return err
		}
		if !scope.Remove(src) {
			return fmt.Errorf("%s is not in the %s mounts (see exile list)", src, scopeName(removeGlobal))
		}
		removed = append(removed, src)
	}

	if err := project.Validate(cfg, dir); err != nil {
		return fmt.Errorf("%w; remove that as well", err)
	}
	if err := cfg.Save(); err != nil {
		return err
	}
	for _, src := range removed {
		fmt.Println("Removed", src)
	}
	return nil
}
