package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/sxwebdev/devc/internal/container"
	"github.com/urfave/cli/v3"
)

func newStatusCmd() *cli.Command {
	return &cli.Command{
		Name:      "status",
		Usage:     "Show the container state and effective config for a workspace",
		Arguments: []cli.Argument{&cli.StringArg{Name: "path"}},
		Flags:     []cli.Flag{outputFormatFlag()},
		Action: func(_ context.Context, cmd *cli.Command) error {
			mgr, err := container.NewManager()
			if err != nil {
				return err
			}
			defer mgr.Close()

			info, err := mgr.Status(workspaceFolder(cmd.StringArg("path")))
			if err != nil {
				return err
			}

			if cmd.String("output-format") == "json" {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(info)
			}

			printStatus(info)
			return nil
		},
	}
}

func printStatus(info *container.StatusInfo) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	row := func(k, v string) { _, _ = fmt.Fprintf(w, "%s\t%s\n", k, v) }

	row("Container:", info.Name)
	row("Workspace:", info.Workspace)
	row("State:", info.State)
	row("Image:", dashIfEmpty(info.Image))
	row("Agents:", dashIfEmpty(strings.Join(info.Agents, ", ")))
	row("Security:", dashIfEmpty(info.SecurityProfile))

	row("Network:", dashIfEmpty(formatNetwork(info.NetworkMode, info.AllowlistSize)))

	if info.CPUs != "" || info.Memory != "" || info.PidsLimit > 0 {
		row("Resources:", formatResources(info.CPUs, info.Memory, info.PidsLimit))
	}
	row("Sessions:", fmt.Sprintf("%d", info.Sessions))
	if len(info.Services) > 0 {
		row("Services:", strings.Join(info.Services, ", "))
	}
	// "not_found" mirrors docker.StateNotFound; drift only applies to a real container.
	if info.State != "not_found" {
		drift := "no"
		if info.ConfigDrift {
			drift = "yes — run 'devc up' to rebuild"
		}
		row("Config drift:", drift)
	}

	_ = w.Flush()
}

func dashIfEmpty(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// formatNetwork renders a network mode with its allowlist size, shared by
// `status` and `config show` so the two stay consistent.
func formatNetwork(mode string, allowlistSize int) string {
	if mode != "" && allowlistSize > 0 {
		return fmt.Sprintf("%s (%d allowed domains)", mode, allowlistSize)
	}
	return mode
}

// formatResources renders the CPU/memory/pids line, shared by `status` and
// `config show`.
func formatResources(cpus, memory string, pidsLimit int64) string {
	return fmt.Sprintf("%s CPUs, %s memory, %d pids", dashIfEmpty(cpus), dashIfEmpty(memory), pidsLimit)
}
