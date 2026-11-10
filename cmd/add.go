package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"exileagent/internal/config"
	"exileagent/internal/container"
	"exileagent/internal/project"
)

var addOpts struct {
	readOnly bool
	hide     bool
	to       string
	global   bool
}

var addCmd = &cobra.Command{
	Use:   "add PATH...",
	Short: "Give the agent access to files or directories",
	Long: `Give the agent access to files or directories, or hide them from it.

Paths are mounted read-write at the same location as on the host, unless
--ro or --to say otherwise. --hide masks a path inside a mounted directory,
such as a .env file in the project. Adding a path again replaces its previous
setting, so "exile add . --ro" makes the project directory read-only and
"exile add .git" lets the agent commit.

Mounts apply to the project directory only, unless --global is given.`,
	Example: `  exile add ../shared-lib --ro
  exile add .env secrets --hide
  exile add . --ro
  exile add ~/.gitconfig --to '~/.gitconfig' --ro --global`,
	Args: cobra.MinimumNArgs(1),
	RunE: runAdd,
}

func init() {
	f := addCmd.Flags()
	f.BoolVar(&addOpts.readOnly, "ro", false, "mount read-only")
	f.BoolVar(&addOpts.hide, "hide", false, "hide the paths from the agent")
	f.StringVar(&addOpts.to, "to", "", "container `path` to mount at; ~ is the agent's home")
	f.BoolVarP(&addOpts.global, "global", "g", false, "apply to all projects")
	addCmd.MarkFlagsMutuallyExclusive("ro", "hide")
	addCmd.MarkFlagsMutuallyExclusive("to", "hide")
	addCmd.MarkFlagsMutuallyExclusive("global", "hide")
	rootCmd.AddCommand(addCmd)
}

func runAdd(cmd *cobra.Command, args []string) error {
	if addOpts.to != "" && len(args) > 1 {
		return errors.New("--to takes exactly one path")
	}
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
	scope := scopeFor(cfg, dir, addOpts.global)

	mode := container.ReadWrite
	switch {
	case addOpts.readOnly:
		mode = container.ReadOnly
	case addOpts.hide:
		mode = container.Hidden
	}

	var report []string
	for _, arg := range args {
		src, err := container.HostPath(arg, cwd)
		if err != nil {
			return err
		}
		if _, err := os.Stat(src); err != nil {
			return err
		}
		m := container.Mount{Source: src, Mode: mode}
		if addOpts.to != "" {
			if m.Target, err = container.ContainerPath(addOpts.to); err != nil {
				return err
			}
			if m.Target == filepath.ToSlash(src) {
				m.Target = ""
			}
		}
		verb := "Added"
		if scope.Add(config.Entry{Mount: m}) {
			verb = "Updated"
		}
		report = append(report, fmt.Sprintf("%s %s %s", verb, m.Mode, describe(m)))
	}

	// Refuse to save mounts the project could not start with.
	if err := project.Validate(cfg, dir); err != nil {
		return err
	}
	if err := cfg.Save(); err != nil {
		return err
	}
	for _, line := range report {
		fmt.Println(line)
	}
	return nil
}

func scopeFor(cfg *config.Config, dir string, global bool) *config.Scope {
	if global {
		return &cfg.Global
	}
	return cfg.Project(dir)
}

func scopeName(global bool) string {
	if global {
		return project.ScopeGlobal
	}
	return project.ScopeProject
}
