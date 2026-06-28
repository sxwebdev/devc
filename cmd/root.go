package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/urfave/cli/v3"
)

// outputFormatFlag returns the shared --output-format flag used by the commands
// that produce machine-readable output (list, status, and config/config show).
// Each Action reads the value via cmd.String("output-format").
func outputFormatFlag() *cli.StringFlag {
	return &cli.StringFlag{
		Name:  "output-format",
		Value: "text",
		Usage: "output format (text, json)",
	}
}

func NewRootCmd(version string) *cli.Command {
	root := &cli.Command{
		Name:        "devc",
		Usage:       "AI-safe development containers",
		Description: "Create and manage AI-safe development containers with devcontainer.json support.",
		Version:     version,
		// A first positional that matches no subcommand falls through to here;
		// report it as an unknown command (exit 1) instead of urfave's default
		// "No help topic for 'X'" + exit 3. Bare `devc` shows help.
		Action: func(_ context.Context, cmd *cli.Command) error {
			if cmd.Args().Present() {
				return fmt.Errorf("unknown command %q for %q", cmd.Args().First(), cmd.Name)
			}
			return cli.ShowRootCommandHelp(cmd)
		},
		Commands: []*cli.Command{
			newUpCmd(),
			newExecCmd(),
			newAttachCmd(),
			newStopCmd(),
			newDownCmd(),
			newListCmd(),
			newStatusCmd(),
			newLogsCmd(),
			newCleanCmd(),
			newConfigCmd(),
			newInitCmd(),
			newServiceCmd(),
		},
	}

	guardArgs(root)
	suppressUsagePrint(root)
	return root
}

// guardArgs wraps each leaf command's Action to reject positional arguments left
// over after its typed cli.Arguments are parsed. urfave/cli binds and enforces
// argument minimums but never rejects surplus positionals, so this restores the
// MaximumNArgs/NoArgs behavior the cobra CLI had. Parent commands (which route to
// subcommands or carry backward-compat positional handling) are skipped.
func guardArgs(c *cli.Command) {
	if len(c.Commands) == 0 && c.Action != nil {
		inner := c.Action
		c.Action = func(ctx context.Context, cmd *cli.Command) error {
			if cmd.Args().Present() {
				return fmt.Errorf("unexpected argument(s): %s", strings.Join(cmd.Args().Slice(), " "))
			}
			return inner(ctx, cmd)
		}
	}
	for _, sub := range c.Commands {
		guardArgs(sub)
	}
}

// suppressUsagePrint routes every command's flag/usage errors through Execute's
// single print path. By default urfave prints "Incorrect Usage: ..." plus full
// help inline AND returns the error, so it would otherwise be printed twice.
func suppressUsagePrint(c *cli.Command) {
	c.OnUsageError = func(_ context.Context, _ *cli.Command, err error, _ bool) error {
		return err
	}
	for _, sub := range c.Commands {
		suppressUsagePrint(sub)
	}
}

func Execute(version string) {
	// urfave/cli prints flag-parsing ("Incorrect Usage") errors itself but not
	// errors returned from an Action, so surface them here before exiting.
	if err := NewRootCmd(version).Run(context.Background(), os.Args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// workspaceFolder returns path if non-empty, else the current working directory.
func workspaceFolder(path string) string {
	if path != "" {
		return path
	}
	dir, err := os.Getwd()
	if err != nil {
		return "."
	}
	return dir
}
