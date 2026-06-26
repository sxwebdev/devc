package cmd

import (
	"context"

	"github.com/sxwebdev/devc/internal/container"
	"github.com/urfave/cli/v3"
)

func newAttachCmd() *cli.Command {
	var shellFlag string

	return &cli.Command{
		Name:      "attach",
		Aliases:   []string{"shell"},
		Usage:     "Open a shell in the container (starts it if stopped)",
		Arguments: []cli.Argument{&cli.StringArg{Name: "path"}},
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "shell", Value: "/bin/bash", Usage: "shell to use", Destination: &shellFlag},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			mgr, err := container.NewManager()
			if err != nil {
				return err
			}
			defer mgr.Close()
			return mgr.Attach(workspaceFolder(cmd.StringArg("path")), shellFlag)
		},
	}
}
