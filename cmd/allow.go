package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"exileagent/internal/config"
	"exileagent/internal/container"
)

var allowGlobal bool

var allowCmd = &cobra.Command{
	Use:   "allow HOST...",
	Short: "Let the agent reach hosts over HTTPS",
	Long: `Let the agent reach hosts over HTTPS (port 443) through the exile proxy.
"*.example.com" allows all subdomains of example.com, but not example.com
itself. The agent's own API hosts are always allowed; "exile list" shows
them.`,
	Example: `  exile allow registry.npmjs.org
  exile allow proxy.golang.org sum.golang.org --global`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return updateHosts(args, allowGlobal, (*config.Scope).AllowHost, "Allowed", "already allowed")
	},
}

var disallowCmd = &cobra.Command{
	Use:   "disallow HOST...",
	Short: "Revoke hosts allowed with exile allow",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return updateHosts(args, allowGlobal, (*config.Scope).DisallowHost, "Disallowed", "not allowed")
	},
}

func init() {
	allowCmd.Flags().BoolVarP(&allowGlobal, "global", "g", false, "apply to all projects")
	disallowCmd.Flags().BoolVarP(&allowGlobal, "global", "g", false, "apply to all projects")
	rootCmd.AddCommand(allowCmd, disallowCmd)
}

// updateHosts applies change to each host in the project or global scope.
func updateHosts(args []string, global bool, change func(*config.Scope, string) bool, done, unchanged string) error {
	dir, err := projectDir()
	if err != nil {
		return err
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	scope := scopeFor(cfg, dir, global)
	var report []string
	for _, arg := range args {
		host, err := container.NormalizeHost(arg)
		if err != nil {
			return err
		}
		if change(scope, host) {
			report = append(report, fmt.Sprintf("%s %s (%s)", done, host, scopeName(global)))
		} else {
			report = append(report, fmt.Sprintf("%s is %s (%s)", host, unchanged, scopeName(global)))
		}
	}
	if err := cfg.Save(); err != nil {
		return err
	}
	for _, line := range report {
		fmt.Println(line)
	}
	return nil
}
