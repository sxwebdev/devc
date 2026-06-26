package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/sxwebdev/devc/internal/container"
	"github.com/urfave/cli/v3"
)

func newListCmd() *cli.Command {
	return &cli.Command{
		Name:    "list",
		Aliases: []string{"ls"},
		Usage:   "List managed containers",
		Flags:   []cli.Flag{outputFormatFlag()},
		Action: func(_ context.Context, cmd *cli.Command) error {
			mgr, err := container.NewManager()
			if err != nil {
				return err
			}
			defer mgr.Close()

			containers, err := mgr.List()
			if err != nil {
				return err
			}

			if cmd.String("output-format") == "json" {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(containers)
			}

			if len(containers) == 0 {
				fmt.Println("No managed containers found")
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			_, _ = fmt.Fprintln(w, "NAME\tSTATE\tIMAGE\tAGENT\tSESSIONS\tWORKSPACE")
			for _, c := range containers {
				agent := c.Agent
				if agent == "" {
					agent = "-"
				}
				_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%d\t%s\n",
					c.Name, c.State, c.Image, agent, c.Sessions, c.WorkspaceFolder)
			}
			return w.Flush()
		},
	}
}
