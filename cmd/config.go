package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/sxwebdev/devc/internal/agent"
	"github.com/sxwebdev/devc/internal/config"
	"github.com/sxwebdev/devc/pkg/types"
	"github.com/urfave/cli/v3"
)

func newConfigCmd() *cli.Command {
	return &cli.Command{
		Name:  "config",
		Usage: "Read and modify devcontainer configuration",
		Commands: []*cli.Command{
			newConfigShowCmd(),
			newConfigSetCmd(),
			newConfigValidateCmd(),
			newConfigGlobalCmd(),
			newConfigAddFeatureCmd(),
			newConfigRemoveFeatureCmd(),
		},
		// Keep backward compat: `devc config [path]` still shows config. The parent
		// needs its own --output-format flag (subcommand flags aren't inherited) so
		// `devc config --output-format json` parses before delegating to show.
		Arguments: []cli.Argument{&cli.StringArg{Name: "path"}},
		Flags:     []cli.Flag{outputFormatFlag()},
		Action: func(_ context.Context, cmd *cli.Command) error {
			return runConfigShow(cmd.StringArg("path"), cmd.String("output-format"))
		},
	}
}

func newConfigShowCmd() *cli.Command {
	return &cli.Command{
		Name:      "show",
		Usage:     "Display the effective (merged) configuration",
		Arguments: []cli.Argument{&cli.StringArg{Name: "path"}},
		Flags:     []cli.Flag{outputFormatFlag()},
		Action: func(_ context.Context, cmd *cli.Command) error {
			return runConfigShow(cmd.StringArg("path"), cmd.String("output-format"))
		},
	}
}

// runConfigShow is shared by `config show` and the backward-compat bare
// `config` command. The output format is passed in explicitly so the caller
// reads it from whichever command was actually invoked.
func runConfigShow(path, format string) error {
	ws := workspaceFolder(path)

	devCfg, merged, err := config.LoadMerged(ws)
	if err != nil {
		return err
	}

	if format == "json" {
		result := map[string]any{
			"devcontainer":   devCfg,
			"devc":           merged,
			"containerName":  config.ContainerName(ws),
			"workspaceMount": config.WorkspaceInContainer(devCfg, ws),
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(result); err != nil {
			return fmt.Errorf("encoding config: %w", err)
		}
		return nil
	}

	printConfigSummary(devCfg, merged, config.ContainerName(ws), config.WorkspaceInContainer(devCfg, ws))
	return nil
}

// printConfigSummary renders the key effective-config fields in a readable form.
// The full structure is available via `config show --output-format json`.
func printConfigSummary(devCfg *types.DevContainerConfig, merged *types.DevcCustomization, containerName, workspaceMount string) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	row := func(k, v string) { _, _ = fmt.Fprintf(w, "%s\t%s\n", k, v) }

	row("Container name:", containerName)
	row("Workspace mount:", workspaceMount)
	row("Image:", dashIfEmpty(devCfg.Image))
	row("Agents:", dashIfEmpty(strings.Join(merged.ResolvedAgents(), ", ")))
	row("Security profile:", dashIfEmpty(merged.SecurityProfile))

	netMode, netAllow := "", 0
	if merged.Network != nil {
		netMode = merged.Network.Mode
		netAllow = len(merged.Network.Allowlist)
	}
	row("Network:", dashIfEmpty(formatNetwork(netMode, netAllow)))

	if merged.Resources != nil {
		row("Resources:", formatResources(merged.Resources.CPUs, merged.Resources.Memory, merged.Resources.PidsLimit))
	}
	if merged.Preset != "" {
		row("Preset:", merged.Preset)
	}
	if merged.CredentialPolicy != "" {
		row("Credential policy:", merged.CredentialPolicy)
	}
	if merged.GitPolicy != "" {
		row("Git policy:", merged.GitPolicy)
	}
	if names := merged.EnabledServiceNames(); len(names) > 0 {
		row("Services:", strings.Join(names, ", "))
	}
	_ = w.Flush()

	fmt.Println()
	fmt.Println("Run 'devc config show --output-format json' for the full configuration.")
}

