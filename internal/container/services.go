package container

import (
	"fmt"
	"regexp"
	"sort"

	"github.com/sxwebdev/devc/internal/docker"
	"github.com/sxwebdev/devc/internal/security"
	"github.com/sxwebdev/devc/pkg/types"
)

// serviceKeyRe matches service keys that are safe to use as a DNS alias,
// container-name suffix, and connection-string host: a lowercase DNS label.
var serviceKeyRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

// effectiveNetworkMode resolves the container's network mode the same way the
// docker layer does: the security profile's mode, overridden by an explicit
// customizations.devc.network.mode.
func effectiveNetworkMode(custom *types.DevcCustomization) string {
	mode := security.GetProfile(custom.SecurityProfile).Network.Mode
	if custom.Network != nil && custom.Network.Mode != "" {
		mode = custom.Network.Mode
	}
	return mode
}

// servicesNetworkOK reports whether the effective network mode provides a
// private network where sibling services resolve by DNS alias. "host" and
// "none" do not, so services cannot be reached via their aliases there.
func servicesNetworkOK(custom *types.DevcCustomization) bool {
	m := effectiveNetworkMode(custom)
	return m != "none" && m != "host"
}

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
	for _, name := range custom.EnabledServiceNames() {
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
	for _, name := range custom.EnabledServiceNames() {
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
		if !serviceKeyRe.MatchString(spec.Alias) {
			return fmt.Errorf("invalid service name %q: use a lowercase DNS label ([a-z0-9-], e.g. \"postgres\")", spec.Alias)
		}
		if spec.Image == "" {
			return fmt.Errorf("service %q has no image", spec.Alias)
		}
		// A host port can only be published when the container port is known.
		// For non-well-known services, set containerPort explicitly.
		if spec.HostPort > 0 && spec.ContainerPort == 0 {
			m.warn(
				"service %q sets hostPort but no containerPort (and no default is known); host port not published — set \"containerPort\"",
				spec.Alias,
			)
		}
		fmt.Printf("Starting service %s (%s)...\n", spec.Alias, spec.Image)
		if err := m.Docker.CreateService(spec); err != nil {
			return err
		}
	}
	return nil
}

// ensureServicesForExisting re-creates the shared network and any missing
// sibling services for an already-existing agent container (idempotent). This
// recovers connectivity when a service or the network was removed out-of-band
// between `devc up` calls. The agent's env was baked at create time, so this
// only restores the resources its connection strings already point at.
func (m *Manager) ensureServicesForExisting(containerName string, custom *types.DevcCustomization) {
	if !servicesEnabled(custom) || !servicesNetworkOK(custom) {
		return
	}
	if err := m.setupServices(containerName, serviceNetworkName(containerName), custom); err != nil {
		m.warn("could not ensure services: %v", err)
	}
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
