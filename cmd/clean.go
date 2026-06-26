package cmd

import (
	"context"
	"fmt"

	"github.com/sxwebdev/devc/internal/container"
	"github.com/urfave/cli/v3"
)

func newCleanCmd() *cli.Command {
	var dryRunFlag bool

	return &cli.Command{
		Name:  "clean",
		Usage: "Remove all stopped managed containers",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "dry-run", Usage: "show what would be removed", Destination: &dryRunFlag},
		},
		Action: func(_ context.Context, _ *cli.Command) error {
			mgr, err := container.NewManager()
			if err != nil {
				return err
			}
			defer mgr.Close()

			removed, err := mgr.Clean(dryRunFlag)
			if err != nil {
				return err
			}

			if len(removed) == 0 {
				fmt.Println("No stopped containers to clean")
				return nil
			}

			verb := "Removed"
			if dryRunFlag {
				verb = "Would remove"
			}
			for _, name := range removed {
				fmt.Printf("%s %s\n", verb, name)
			}

			return nil
		},
	}
}
