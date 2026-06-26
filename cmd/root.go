package cmd

import (
	"context"
	"fmt"
	"os"

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
	return &cli.Command{
		Name:        "devc",
		Usage:       "AI-safe development containers",
		Description: "Create and manage AI-safe development containers with devcontainer.json support.",
		Version:     version,
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
