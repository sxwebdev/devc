package cmd

import (
	"context"

	"github.com/sxwebdev/devc/internal/container"
	"github.com/urfave/cli/v3"
)

func newExecCmd() *cli.Command {
	var workspaceFlag string

	return &cli.Command{
		Name:      "exec",
		Usage:     "Execute a command in a running container",
		UsageText: "devc exec [options] -- <command...>",
		Arguments: []cli.Argument{&cli.StringArgs{Name: "command", Min: 1, Max: -1}},
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "workspace-folder", Usage: "project root (default: cwd)", Destination: &workspaceFlag},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			mgr, err := container.NewManager()
			if err != nil {
				return err
			}
			defer mgr.Close()
			return mgr.Exec(workspaceFolder(workspaceFlag), cmd.StringArgs("command"))
		},
	}
}
