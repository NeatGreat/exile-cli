package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"exileagent/internal/container"
)

var buildOpts container.BuildOptions

var buildCmd = &cobra.Command{
	Use:   "build",
	Short: "Build the agent image",
	Long: `Build the container image with Claude Code, Codex and Cline. "exile start"
builds it automatically when it is missing or outdated; use --no-cache to
update the agents to their latest versions.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return container.BuildImage(container.DefaultImage, buildOpts)
	},
}

func init() {
	buildCmd.Flags().BoolVar(&buildOpts.NoCache, "no-cache", false, "rebuild all layers")
	buildCmd.Flags().BoolVar(&buildOpts.Pull, "pull", false, "pull a newer base image")
	rootCmd.AddCommand(buildCmd)
}

// ensureImage builds image if it is missing or was built from an older
// Dockerfile.
func ensureImage(image string) error {
	ok, err := container.ImageCurrent(image)
	if err != nil || ok {
		return err
	}
	fmt.Fprintf(os.Stderr, "exile: building image %s\n", image)
	return container.BuildImage(image, container.BuildOptions{})
}
