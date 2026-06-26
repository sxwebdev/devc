package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/sxwebdev/devc/internal/agent"
	"github.com/sxwebdev/devc/internal/config"
	"github.com/sxwebdev/devc/internal/preset"
	"github.com/sxwebdev/devc/internal/secrets"
	"github.com/sxwebdev/devc/internal/services"
	"github.com/sxwebdev/devc/pkg/types"
)

func newInitCmd() *cobra.Command {
	var (
		agentFlag    string
		imageFlag    string
		presetFlag   string
		servicesFlag string
		listImages   bool
		listAgents   bool
		listPresets  bool
	)

	cmd := &cobra.Command{
		Use:   "init [path]",
		Short: "Initialize a devcontainer.json with AI safety defaults",
		Long: `Initialize a devcontainer.json with AI safety defaults.

Use --image to select a base image by name, or --list-images to see
all available images. If --image is not specified, defaults to "base" (Ubuntu).

Use --agent to pre-configure an AI coding agent. This adds the agent's
binary install command, network allowlist entries, and environment
variables. Use --list-agents to see options.

Use --services to scaffold sibling service containers (e.g. postgres, redis),
comma-separated. None are added by default. Run 'devc service list' to see the
catalog, or add them later with 'devc service add'.

You can also pass a full image reference directly (e.g., --image myregistry/myimage:tag).`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if listImages {
				fmt.Print("Available images:\n\n")
				fmt.Print(config.FormatImageList())
				return nil
			}

			if listAgents {
				fmt.Print("Available agents:\n\n")
				fmt.Print(agent.FormatProfileList())
				fmt.Println()
				detected := agent.Detect()
				if len(detected) > 0 {
					fmt.Print("Detected on host:")
					for _, d := range detected {
						fmt.Printf(" %s", d.Name)
					}
					fmt.Println()
				}
				return nil
			}

			if listPresets {
				fmt.Print("Available presets:\n\n")
				for _, name := range preset.Names() {
					fmt.Printf("  %s\n", name)
				}
				return nil
			}

			if presetFlag != "" && !preset.Exists(presetFlag) {
				return fmt.Errorf("unknown preset %q; use --list-presets to see options", presetFlag)
			}

			ws := getWorkspaceFolder(args)
			dir := filepath.Join(ws, ".devcontainer")
			target := filepath.Join(dir, "devcontainer.json")

			if _, err := os.Stat(target); err == nil {
				return fmt.Errorf("%s already exists; use 'devc config set' to modify it", target)
			}

			// Resolve image
			imageRef := "mcr.microsoft.com/devcontainers/base:ubuntu"
			if imageFlag != "" {
				if img := config.FindImage(imageFlag); img != nil {
					imageRef = img.Reference
				} else {
					imageRef = imageFlag
				}
			}

			var devcConfig map[string]any
			if presetFlag != "" {
				devcConfig = secureDevcConfig(presetFlag)
			} else {
				devcConfig = map[string]any{
					"securityProfile": "moderate",
					"network": map[string]any{
						"mode":      "restricted",
						"allowlist": []string{},
					},
					"resources": map[string]any{
						"cpus":      "4",
						"memory":    "8g",
						"pidsLimit": 256,
					},
					"session": map[string]any{
						"stopOnLastDetach": true,
					},
				}
			}

			// Scaffold opt-in sibling services (postgres, redis, ...) into both the
			// preset and non-preset paths.
			var serviceNames []string
			if servicesFlag != "" {
				svcMap, names, err := buildServicesMap(servicesFlag)
				if err != nil {
					return err
				}
				// Skip the assignment when nothing resolved (e.g. "--services ,")
				// so the output never carries a junk "services": null key.
				if len(svcMap) > 0 {
					devcConfig["services"] = svcMap
					serviceNames = names
				}
			}

			cfg := map[string]any{
				"name":  filepath.Base(ws),
				"image": imageRef,
			}

			// Apply agent profiles
			if agentFlag != "" {
				var agentNames []string
				var allAllowlist []string
				var installCmds []string
				var envPass []string
				envSeen := make(map[string]bool)
				allowSeen := make(map[string]bool)

				// Seed the allowlist with any baseline already in the config (e.g.
				// the strict preset's package-registry domains) so the agent's own
				// domains are unioned in rather than replacing it.
				if net, ok := devcConfig["network"].(map[string]any); ok {
					for _, d := range allowlistStrings(net["allowlist"]) {
						if !allowSeen[d] {
							allowSeen[d] = true
							allAllowlist = append(allAllowlist, d)
						}
					}
				}

				for name := range strings.SplitSeq(agentFlag, ",") {
					name = strings.TrimSpace(name)
					if name == "" {
						continue
					}
					p := agent.GetProfile(name)
					if p == nil {
						return fmt.Errorf("unknown agent %q; use --list-agents to see options", name)
					}
					agentNames = append(agentNames, name)
					for _, d := range p.NetworkAllow {
						if !allowSeen[d] {
							allowSeen[d] = true
							allAllowlist = append(allAllowlist, d)
						}
					}
					if cmd := p.GuardedInstallCmd(); cmd != "" {
						installCmds = append(installCmds, cmd)
					}
					for _, e := range p.EnvPassthrough {
						if !envSeen[e] {
							envSeen[e] = true
							envPass = append(envPass, e)
						}
					}
				}

				if len(agentNames) == 1 {
					devcConfig["agent"] = agentNames[0]
				} else {
					devcConfig["agents"] = agentNames
				}
				devcConfig["network"].(map[string]any)["allowlist"] = allAllowlist
				if len(installCmds) == 1 {
					cfg["postCreateCommand"] = installCmds[0]
				} else if len(installCmds) > 1 {
					cfg["postCreateCommand"] = strings.Join(installCmds, " && ")
				}
				if len(envPass) > 0 {
					devcConfig["envPassthrough"] = envPass
				}
			}

			cfg["customizations"] = map[string]any{
				"devc": devcConfig,
			}

			data, err := json.MarshalIndent(cfg, "", "  ")
			if err != nil {
				return err
			}

			if err := os.MkdirAll(dir, 0o755); err != nil {
				return err
			}

			if err := os.WriteFile(target, append(data, '\n'), 0o644); err != nil {
				return err
			}

			fmt.Printf("Created %s\n", target)
			fmt.Printf("Image:  %s\n", imageRef)
			if agentFlag != "" {
				for name := range strings.SplitSeq(agentFlag, ",") {
					name = strings.TrimSpace(name)
					if p := agent.GetProfile(name); p != nil {
						fmt.Printf("Agent:  %s (%s)\n", name, p.DisplayName)
					}
				}
			}
			if len(serviceNames) > 0 {
				fmt.Printf("Services: %s\n", strings.Join(serviceNames, ", "))
			}
			fmt.Println()
			fmt.Println("Next: run 'devc up' to start the container")
			return nil
		},
	}

	cmd.Flags().StringVar(&agentFlag, "agent", "", "pre-configure AI agents, comma-separated (use --list-agents to see options)")
	cmd.Flags().StringVar(&imageFlag, "image", "", "base image name or full reference (use --list-images to see options)")
	cmd.Flags().StringVar(&presetFlag, "preset", "", "apply a security preset (use --list-presets to see options)")
	cmd.Flags().StringVar(&servicesFlag, "services", "", "comma-separated sibling services to scaffold (e.g. postgres,redis; see 'devc service list')")
	cmd.Flags().BoolVar(&listImages, "list-images", false, "list available base images")
	cmd.Flags().BoolVar(&listAgents, "list-agents", false, "list available AI agent profiles")
	cmd.Flags().BoolVar(&listPresets, "list-presets", false, "list available security presets")

	return cmd
}

