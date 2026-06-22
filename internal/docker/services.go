package docker

import (
	"context"
	"fmt"
	"net/netip"
	"strconv"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/network"
	dockerclient "github.com/moby/moby/client"
)

// Labels applied to devc-managed networks and service containers so they can be
// discovered and cleaned up by the parent (agent) container name.
const (
	LabelManaged = "devc.managed"
	LabelParent  = "devc.parent"
	LabelService = "devc.service"
)

// ServiceVolumeSpec is a named volume mounted into a service container.
type ServiceVolumeSpec struct {
	VolumeName string
	Target     string
}

// ServiceSpec describes a sibling service container (e.g. Postgres, Redis).
type ServiceSpec struct {
	ContainerName string // full container name on the host
	Parent        string // agent container name, used for cleanup grouping
	Alias         string // DNS alias on the shared network (e.g. "postgres")
	Image         string
	NetworkName   string
	Env           []string
	ContainerPort int
	HostPort      int    // 0 = do not publish to the host
	HostIP        string // default 127.0.0.1
	Volumes       []ServiceVolumeSpec
}

// EnsureNetwork creates a user-defined bridge network if it does not already
// exist and returns its name. Existing networks (by name) are reused so repeated
// `devc up` calls are idempotent.
func (c *Client) EnsureNetwork(name, parent string) (string, error) {
	ctx := context.Background()

	f := make(dockerclient.Filters)
	f.Add("name", name)
	list, err := c.api.NetworkList(ctx, dockerclient.NetworkListOptions{Filters: f})
	if err == nil {
		for _, n := range list.Items {
			if n.Name == name {
				return name, nil
			}
		}
	}

	_, err = c.api.NetworkCreate(ctx, name, dockerclient.NetworkCreateOptions{
		Driver:     "bridge",
		Attachable: true,
		Labels: map[string]string{
			LabelManaged: "true",
			LabelParent:  parent,
		},
	})
	if err != nil {
		return "", fmt.Errorf("creating network %s: %w", name, err)
	}
	return name, nil
}

// CreateService creates and starts a sibling service container attached to the
// shared network with a DNS alias. It is idempotent: an existing container with
// the same name is reused (started if stopped). Named volumes are created on
// demand and preserved across runs.
func (c *Client) CreateService(spec ServiceSpec) error {
	ctx := context.Background()

	// Reuse an existing service container if present.
	switch c.Inspect(spec.ContainerName).State {
	case StateRunning:
		return nil
	case StateStopped, StateCreated:
		return c.Start(spec.ContainerName)
	}

	// Ensure named volumes exist (idempotent) and build mounts.
	var mounts []mount.Mount
	for _, v := range spec.Volumes {
		if _, err := c.api.VolumeCreate(ctx, dockerclient.VolumeCreateOptions{
			Name: v.VolumeName,
			Labels: map[string]string{
				LabelManaged: "true",
				LabelParent:  spec.Parent,
			},
		}); err != nil {
			return fmt.Errorf("creating volume %s: %w", v.VolumeName, err)
		}
		mounts = append(mounts, mount.Mount{
			Type:   mount.TypeVolume,
			Source: v.VolumeName,
			Target: v.Target,
		})
	}

	cfg := &container.Config{
		Image:  spec.Image,
		Env:    spec.Env,
		Labels: map[string]string{LabelManaged: "true", LabelParent: spec.Parent, LabelService: spec.Alias},
	}

	hostCfg := &container.HostConfig{
		Mounts:      mounts,
		NetworkMode: container.NetworkMode(spec.NetworkName),
		RestartPolicy: container.RestartPolicy{
			Name: container.RestartPolicyUnlessStopped,
		},
	}

	// Publish the container port to the host (localhost-only by default).
	if spec.ContainerPort > 0 {
		port, ok := network.PortFrom(uint16(spec.ContainerPort), network.TCP)
		if !ok {
			return fmt.Errorf("invalid container port %d for service %s", spec.ContainerPort, spec.Alias)
		}
		cfg.ExposedPorts = network.PortSet{port: struct{}{}}
		if spec.HostPort > 0 {
			hostIP := spec.HostIP
			if hostIP == "" {
				hostIP = "127.0.0.1"
			}
			addr, addrErr := netip.ParseAddr(hostIP)
			if addrErr != nil {
				return fmt.Errorf("invalid hostIP %q for service %s: %w", hostIP, spec.Alias, addrErr)
			}
			hostCfg.PortBindings = network.PortMap{
				port: []network.PortBinding{{HostIP: addr, HostPort: strconv.Itoa(spec.HostPort)}},
			}
		}
	}

	netCfg := &network.NetworkingConfig{
		EndpointsConfig: map[string]*network.EndpointSettings{
			spec.NetworkName: {Aliases: []string{spec.Alias}},
		},
	}

	createResult, err := c.api.ContainerCreate(ctx, dockerclient.ContainerCreateOptions{
		Config:           cfg,
		HostConfig:       hostCfg,
		NetworkingConfig: netCfg,
		Name:             spec.ContainerName,
	})
	if err != nil {
		return fmt.Errorf("creating service %s: %w", spec.Alias, err)
	}
	if _, err := c.api.ContainerStart(ctx, createResult.ID, dockerclient.ContainerStartOptions{}); err != nil {
		return fmt.Errorf("starting service %s: %w", spec.Alias, err)
	}
	return nil
}

// RemoveServicesForParent removes all service containers created for the given
// parent (agent) container. Named volumes are preserved.
func (c *Client) RemoveServicesForParent(parent string) error {
	ctx := context.Background()

	f := make(dockerclient.Filters)
	f.Add("label", LabelParent+"="+parent)
	f.Add("label", LabelService)
	list, err := c.api.ContainerList(ctx, dockerclient.ContainerListOptions{All: true, Filters: f})
	if err != nil {
		return err
	}
	for _, ctr := range list.Items {
		if _, err := c.api.ContainerRemove(ctx, ctr.ID, dockerclient.ContainerRemoveOptions{Force: true}); err != nil {
			return err
		}
	}
	return nil
}

// RemoveNetworkForParent removes the devc network created for the given parent
// container, if any.
func (c *Client) RemoveNetworkForParent(parent string) error {
	ctx := context.Background()

	f := make(dockerclient.Filters)
	f.Add("label", LabelParent+"="+parent)
	list, err := c.api.NetworkList(ctx, dockerclient.NetworkListOptions{Filters: f})
	if err != nil {
		return err
	}
	for _, n := range list.Items {
		if _, err := c.api.NetworkRemove(ctx, n.ID, dockerclient.NetworkRemoveOptions{}); err != nil {
			return err
		}
	}
	return nil
}
