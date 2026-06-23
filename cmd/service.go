package cmd

import (
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/sxwebdev/devc/internal/config"
	"github.com/sxwebdev/devc/internal/services"
	"github.com/sxwebdev/devc/pkg/types"
)

func newServiceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "service",
		Short: "Manage sibling service containers (postgres, redis, ...)",
		Long: `Add, remove, and list sibling service containers in devcontainer.json.

Services run alongside the agent container on the same network. Use 'devc service
list' to see the catalog, then 'devc service add <name>' to inject one into the
current project's config. Run 'devc up' afterwards to start it.`,
	}

	cmd.AddCommand(
		newServiceAddCmd(),
		newServiceRemoveCmd(),
		newServiceListCmd(),
	)

	return cmd
}

func newServiceAddCmd() *cobra.Command {
	var (
		pathFlag  string
		forceFlag bool
	)

	cmd := &cobra.Command{
		Use:   "add <name>...",
		Short: "Add one or more services to devcontainer.json",
		Long: `Add one or more catalog services to the project's devcontainer.json.

Existing services are left untouched unless --force is given. Run
'devc service list' to see available service names.

Examples:
  devc service add postgres
  devc service add postgres redis
  devc service add postgres --force`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ws := getWorkspaceFolder(nil)
			if pathFlag != "" {
				ws = pathFlag
			}
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
			for _, name := range args {
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

	cmd.Flags().StringVar(&pathFlag, "path", "", "workspace folder (default: current directory)")
	cmd.Flags().BoolVar(&forceFlag, "force", false, "overwrite a service that is already configured")

	return cmd
}

func newServiceRemoveCmd() *cobra.Command {
	var pathFlag string

	cmd := &cobra.Command{
		Use:   "remove <name>...",
		Short: "Remove one or more services from devcontainer.json",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ws := getWorkspaceFolder(nil)
			if pathFlag != "" {
				ws = pathFlag
			}
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
			for _, name := range args {
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

	cmd.Flags().StringVar(&pathFlag, "path", "", "workspace folder (default: current directory)")

	return cmd
}

func newServiceListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List available catalog services",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "SERVICE\tIMAGE\tPORT")
			for _, name := range services.Names() {
				tmpl, _ := services.Template(name)
				fmt.Fprintf(w, "%s\t%s\t%d\n", name, tmpl.Image, tmpl.ContainerPort)
			}
			return w.Flush()
		},
	}
}
