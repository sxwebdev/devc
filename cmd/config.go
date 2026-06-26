package cmd

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/sxwebdev/devc/internal/agent"
	"github.com/sxwebdev/devc/internal/config"
	"github.com/sxwebdev/devc/pkg/types"
)

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Read and modify devcontainer configuration",
	}

	cmd.AddCommand(
		newConfigShowCmd(),
		newConfigSetCmd(),
		newConfigValidateCmd(),
		newConfigGlobalCmd(),
		newConfigAddFeatureCmd(),
		newConfigRemoveFeatureCmd(),
	)

	// Keep backward compat: `devc config [path]` still shows config. The parent
	// needs its own --output-format flag (subcommand flags aren't inherited) so
	// `devc config --output-format json` parses before delegating to show.
	addOutputFormatFlag(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		return runConfigShow(args)
	}
	cmd.Args = cobra.MaximumNArgs(1)

	return cmd
}

func newConfigShowCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show [path]",
		Short: "Display the effective (merged) configuration",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runConfigShow(args)
		},
	}

	addOutputFormatFlag(cmd)
	return cmd
}

// runConfigShow is shared by `config show` and the backward-compat bare
// `config` command. It must NOT construct a fresh command (doing so re-runs
// addOutputFormatFlag, whose StringVar would reset the shared flagOutputFormat
// and clobber the value parsed for the invoked command).
func runConfigShow(args []string) error {
	ws := getWorkspaceFolder(args)

	devCfg, merged, err := config.LoadMerged(ws)
	if err != nil {
		return err
	}

	if flagOutputFormat == "json" {
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

func newConfigSetCmd() *cobra.Command {
	var (
		imageFlag    string
		agentFlag    string
		securityFlag string
		cpusFlag     string
		memoryFlag   string
		networkFlag  string
	)

	cmd := &cobra.Command{
		Use:   "set [path]",
		Short: "Modify devcontainer.json settings",
		Long: `Modify devcontainer.json settings in place.

Examples:
  devc config set --image python
  devc config set --image myregistry/custom:v2
  devc config set --agent claude --security-profile strict
  devc config set --cpus 8 --memory 16g
  devc config set --network none`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ws := getWorkspaceFolder(args)
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

	cmd.Flags().StringVar(&imageFlag, "image", "", "base image name or full reference (use 'devc init --list-images')")
	cmd.Flags().StringVar(&agentFlag, "agent", "", "AI agent (claude, codex, gemini, opencode)")
	cmd.Flags().StringVar(&securityFlag, "security-profile", "", "security preset (strict, moderate, permissive)")
	cmd.Flags().StringVar(&cpusFlag, "cpus", "", "CPU limit (e.g., 4)")
	cmd.Flags().StringVar(&memoryFlag, "memory", "", "memory limit (e.g., 8g)")
	cmd.Flags().StringVar(&networkFlag, "network", "", "network mode (none, restricted, host)")

	return cmd
}

func newConfigAddFeatureCmd() *cobra.Command {
	var versionFlag string

	cmd := &cobra.Command{
		Use:   "add-feature <feature> [path]",
		Short: "Add a Dev Container Feature",
		Long: `Add a Dev Container Feature to devcontainer.json.

The feature argument can be a short name from the official registry or a full
OCI reference. Options can be passed as key=value after the feature name.

Examples:
  devc config add-feature git
  devc config add-feature node --version 20
  devc config add-feature ghcr.io/devcontainers/features/python:1
  devc config add-feature ghcr.io/devcontainers/features/docker-in-docker:2`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			feature := args[0]
			ws := getWorkspaceFolder(args[1:])
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

	cmd.Flags().StringVar(&versionFlag, "version", "", "feature version")

	return cmd
}

func newConfigRemoveFeatureCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove-feature <feature> [path]",
		Short: "Remove a Dev Container Feature",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			feature := args[0]
			ws := getWorkspaceFolder(args[1:])
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

func newConfigValidateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "validate [path]",
		Short: "Check the configuration for invalid values",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, merged, err := config.LoadMerged(getWorkspaceFolder(args))
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

func newConfigGlobalCmd() *cobra.Command {
	var initFlag bool

	cmd := &cobra.Command{
		Use:   "global",
		Short: "Show or initialize the global config (~/.devc/config.json)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
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

	cmd.Flags().BoolVar(&initFlag, "init", false, "write a default ~/.devc/config.json")
	return cmd
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
