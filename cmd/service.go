package cmd

import (
	"context"
	"fmt"
	"text/tabwriter"

	"github.com/sxwebdev/devc/internal/config"
	"github.com/sxwebdev/devc/internal/services"
	"github.com/sxwebdev/devc/pkg/types"
	"github.com/urfave/cli/v3"
)

func newServiceCmd() *cli.Command {
	return &cli.Command{
		Name:  "service",
		Usage: "Manage sibling service containers (postgres, redis, ...)",
		Description: `Add, remove, and list sibling service containers in devcontainer.json.

Services run alongside the agent container on the same network. Use 'devc service
list' to see the catalog, then 'devc service add <name>' to inject one into the
current project's config. Run 'devc up' afterwards to start it.`,
		// An unrecognized subcommand falls through here; report it (exit 1) rather
		// than urfave's default "No help topic for 'X'" + exit 3. Bare `devc
		// service` shows help.
		Action: func(_ context.Context, cmd *cli.Command) error {
			if cmd.Args().Present() {
				return fmt.Errorf("unknown command %q for %q", cmd.Args().First(), cmd.Name)
			}
			return cli.ShowSubcommandHelp(cmd)
		},
		Commands: []*cli.Command{
			newServiceAddCmd(),
			newServiceRemoveCmd(),
			newServiceListCmd(),
		},
	}
}

func newServiceAddCmd() *cli.Command {
	var (
		pathFlag  string
		forceFlag bool
	)

	return &cli.Command{
		Name:  "add",
		Usage: "Add one or more services to devcontainer.json",
		Description: `Add one or more catalog services to the project's devcontainer.json.

Existing services are left untouched unless --force is given. Run
'devc service list' to see available service names.

Examples:
  devc service add postgres
  devc service add postgres redis
  devc service add postgres --force`,
		Arguments: []cli.Argument{&cli.StringArgs{Name: "name", Min: 0, Max: -1}},
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "path", Usage: "workspace folder (default: current directory)", Destination: &pathFlag},
			&cli.BoolFlag{Name: "force", Usage: "overwrite a service that is already configured", Destination: &forceFlag},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			names := cmd.StringArgs("name")
			if len(names) == 0 {
				return fmt.Errorf("requires at least one service name; run 'devc service list' to see options")
			}

			ws := workspaceFolder(pathFlag)
			cfgPath := config.FindDevcontainerPath(ws)

			devCfg, err := config.LoadDevcontainerConfig(ws)
			if err != nil {
				return fmt.Errorf("no devcontainer.json found; run 'devc init' first")
			}

			custom, err := config.ExtractDevcCustomization(devCfg)
			if err != nil {
				return err
			}
			if custom.Services == nil {
				custom.Services = make(map[string]*types.ServiceConfig)
			}

			added := 0
			for _, name := range names {
				tmpl, ok := services.Template(name)
				if !ok {
					return fmt.Errorf("unknown service %q; run 'devc service list' to see options", name)
				}
				if custom.Services[name] != nil && !forceFlag {
					fmt.Printf("Service %q already present; leaving it unchanged (use --force to overwrite)\n", name)
					continue
				}
				custom.Services[name] = tmpl
				fmt.Printf("Added service %s (%s)\n", name, tmpl.Image)
				added++
			}

			if added == 0 {
				return nil
			}

			if err := config.ApplyDevcCustomization(cfgPath, devCfg, custom); err != nil {
				return fmt.Errorf("saving config: %w", err)
			}
			fmt.Printf("Updated %s\n", cfgPath)
			return nil
		},
	}
}

func newServiceRemoveCmd() *cli.Command {
	var pathFlag string

	return &cli.Command{
		Name:      "remove",
		Usage:     "Remove one or more services from devcontainer.json",
		Arguments: []cli.Argument{&cli.StringArgs{Name: "name", Min: 0, Max: -1}},
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "path", Usage: "workspace folder (default: current directory)", Destination: &pathFlag},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			names := cmd.StringArgs("name")
			if len(names) == 0 {
				return fmt.Errorf("requires at least one service name to remove")
			}

			ws := workspaceFolder(pathFlag)
			cfgPath := config.FindDevcontainerPath(ws)

			devCfg, err := config.LoadDevcontainerConfig(ws)
			if err != nil {
				return fmt.Errorf("no devcontainer.json found; run 'devc init' first")
			}

			custom, err := config.ExtractDevcCustomization(devCfg)
			if err != nil {
				return err
			}
			if len(custom.Services) == 0 {
				return fmt.Errorf("no services configured")
			}

			removed := 0
			for _, name := range names {
				if _, ok := custom.Services[name]; !ok {
					fmt.Printf("Service %q not found; skipping\n", name)
					continue
				}
				delete(custom.Services, name)
				fmt.Printf("Removed service %s\n", name)
				removed++
			}

			if removed == 0 {
				return fmt.Errorf("no matching services to remove")
			}

			// Prune the empty map so the services key drops out of the JSON cleanly.
			if len(custom.Services) == 0 {
				custom.Services = nil
			}

			if err := config.ApplyDevcCustomization(cfgPath, devCfg, custom); err != nil {
				return fmt.Errorf("saving config: %w", err)
			}
			fmt.Printf("Updated %s\n", cfgPath)
			return nil
		},
	}
}

func newServiceListCmd() *cli.Command {
	return &cli.Command{
		Name:  "list",
		Usage: "List available catalog services",
		Action: func(_ context.Context, cmd *cli.Command) error {
			w := tabwriter.NewWriter(cmd.Writer, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "SERVICE\tIMAGE\tPORT")
			for _, name := range services.Names() {
				tmpl, _ := services.Template(name)
				fmt.Fprintf(w, "%s\t%s\t%d\n", name, tmpl.Image, tmpl.ContainerPort)
			}
			return w.Flush()
		},
	}
}
