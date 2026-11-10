package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"exileagent/internal/container"
)

var stopAll bool

var stopCmd = &cobra.Command{
	Use:   "stop [name...]",
	Short: "Stop agent containers",
	Long: `Stop agent containers by name. Without names, stops the containers started
from the project directory.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		running, err := container.Running()
		if err != nil {
			return err
		}
		names, err := selectContainers(running, args)
		if err != nil {
			return err
		}
		if len(names) == 0 {
			fmt.Println("No matching exile containers running.")
		} else if err := container.Stop(names...); err != nil {
			return err
		} else {
			fmt.Println("Stopped", strings.Join(names, ", "))
		}
		return container.Prune()
	},
}

func init() {
	stopCmd.Flags().BoolVarP(&stopAll, "all", "a", false, "stop all exile containers")
	rootCmd.AddCommand(stopCmd)
}

func selectContainers(running []container.Container, args []string) ([]string, error) {
	var names []string
	switch {
	case stopAll:
		if len(args) > 0 {
			return nil, fmt.Errorf("--all takes no container names")
		}
		for _, c := range running {
			names = append(names, c.Name)
		}
	case len(args) > 0:
		known := map[string]bool{}
		for _, c := range running {
			known[c.Name] = true
		}
		for _, name := range args {
			if !known[name] {
				return nil, fmt.Errorf("no running exile container named %q", name)
			}
			names = append(names, name)
		}
	default:
		dir, err := projectDir()
		if err != nil {
			return nil, err
		}
		for _, c := range running {
			if c.Workdir == dir {
				names = append(names, c.Name)
			}
		}
	}
	return names, nil
}