func newConfigSetCmd() *cli.Command {
	var (
		imageFlag    string
		agentFlag    string
		securityFlag string
		cpusFlag     string
		memoryFlag   string
		networkFlag  string
	)

	return &cli.Command{
		Name:  "set",
		Usage: "Modify devcontainer.json settings",
		Description: `Modify devcontainer.json settings in place.

Examples:
  devc config set --image python
  devc config set --image myregistry/custom:v2
  devc config set --agent claude --security-profile strict
  devc config set --cpus 8 --memory 16g
  devc config set --network none`,
		Arguments: []cli.Argument{&cli.StringArg{Name: "path"}},
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "image", Usage: "base image name or full reference (use 'devc init --list-images')", Destination: &imageFlag},
			&cli.StringFlag{Name: "agent", Usage: "AI agent (claude, codex, gemini, opencode)", Destination: &agentFlag},
			&cli.StringFlag{Name: "security-profile", Usage: "security preset (strict, moderate, permissive)", Destination: &securityFlag},
			&cli.StringFlag{Name: "cpus", Usage: "CPU limit (e.g., 4)", Destination: &cpusFlag},
			&cli.StringFlag{Name: "memory", Usage: "memory limit (e.g., 8g)", Destination: &memoryFlag},
			&cli.StringFlag{Name: "network", Usage: "network mode (none, restricted, host)", Destination: &networkFlag},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			ws := workspaceFolder(cmd.StringArg("path"))
			cfgPath := config.FindDevcontainerPath(ws)

			devCfg, err := config.LoadDevcontainerConfig(ws)
			if err != nil {
				return fmt.Errorf("no devcontainer.json found; run 'devc init' first")
			}

			changed := false

			// Image
			if imageFlag != "" {
				if img := config.FindImage(imageFlag); img != nil {
					devCfg.Image = img.Reference
					fmt.Printf("Image: %s (%s)\n", img.Name, img.Reference)
				} else {
					devCfg.Image = imageFlag
					fmt.Printf("Image: %s\n", imageFlag)
				}
				changed = true
			}

			// Devc customization fields
			custom, err := config.ExtractDevcCustomization(devCfg)
			if err != nil {
				return err
			}

			if agentFlag != "" {
				var agentNames []string
				var installCmds []string
				if custom.Network == nil {
					custom.Network = &types.NetworkConfig{Mode: "restricted"}
				}
				existing := make(map[string]bool)
				for _, d := range custom.Network.Allowlist {
					existing[d] = true
				}

				for name := range strings.SplitSeq(agentFlag, ",") {
					name = strings.TrimSpace(name)
					if name == "" {
						continue
					}
					p := agent.GetProfile(name)
					if p == nil {
						return fmt.Errorf("unknown agent %q; use 'devc init --list-agents' to see options", name)
					}
					agentNames = append(agentNames, name)
					fmt.Printf("Agent: %s (%s)\n", name, p.DisplayName)

					if cmd := p.GuardedInstallCmd(); cmd != "" {
						installCmds = append(installCmds, cmd)
					}
					if len(p.EnvVars) > 0 {
						if devCfg.ContainerEnv == nil {
							devCfg.ContainerEnv = make(map[string]string)
						}
						maps.Copy(devCfg.ContainerEnv, p.EnvVars)
					}
					for _, d := range p.NetworkAllow {
						if !existing[d] {
							existing[d] = true
							custom.Network.Allowlist = append(custom.Network.Allowlist, d)
						}
					}
				}

				if len(agentNames) == 1 {
					custom.Agent = agentNames[0]
					custom.Agents = nil
				} else {
					custom.Agents = agentNames
					custom.Agent = ""
				}
				if len(installCmds) == 1 {
					devCfg.PostCreateCommand = installCmds[0]
				} else if len(installCmds) > 1 {
					devCfg.PostCreateCommand = strings.Join(installCmds, " && ")
				}

				changed = true
			}
			if securityFlag != "" {
				if err := config.ValidateSecurityProfile(securityFlag); err != nil {
					return err
				}
				custom.SecurityProfile = securityFlag
				fmt.Printf("Security profile: %s\n", securityFlag)
				changed = true
			}
			if cpusFlag != "" {
				if custom.Resources == nil {
					custom.Resources = &types.ResourceConfig{}
				}
				custom.Resources.CPUs = cpusFlag
				fmt.Printf("CPUs: %s\n", cpusFlag)
				changed = true
			}
			if memoryFlag != "" {
				if custom.Resources == nil {
					custom.Resources = &types.ResourceConfig{}
				}
				custom.Resources.Memory = memoryFlag
				fmt.Printf("Memory: %s\n", memoryFlag)
				changed = true
			}
			if networkFlag != "" {
				if err := config.ValidateNetworkMode(networkFlag); err != nil {
					return err
				}
				if custom.Network == nil {
					custom.Network = &types.NetworkConfig{}
				}
				custom.Network.Mode = networkFlag
				fmt.Printf("Network: %s\n", networkFlag)
				changed = true
			}

			if !changed {
				return fmt.Errorf("no changes specified; use flags like --image, --agent, --cpus, --memory, --network, --security-profile")
			}

			// Write customization back
			if err := config.ApplyDevcCustomization(cfgPath, devCfg, custom); err != nil {
				return fmt.Errorf("saving config: %w", err)
			}

			fmt.Printf("Updated %s\n", cfgPath)
			return nil
		},
	}
}

