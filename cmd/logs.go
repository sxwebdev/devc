package cmd

import (
	"github.com/spf13/cobra"
	"github.com/sxwebdev/devc/internal/container"
)

func newLogsCmd() *cobra.Command {
	var followFlag bool

	cmd := &cobra.Command{
		Use:   "logs [path]",
		Short: "Show container logs",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr, err := container.NewManager()
			if err != nil {
				return err
			}
			defer mgr.Close()
			return mgr.Logs(getWorkspaceFolder(args), followFlag)
		},
	}

	cmd.Flags().BoolVarP(&followFlag, "follow", "f", false, "stream new log output")
	return cmd
}
