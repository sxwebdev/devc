package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/sxwebdev/devc/internal/agent"
	"github.com/sxwebdev/devc/internal/config"
	"github.com/sxwebdev/devc/internal/preset"
)

func newInitCmd() *cobra.Command {
	var (
		agentFlag   string
		imageFlag   string
		presetFlag  string
		listImages  bool
		listAgents  bool
		listPresets bool
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
					if p.InstallCmd != "" {
						installCmds = append(installCmds, p.InstallCmd)
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
			return nil
		},
	}

	cmd.Flags().StringVar(&agentFlag, "agent", "", "pre-configure AI agents, comma-separated (use --list-agents to see options)")
	cmd.Flags().StringVar(&imageFlag, "image", "", "base image name or full reference (use --list-images to see options)")
	cmd.Flags().StringVar(&presetFlag, "preset", "", "apply a security preset (use --list-presets to see options)")
	cmd.Flags().BoolVar(&listImages, "list-images", false, "list available base images")
	cmd.Flags().BoolVar(&listAgents, "list-agents", false, "list available AI agent profiles")
	cmd.Flags().BoolVar(&listPresets, "list-presets", false, "list available security presets")

	return cmd
}

// secureDevcConfig produces a self-documenting devc customization block for a
// security preset. The "preset" field drives behavior at runtime; the explicit
// fields make the resulting protections visible and easy to tweak.
func secureDevcConfig(presetName string) map[string]any {
	return map[string]any{
		"preset":           presetName,
		"securityProfile":  "moderate",
		"credentialPolicy": "agentOnly",
		"gitPolicy":        "commitOnly",
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
		"workspaceSecretsPolicy": map[string]any{
			"enabled":       true,
			"mode":          "fail",
			"patterns":      []string{".env", ".env.*", "*.env", "config.yaml", "config.yml", "secrets.yaml", "secrets.yml", "credentials.json", "service-account*.json", ".npmrc", ".pypirc", ".netrc"},
			"allowPatterns": []string{".env.example", ".env.sample", "*.example.yaml", "*.sample.yaml"},
		},
		"skills": map[string]any{
			"enabled":  true,
			"source":   "~/.agent/skills",
			"target":   "/skills",
			"readonly": true,
			"required": false,
		},
	}
}
