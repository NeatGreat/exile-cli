package cmd

import (
	"cmp"
	"fmt"
	"os"
	"os/exec"
	"runtime"

	"github.com/spf13/cobra"

	"exileagent/internal/config"
)

var configPathOnly bool

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Edit the agent configuration: credentials and environment",
	Long: `Open the agent configuration in your editor ($VISUAL or $EDITOR; TextEdit on
macOS otherwise). It holds environment variables for the agents, such as
CLAUDE_CODE_OAUTH_TOKEN or OPENAI_API_KEY, in a section per agent. At start,
exile copies the agent's variables into its container.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		path, _, err := config.InitAgents()
		if err != nil {
			return err
		}
		if configPathOnly {
			fmt.Println(path)
			return nil
		}

		var edit *exec.Cmd
		switch editor := cmp.Or(os.Getenv("VISUAL"), os.Getenv("EDITOR")); {
		case editor != "":
			// The editor may come with arguments, such as "code --wait".
			edit = exec.Command("sh", "-c", editor+` "$1"`, "sh", path)
		case runtime.GOOS == "darwin":
			edit = exec.Command("open", "-W", "-t", path)
		default:
			fmt.Println(path)
			return nil
		}
		edit.Stdin, edit.Stdout, edit.Stderr = os.Stdin, os.Stdout, os.Stderr
		if err := edit.Run(); err != nil {
			return err
		}
		_, err = config.LoadAgents()
		return err
	},
}

func init() {
	configCmd.Flags().BoolVar(&configPathOnly, "path", false, "print the file's path instead of opening it")
	rootCmd.AddCommand(configCmd)
}
