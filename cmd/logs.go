package cmd

import (
	"context"

	"github.com/sxwebdev/devc/internal/container"
	"github.com/urfave/cli/v3"
)

func newLogsCmd() *cli.Command {
	var followFlag bool

	return &cli.Command{
		Name:      "logs",
		Usage:     "Show container logs",
		Arguments: []cli.Argument{&cli.StringArg{Name: "path"}},
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "follow", Aliases: []string{"f"}, Usage: "stream new log output", Destination: &followFlag},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			mgr, err := container.NewManager()
			if err != nil {
				return err
			}
			defer mgr.Close()
			return mgr.Logs(workspaceFolder(cmd.StringArg("path")), followFlag)
		},
	}
}
