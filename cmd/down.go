package cmd

import (
	"context"

	"github.com/sxwebdev/devc/internal/container"
	"github.com/urfave/cli/v3"
)

func newDownCmd() *cli.Command {
	var forceFlag bool

	return &cli.Command{
		Name:      "down",
		Usage:     "Stop and remove a container",
		Arguments: []cli.Argument{&cli.StringArg{Name: "path"}},
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "force", Usage: "remove even with active sessions", Destination: &forceFlag},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			mgr, err := container.NewManager()
			if err != nil {
				return err
			}
			defer mgr.Close()
			return mgr.Down(workspaceFolder(cmd.StringArg("path")), forceFlag)
		},
	}
}
