package cmd

import (
	"github.com/spf13/cobra"
	"github.com/sxwebdev/devc/internal/container"
)

func newStopCmd() *cobra.Command {
	var forceFlag bool

	cmd := &cobra.Command{
		Use:   "stop [path]",
		Short: "Stop a running container",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr, err := container.NewManager()
			if err != nil {
				return err
			}
			defer mgr.Close()
			return mgr.Stop(getWorkspaceFolder(args), forceFlag)
		},
	}

	cmd.Flags().BoolVar(&forceFlag, "force", false, "stop even with active sessions")

	return cmd
}