func newConfigAddFeatureCmd() *cli.Command {
	var versionFlag string

	return &cli.Command{
		Name:  "add-feature",
		Usage: "Add a Dev Container Feature",
		Description: `Add a Dev Container Feature to devcontainer.json.

The feature argument can be a short name from the official registry or a full
OCI reference. Options can be passed as key=value after the feature name.

Examples:
  devc config add-feature git
  devc config add-feature node --version 20
  devc config add-feature ghcr.io/devcontainers/features/python:1
  devc config add-feature ghcr.io/devcontainers/features/docker-in-docker:2`,
		Arguments: []cli.Argument{
			&cli.StringArg{Name: "feature"},
			&cli.StringArg{Name: "path"},
		},
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "version", Usage: "feature version", Destination: &versionFlag},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			feature := cmd.StringArg("feature")
			if feature == "" {
				return fmt.Errorf("requires a feature name; e.g. 'devc config add-feature git'")
			}
			ws := workspaceFolder(cmd.StringArg("path"))
			cfgPath := config.FindDevcontainerPath(ws)

			devCfg, err := config.LoadDevcontainerConfig(ws)
			if err != nil {
				return fmt.Errorf("no devcontainer.json found; run 'devc init' first")
			}

			// Resolve feature reference
			featureRef := resolveFeatureRef(feature)

			// Build options from --version flag
			featureOpts := make(map[string]any)
			if versionFlag != "" {
				featureOpts["version"] = versionFlag
			}

			// Add to features map
			if devCfg.Features == nil {
				devCfg.Features = make(map[string]any)
			}

			var opts any = featureOpts
			if len(featureOpts) == 0 {
				opts = map[string]any{}
			}
			devCfg.Features[featureRef] = opts

			if err := config.SaveDevcontainerConfig(cfgPath, devCfg); err != nil {
				return fmt.Errorf("saving config: %w", err)
			}

			fmt.Printf("Added feature %s\n", featureRef)
			fmt.Printf("Updated %s\n", cfgPath)
			return nil
		},
	}
}

func newConfigRemoveFeatureCmd() *cli.Command {
	return &cli.Command{
		Name:  "remove-feature",
		Usage: "Remove a Dev Container Feature",
		Arguments: []cli.Argument{
			&cli.StringArg{Name: "feature"},
			&cli.StringArg{Name: "path"},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			feature := cmd.StringArg("feature")
			if feature == "" {
				return fmt.Errorf("requires a feature name; e.g. 'devc config remove-feature git'")
			}
			ws := workspaceFolder(cmd.StringArg("path"))
			cfgPath := config.FindDevcontainerPath(ws)

			devCfg, err := config.LoadDevcontainerConfig(ws)
			if err != nil {
				return fmt.Errorf("no devcontainer.json found; run 'devc init' first")
			}

			if devCfg.Features == nil {
				return fmt.Errorf("no features configured")
			}

			featureRef := resolveFeatureRef(feature)

			// Try exact match first, then prefix match
			deleted := false
			for key := range devCfg.Features {
				if key == featureRef || key == feature || strings.Contains(key, "/"+feature+":") || strings.HasSuffix(key, "/"+feature) {
					delete(devCfg.Features, key)
					fmt.Printf("Removed feature %s\n", key)
					deleted = true
					break
				}
			}

			if !deleted {
				return fmt.Errorf("feature %q not found in configuration", feature)
			}

			if err := config.SaveDevcontainerConfig(cfgPath, devCfg); err != nil {
				return fmt.Errorf("saving config: %w", err)
			}

			fmt.Printf("Updated %s\n", cfgPath)
			return nil
		},
	}
}

func newConfigValidateCmd() *cli.Command {
	return &cli.Command{
		Name:      "validate",
		Usage:     "Check the configuration for invalid values",
		Arguments: []cli.Argument{&cli.StringArg{Name: "path"}},
		Action: func(_ context.Context, cmd *cli.Command) error {
			_, merged, err := config.LoadMerged(workspaceFolder(cmd.StringArg("path")))
			if err != nil {
				return err
			}

			if err := config.ValidateCustomization(merged); err != nil {
				return err
			}

			fmt.Println("OK — configuration is valid")
			return nil
		},
	}
}

func newConfigGlobalCmd() *cli.Command {
	var initFlag bool

	return &cli.Command{
		Name:  "global",
		Usage: "Show or initialize the global config (~/.devc/config.json)",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "init", Usage: "write a default ~/.devc/config.json", Destination: &initFlag},
		},
		Action: func(_ context.Context, _ *cli.Command) error {
			path, err := config.GlobalConfigPath()
			if err != nil {
				return err
			}

			if initFlag {
				written, werr := config.WriteGlobalConfig(config.DefaultGlobalConfig())
				if werr != nil {
					return werr
				}
				fmt.Printf("Created %s\n", written)
				return nil
			}

			fmt.Printf("Path:   %s\n", path)
			if _, statErr := os.Stat(path); statErr == nil {
				fmt.Println("Status: present")
			} else {
				fmt.Println("Status: not present (built-in defaults in use; run 'devc config global --init' to create it)")
			}

			globalCfg, err := config.LoadGlobalConfig()
			if err != nil {
				return err
			}
			fmt.Println("\nEffective global defaults:")
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(globalCfg.Defaults)
		},
	}
}

// resolveFeatureRef expands short feature names to full OCI references.
func resolveFeatureRef(feature string) string {
	// Already a full reference
	if strings.Contains(feature, "/") {
		return feature
	}

	// Well-known official features
	return "ghcr.io/devcontainers/features/" + feature + ":latest"
}
