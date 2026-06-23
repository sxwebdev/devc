package cmd

import (
	"github.com/spf13/cobra"
	"github.com/sxwebdev/devc/internal/container"
)

func newAttachCmd() *cobra.Command {
	var shellFlag string

	cmd := &cobra.Command{
		Use:     "attach [path]",
		Aliases: []string{"shell"},
		Short:   "Open a shell in the container (starts it if stopped)",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr, err := container.NewManager()
			if err != nil {
				return err
			}
			defer mgr.Close()
			return mgr.Attach(getWorkspaceFolder(args), shellFlag)
		},
	}

	cmd.Flags().StringVar(&shellFlag, "shell", "/bin/bash", "shell to use")

	return cmd
}
