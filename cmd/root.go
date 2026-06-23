package cmd

import (
	"os"

	"github.com/spf13/cobra"
)

// flagOutputFormat is bound by addOutputFormatFlag on the commands that produce
// machine-readable output (list, status, and config/config show).
var flagOutputFormat string

// addOutputFormatFlag registers the shared --output-format flag on a command.
func addOutputFormatFlag(cmd *cobra.Command) {
	cmd.Flags().StringVar(&flagOutputFormat, "output-format", "text", "output format (text, json)")
}

func NewRootCmd(version string) *cobra.Command {
	root := &cobra.Command{
		Use:          "devc",
		Short:        "AI-safe development containers",
		Long:         "Create and manage AI-safe development containers with devcontainer.json support.",
		Version:      version,
		SilenceUsage: true,
	}

	root.AddCommand(
		newUpCmd(),
		newExecCmd(),
		newAttachCmd(),
		newStopCmd(),
		newDownCmd(),
		newListCmd(),
		newStatusCmd(),
		newLogsCmd(),
		newCleanCmd(),
		newConfigCmd(),
		newInitCmd(),
	)

	return root
}

func Execute(version string) {
	if err := NewRootCmd(version).Execute(); err != nil {
		os.Exit(1)
	}
}

func getWorkspaceFolder(args []string) string {
	if len(args) > 0 {
		return args[0]
	}
	dir, err := os.Getwd()
	if err != nil {
		return "."
	}
	return dir
}
