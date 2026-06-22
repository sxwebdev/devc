package config

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/sxwebdev/devc/internal/agent"
	"github.com/sxwebdev/devc/pkg/types"
)

// configSnapshot captures the fields that affect container build/setup.
// A change to any of these means the container should be rebuilt.
type configSnapshot struct {
	Image             string            `json:"image"`
	Features          map[string]any    `json:"features,omitempty"`
	Agents            []string          `json:"agents,omitempty"`
	SecurityProfile   string            `json:"securityProfile,omitempty"`
	PostCreateCommand any               `json:"postCreateCommand,omitempty"`
	OnCreateCommand   any               `json:"onCreateCommand,omitempty"`
	ContainerEnv      map[string]string `json:"containerEnv,omitempty"`
	ForwardPorts      []any             `json:"forwardPorts,omitempty"`
	EnvPassthrough    []string          `json:"envPassthrough,omitempty"`
	ResourcesCPUs     string            `json:"cpus,omitempty"`
	ResourcesMemory   string            `json:"memory,omitempty"`
	NetworkMode       string            `json:"networkMode,omitempty"`
	AgentMounts       []mountSnapshot   `json:"agentMounts,omitempty"`
	CredentialPolicy  string            `json:"credentialPolicy,omitempty"`
	GitPolicy         string            `json:"gitPolicy,omitempty"`
	Skills            *skillsSnapshot   `json:"skills,omitempty"`
	Services          []serviceSnapshot `json:"services,omitempty"`
}

type serviceSnapshot struct {
	Name          string            `json:"name"`
	Image         string            `json:"image,omitempty"`
	ContainerPort int               `json:"containerPort,omitempty"`
	HostPort      int               `json:"hostPort,omitempty"`
	AgentEnv      map[string]string `json:"agentEnv,omitempty"`
}

type skillsSnapshot struct {
	Enabled  bool   `json:"enabled"`
	Source   string `json:"source,omitempty"`
	Target   string `json:"target,omitempty"`
	ReadOnly bool   `json:"readOnly"`
}

type mountSnapshot struct {
	HostPath string `json:"hostPath"`
	ReadOnly bool   `json:"readOnly"`
	Copy     bool   `json:"copy"`
}

// ConfigHash computes a hash of all config fields that affect how the container
// is built and configured. Two configs with the same hash produce identical containers.
func ConfigHash(devCfg *types.DevContainerConfig, custom *types.DevcCustomization) string {
	snap := configSnapshot{
		Image:             devCfg.Image,
		Features:          devCfg.Features,
		Agents:            custom.ResolvedAgents(),
		SecurityProfile:   custom.SecurityProfile,
		PostCreateCommand: devCfg.PostCreateCommand,
		OnCreateCommand:   devCfg.OnCreateCommand,
		ContainerEnv:      devCfg.ContainerEnv,
		CredentialPolicy:  custom.CredentialPolicy,
		GitPolicy:         custom.GitPolicy,
		ForwardPorts:      devCfg.ForwardPorts,
	}

	if len(custom.Services) > 0 {
		names := make([]string, 0, len(custom.Services))
		for name := range custom.Services {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			svc := custom.Services[name]
			if svc == nil || !svc.Enabled {
				continue
			}
			snap.Services = append(snap.Services, serviceSnapshot{
				Name:          name,
				Image:         svc.Image,
				ContainerPort: svc.ContainerPort,
				HostPort:      svc.HostPort,
				AgentEnv:      svc.AgentEnv,
			})
		}
	}

	if custom.Skills != nil && custom.Skills.Enabled {
		ro := true
		if custom.Skills.ReadOnly != nil {
			ro = *custom.Skills.ReadOnly
		}
		snap.Skills = &skillsSnapshot{
			Enabled:  true,
			Source:   custom.Skills.Source,
			Target:   custom.Skills.Target,
			ReadOnly: ro,
		}
	}

	if custom.EnvPassthrough != nil {
		sorted := make([]string, len(custom.EnvPassthrough))
		copy(sorted, custom.EnvPassthrough)
		sort.Strings(sorted)
		snap.EnvPassthrough = sorted
	}
	if custom.Resources != nil {
		snap.ResourcesCPUs = custom.Resources.CPUs
		snap.ResourcesMemory = custom.Resources.Memory
	}
	if custom.Network != nil {
		snap.NetworkMode = custom.Network.Mode
	}

	// Include agent mount specs so changes to mount modes trigger rebuild
	for _, name := range custom.ResolvedAgents() {
		if profile := agent.GetProfile(name); profile != nil {
			for _, m := range profile.ConfigMounts {
				snap.AgentMounts = append(snap.AgentMounts, mountSnapshot{
					HostPath: m.HostPath,
					ReadOnly: m.ReadOnly,
					Copy:     m.Copy,
				})
			}
		}
	}

	data, err := json.Marshal(snap)
	if err != nil {
		return "unknown"
	}

	h := sha256.Sum256(data)
	return fmt.Sprintf("%x", h[:8])
}
