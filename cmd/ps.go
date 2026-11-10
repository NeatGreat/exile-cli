package cmd

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"exileagent/internal/container"
)

var psCmd = &cobra.Command{
	Use:   "ps",
	Short: "List running agent containers",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		running, err := container.Running()
		if err != nil {
			return err
		}
		if len(running) == 0 {
			fmt.Println("No exile containers running.")
			return nil
		}
		tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "NAME\tAGENT\tPROJECT\tSTATUS")
		for _, c := range running {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", c.Name, c.Agent, c.Workdir, c.Status)
		}
		return tw.Flush()
	},
}

func init() {
	rootCmd.AddCommand(psCmd)
}
