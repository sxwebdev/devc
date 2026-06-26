package cmd

import (
	"context"
	"strings"

	"github.com/sxwebdev/devc/internal/container"
	"github.com/urfave/cli/v3"
)

func newUpCmd() *cli.Command {
	var (
		agentFlag    string
		securityFlag string
		detachFlag   bool
		rebuildFlag  bool
		yesFlag      bool
		noFlag       bool
	)

	return &cli.Command{
		Name:      "up",
		Usage:     "Create and start a development container",
		Arguments: []cli.Argument{&cli.StringArg{Name: "path"}},
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "agent", Usage: "AI agent profiles, comma-separated (claude,codex,copilot)", Destination: &agentFlag},
			&cli.StringFlag{Name: "security-profile", Usage: "security preset (strict, moderate, permissive)", Destination: &securityFlag},
			&cli.BoolFlag{Name: "detach", Usage: "don't attach after starting", Destination: &detachFlag},
			&cli.BoolFlag{Name: "rebuild", Usage: "force rebuild even if container exists", Destination: &rebuildFlag},
		},
		// --yes and --no are mutually exclusive: at most one may be set.
		MutuallyExclusiveFlags: []cli.MutuallyExclusiveFlags{
			{
				Flags: [][]cli.Flag{
					{&cli.BoolFlag{Name: "yes", Aliases: []string{"y"}, Usage: "answer the rebuild-on-config-change prompt with yes (non-interactive)", Destination: &yesFlag}},
					{&cli.BoolFlag{Name: "no", Usage: "answer the rebuild-on-config-change prompt with no (non-interactive)", Destination: &noFlag}},
				},
			},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			mgr, err := container.NewManager()
			if err != nil {
				return err
			}
			defer mgr.Close()

			var agents []string
			if agentFlag != "" {
				for a := range strings.SplitSeq(agentFlag, ",") {
					a = strings.TrimSpace(a)
					if a != "" {
						agents = append(agents, a)
					}
				}
			}

			return mgr.Up(container.UpOptions{
				WorkspaceFolder: workspaceFolder(cmd.StringArg("path")),
				Agents:          agents,
				SecurityProfile: securityFlag,
				Detach:          detachFlag,
				Rebuild:         rebuildFlag,
				AssumeYes:       yesFlag,
				AssumeNo:        noFlag,
			})
		},
	}
}
