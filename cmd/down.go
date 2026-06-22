package cmd

import (
	"github.com/spf13/cobra"
	"github.com/sxwebdev/devc/internal/container"
)

func newDownCmd() *cobra.Command {
	var forceFlag bool

	cmd := &cobra.Command{
		Use:   "down [path]",
		Short: "Stop and remove a container",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr, err := container.NewManager()
			if err != nil {
				return err
			}
			defer mgr.Close()
			return mgr.Down(getWorkspaceFolder(args), forceFlag)
		},
	}

	cmd.Flags().BoolVar(&forceFlag, "force", false, "remove even with active sessions")

	return cmd
}
