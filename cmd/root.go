package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var workdir string

var rootCmd = &cobra.Command{
	Use:   "exile",
	Short: "Run coding agents isolated in Docker containers",
	Long: `Exile runs coding agents such as Claude Code or Codex inside a Docker
container. The agent only sees the project directory and the files and
directories you explicitly grant with "exile add".`,
	SilenceUsage:  true,
	SilenceErrors: true,
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Help()
	},
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&workdir, "workdir", "w", "", "project directory (default: current directory)")
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "exile:", err)
		os.Exit(1)
	}
}
