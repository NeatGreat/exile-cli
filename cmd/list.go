package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"exileagent/internal/config"
	"exileagent/internal/container"
	"exileagent/internal/project"
)

var listCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "Show what the agent can access",
	Long: `Show the files, directories and hosts the agent can access when started in
the project directory.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		dir, err := projectDir()
		if err != nil {
			return err
		}
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		// Show the configured mounts even if they cannot be resolved, so
		// they can be fixed with exile add/remove.
		entries := project.Mounts(cfg, dir)
		resolved, resolveErr := project.Resolve(entries)
		if resolveErr == nil {
			entries = resolved
		}
		rows := mountRows(entries)
		rows = append(rows, row{"rw", "volume " + container.HomeVolumeName(dir) + " -> " + container.Home, "home"})
		for _, global := range []bool{true, false} {
			for _, e := range scopeFor(cfg, dir, global).Mounts {
				if e.Disabled {
					rows = append(rows, row{"off", describe(e.Mount), scopeName(global)})
				}
			}
		}

		fmt.Printf("Project %s\n\n", dir)
		if err := printRows(os.Stdout, row{"MODE", "PATH", "SCOPE"}, rows); err != nil {
			return err
		}
		fmt.Println()
		var hosts []row
		for _, h := range project.Hosts(cfg, dir, container.AgentNames()...) {
			hosts = append(hosts, row{"https", h.Name, h.Scope})
		}
		if err := printRows(os.Stdout, row{"NET", "HOST", "SCOPE"}, hosts); err != nil {
			return err
		}

		agents, err := config.LoadAgents()
		if err != nil {
			return err
		}
		var vars []row
		for _, section := range append([]string{config.SectionAll}, container.AgentNames()...) {
			for _, v := range agents[section] {
				vars = append(vars, row{"env", v.Name, section})
			}
		}
		if len(vars) > 0 {
			fmt.Println()
			if err := printRows(os.Stdout, row{"CONFIG", "VARIABLE", "AGENT"}, vars); err != nil {
				return err
			}
		}
		return resolveErr
	},
}

func init() {
	rootCmd.AddCommand(listCmd)
}