// secureDevcConfig produces a self-documenting devc customization block for a
// security preset. The security posture is derived entirely from the preset
// (the single source of truth), so init never keeps a parallel hardcoded copy
// of credentialPolicy/gitPolicy/network/skills that could silently drift. init
// only layers on editable starter conveniences that are not part of the preset.
func secureDevcConfig(presetName string) map[string]any {
	c := preset.Apply(presetName)
	if c == nil {
		c = &types.DevcCustomization{}
	}
	c.Preset = presetName

	// Materialize the built-in default secret lists as an editable starter set
	// that matches runtime detection exactly (the preset leaves them empty so the
	// runtime falls back to these same defaults).
	if c.WorkspaceSecretsPolicy != nil {
		c.WorkspaceSecretsPolicy.Patterns = secrets.DefaultPatterns()
		c.WorkspaceSecretsPolicy.AllowPatterns = secrets.DefaultAllowPatterns()
	}

	// Ensure a network block exists so the agent-domain allowlist has a place to
	// be written; presets that already define one (strict's enforced firewall)
	// keep their settings.
	if c.Network == nil {
		c.Network = &types.NetworkConfig{Mode: "restricted"}
	}

	devc := customizationToMap(c)

	// init-only starter conveniences (not part of the security preset).
	devc["resources"] = map[string]any{
		"cpus":      "4",
		"memory":    "8g",
		"pidsLimit": 256,
	}
	devc["session"] = map[string]any{
		"stopOnLastDetach": true,
	}
	// Services are opt-in via `devc init --services` / `devc service add`, never
	// scaffolded by default — a project that needs no database should not get one.
	return devc
}

// buildServicesMap parses a comma-separated service list, validates each name
// against the catalog, and renders the chosen service templates into a generic
// JSON object that merges cleanly into the devc customization map. It returns
// the rendered map and the sorted, deduplicated list of service names added.
func buildServicesMap(csv string) (map[string]any, []string, error) {
	svcConfigs := make(map[string]*types.ServiceConfig)
	for name := range strings.SplitSeq(csv, ",") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		tmpl, ok := services.Template(name)
		if !ok {
			return nil, nil, fmt.Errorf("unknown service %q; run 'devc service list' to see options", name)
		}
		svcConfigs[name] = tmpl
	}
	if len(svcConfigs) == 0 {
		return nil, nil, nil
	}

	svcMap, err := config.ToRawMap(svcConfigs)
	if err != nil {
		return nil, nil, err
	}

	names := make([]string, 0, len(svcConfigs))
	for name := range svcConfigs {
		names = append(names, name)
	}
	sort.Strings(names)
	return svcMap, names, nil
}

// customizationToMap renders a customization as a generic JSON object so the
// init flow can extend it with fields that are not part of the preset.
func customizationToMap(c *types.DevcCustomization) map[string]any {
	m, _ := config.ToRawMap(c)
	return m
}

// allowlistStrings coerces a network allowlist value into []string. A hand-built
// config carries []string; one rendered from a struct via JSON carries []any.
func allowlistStrings(v any) []string {
	switch s := v.(type) {
	case []string:
		return s
	case []any:
		out := make([]string, 0, len(s))
		for _, e := range s {
			if str, ok := e.(string); ok {
				out = append(out, str)
			}
		}
		return out
	}
	return nil
}
