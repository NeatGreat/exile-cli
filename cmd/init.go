package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"

	"exileagent/internal/config"
	"exileagent/internal/container"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Set up exile: check Docker, build the agent image, create the agent config",
	Long: `Set up everything exile needs in one go: check that Docker is running, build
the agent image and create the agent configuration for your credentials.
Running it again is safe; it only does what is missing or outdated.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		p := painter(useColor(os.Stdout))
		done := func(format string, a ...any) {
			fmt.Printf("%s %s\n", p.paint("1;32", "✓"), fmt.Sprintf(format, a...))
		}
		todo := func(format string, a ...any) {
			fmt.Printf("%s %s\n", p.paint("1;33", "→"), fmt.Sprintf(format, a...))
		}

		cpus, memory, err := container.Resources()
		if err != nil {
			return fmt.Errorf("Docker is not reachable: %w\nStart Docker Desktop or OrbStack, or run \"colima start\", and try again", err)
		}
		done("Docker is running (%d CPUs, %s memory)", cpus, memoryText(strconvMB(memory)))

		if err := ensureImage(container.DefaultImage); err != nil {
			return err
		}
		done("Agent image %s is ready", container.DefaultImage)

		path, created, err := config.InitAgents()
		if err != nil {
			return err
		}
		if created {
			done("Created the agent configuration %s", path)
		} else {
			done("Agent configuration %s", path)
		}
		agents, err := config.LoadAgents()
		if err != nil {
			return err
		}
		for _, agent := range []string{"claude", "codex", "cline"} {
			if len(agents[agent]) == 0 {
				todo("No credentials for %s yet: add them with \"exile config\", or log in inside the container", agent)
			}
		}

		if _, err := exec.LookPath("exile"); err != nil {
			if exe, err := os.Executable(); err == nil {
				rc := "~/." + filepath.Base(os.Getenv("SHELL")) + "rc"
				todo("exile is not on your PATH. Add it with:\n    echo 'export PATH=\"%s:$PATH\"' >> %s", filepath.Dir(exe), rc)
			}
		}

		fmt.Printf("\nAll set. Run %s in a project folder to start Claude Code in exile.\n", p.paint("1", "exile start"))
		return nil
	},
}

func init() {
	rootCmd.AddCommand(initCmd)
}

// strconvMB formats a byte count in docker's "1536m" notation.
func strconvMB(bytes int64) string {
	return fmt.Sprintf("%dm", bytes>>20)
}
