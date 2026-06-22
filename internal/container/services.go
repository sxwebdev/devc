package container

import (
	"fmt"
	"os"
	"sort"

	"github.com/sxwebdev/devc/internal/docker"
	"github.com/sxwebdev/devc/pkg/types"
)

// servicesEnabled reports whether any service is enabled.
func servicesEnabled(custom *types.DevcCustomization) bool {
	for _, svc := range custom.Services {
		if svc != nil && svc.Enabled {
			return true
		}
	}
	return false
}

// serviceNetworkName derives the per-project network name from the agent
// container name (which is already path-deterministic).
func serviceNetworkName(containerName string) string {
	return "devc-net-" + containerName
}

// enabledServiceNames returns the enabled service keys in deterministic order.
func enabledServiceNames(custom *types.DevcCustomization) []string {
	var names []string
	for name, svc := range custom.Services {
		if svc != nil && svc.Enabled {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// containerPortFor returns the configured container port or a known default.
func containerPortFor(name string, svc *types.ServiceConfig) int {
	if svc.ContainerPort > 0 {
		return svc.ContainerPort
	}
	return defaultServicePorts[name]
}

// buildServiceSpecs translates the enabled services into docker service specs.
func buildServiceSpecs(custom *types.DevcCustomization, containerName, networkName string) []docker.ServiceSpec {
	var specs []docker.ServiceSpec
	for _, name := range enabledServiceNames(custom) {
		svc := custom.Services[name]

		env := make([]string, 0, len(svc.Env))
		for k, v := range svc.Env {
			env = append(env, k+"="+v)
		}
		sort.Strings(env)

		var vols []docker.ServiceVolumeSpec
		for _, v := range svc.Volumes {
			volName := v.Name
			if volName == "" {
				continue
			}
			// Namespace the volume by parent so projects don't collide.
			vols = append(vols, docker.ServiceVolumeSpec{
				VolumeName: containerName + "-" + volName,
				Target:     v.Target,
			})
		}

		specs = append(specs, docker.ServiceSpec{
			ContainerName: containerName + "-" + name,
			Parent:        containerName,
			Alias:         name,
			Image:         svc.Image,
			NetworkName:   networkName,
			Env:           env,
			ContainerPort: containerPortFor(name, svc),
			HostPort:      svc.HostPort,
			HostIP:        svc.HostIP,
			Volumes:       vols,
		})
	}
	return specs
}

// serviceEnv derives connection-string env vars injected into the agent
// container. An explicit agentEnv on a service overrides the default derivation;
// otherwise a well-known service key (see connStringBuilders) gets a sensible
// default. Hosts use the service DNS alias.
func serviceEnv(custom *types.DevcCustomization) []string {
	var env []string
	for _, name := range enabledServiceNames(custom) {
		svc := custom.Services[name]

		// Explicit override: inject the user-provided env verbatim.
		if len(svc.AgentEnv) > 0 {
			keys := make([]string, 0, len(svc.AgentEnv))
			for k := range svc.AgentEnv {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				env = append(env, k+"="+svc.AgentEnv[k])
			}
			continue
		}

		if build, ok := connStringBuilders[name]; ok {
			if e := build(svc, name, containerPortFor(name, svc)); e != "" {
				env = append(env, e)
			}
		}
	}
	return env
}

func valueOr(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

// setupServices creates the shared network and starts the enabled service
// containers. It is idempotent across repeated `devc up` calls.
func (m *Manager) setupServices(containerName, networkName string, custom *types.DevcCustomization) error {
	if _, err := m.Docker.EnsureNetwork(networkName, containerName); err != nil {
		return err
	}
	for _, spec := range buildServiceSpecs(custom, containerName, networkName) {
		if spec.Image == "" {
			return fmt.Errorf("service %q has no image", spec.Alias)
		}
		// A host port can only be published when the container port is known.
		// For non-well-known services, set containerPort explicitly.
		if spec.HostPort > 0 && spec.ContainerPort == 0 {
			_, _ = fmt.Fprintf(os.Stderr,
				"warning: service %q sets hostPort but no containerPort (and no default is known); host port not published — set \"containerPort\"\n",
				spec.Alias)
		}
		fmt.Printf("Starting service %s (%s)...\n", spec.Alias, spec.Image)
		if err := m.Docker.CreateService(spec); err != nil {
			return err
		}
	}
	return nil
}

// cleanupServices removes service containers and the network for a parent
// container. Named volumes are preserved.
func (m *Manager) cleanupServices(containerName string) {
	if err := m.Docker.RemoveServicesForParent(containerName); err != nil {
		fmt.Printf("warning: could not remove services for %s: %v\n", containerName, err)
	}
	if err := m.Docker.RemoveNetworkForParent(containerName); err != nil {
		fmt.Printf("warning: could not remove network for %s: %v\n", containerName, err)
	}
}
