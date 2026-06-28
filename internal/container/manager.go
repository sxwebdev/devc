package container

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/sxwebdev/devc/internal/agent"
	"github.com/sxwebdev/devc/internal/config"
	"github.com/sxwebdev/devc/internal/credpolicy"
	"github.com/sxwebdev/devc/internal/docker"
	"github.com/sxwebdev/devc/internal/secretfsbin"
	"github.com/sxwebdev/devc/internal/secrets"
	"github.com/sxwebdev/devc/internal/security"
	"github.com/sxwebdev/devc/internal/session"
	"github.com/sxwebdev/devc/pkg/types"
	"golang.org/x/term"
)

// safeShellPathRe matches path components that are safe to interpolate into
// shell commands. This covers the output of claudeProjectKey, which replaces
// path separators with dashes. We reject anything outside this set to prevent
// command injection via workspace paths with unusual characters.
var safeShellPathRe = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)

// Manager orchestrates container lifecycle operations.
type Manager struct {
	Docker  *docker.Client
	Session *session.Tracker

	// warnings accumulates non-fatal setup warnings during an Up so they can be
	// summarized at the end instead of scrolling past in the build output.
	warnings []string
}

// warn records a non-fatal setup warning: it prints immediately to stderr (so
// the message stays near the operation that produced it) and accumulates the
// text so Up can show a summary at the end. The format must omit the "warning: "
// prefix and trailing newline.
func (m *Manager) warn(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	m.warnings = append(m.warnings, msg)
	_, _ = fmt.Fprintf(os.Stderr, "warning: %s\n", msg)
}

// printWarningSummary echoes accumulated setup warnings as a single block so
// they are not lost above a wall of image-build and lifecycle output.
func (m *Manager) printWarningSummary() {
	n := len(m.warnings)
	if n == 0 {
		return
	}
	noun := "warning"
	if n != 1 {
		noun += "s"
	}
	_, _ = fmt.Fprintf(os.Stderr, "\n⚠ %d setup %s:\n", n, noun)
	for _, w := range m.warnings {
		_, _ = fmt.Fprintf(os.Stderr, "  - %s\n", w)
	}
}

// NewManager creates a container manager.
func NewManager() (*Manager, error) {
	dc, err := docker.NewClient()
	if err != nil {
		return nil, err
	}

	tracker, err := session.NewTracker()
	if err != nil {
		return nil, err
	}

	return &Manager{Docker: dc, Session: tracker}, nil
}

// Implement closeable for Manager
func (m *Manager) Close() error {
	return m.Docker.Close()
}

// UpOptions configures the "up" command.
type UpOptions struct {
	WorkspaceFolder string
	Agents          []string // One or more agent profiles (comma-separated at CLI)
	SecurityProfile string
	Detach          bool
	Rebuild         bool // Force rebuild even if container exists
	AssumeYes       bool // Answer the config-drift rebuild prompt with "yes" (non-interactive)
	AssumeNo        bool // Answer the config-drift rebuild prompt with "no" (non-interactive)
}

// Up creates or starts a container for the workspace.
func (m *Manager) Up(opts UpOptions) error {
	m.warnings = nil

	devCfg, err := config.LoadDevcontainerConfig(opts.WorkspaceFolder)
	if err != nil {
		return err
	}

	globalCfg, err := config.LoadGlobalConfig()
	if err != nil {
		return err
	}

	custom, err := config.ExtractDevcCustomization(devCfg)
	if err != nil {
		return err
	}

	// CLI overrides
	if len(opts.Agents) > 0 {
		custom.Agents = opts.Agents
		custom.Agent = "" // Agents takes precedence
	}
	if opts.SecurityProfile != "" {
		custom.SecurityProfile = opts.SecurityProfile
	}

	merged := config.MergeCustomization(globalCfg, custom)

	// Validate enum fields up front so a typo produces an actionable message
	// here instead of an opaque failure at container runtime. Agent names are
	// intentionally NOT hard-validated: an unknown agent is skipped with a
	// warning below, so a single stale/typo'd name doesn't block the container.
	if err := config.ValidateEnums(merged); err != nil {
		return err
	}

	// Enforce the workspace secrets policy before any container operation so a
	// protected file blocks startup regardless of container state.
	if err := enforceWorkspaceSecrets(opts.WorkspaceFolder, merged); err != nil {
		return err
	}

	containerName := config.ContainerName(opts.WorkspaceFolder)

	// Resolve agent profiles
	var agentProfiles []*agent.Profile
	for _, name := range merged.ResolvedAgents() {
		p := agent.GetProfile(name)
		if p == nil {
			m.warn("unknown agent %q, skipping", name)
			continue
		}
		agentProfiles = append(agentProfiles, p)
	}

	// Compute config hash for drift detection
	currentHash := config.ConfigHash(devCfg, merged)

	// Check existing container state
	inspectResult := m.Docker.Inspect(containerName)
	state := inspectResult.State

	// Detect config drift on existing containers
	if !opts.Rebuild && (state == docker.StateRunning || state == docker.StateStopped || state == docker.StateCreated || state == docker.StatePaused) {
		storedHash := inspectResult.Labels["devc.config-hash"]
		if storedHash != "" && storedHash != currentHash {
			changes := describeChanges(inspectResult.Labels, devCfg, merged, agentProfiles)
			fmt.Printf("Configuration has changed since this container was created:\n")
			for _, change := range changes {
				fmt.Printf("  - %s\n", change)
			}
			switch {
			case opts.AssumeYes:
				fmt.Println("Rebuilding (--yes)")
				opts.Rebuild = true
			case opts.AssumeNo:
				fmt.Println("Continuing with existing container (--no)")
			case !term.IsTerminal(int(os.Stdin.Fd())):
				// No TTY (CI/piped): don't block on a prompt that can't be answered.
				fmt.Println("Non-interactive input; continuing with existing container (pass --yes to rebuild)")
			default:
				fmt.Printf("\nRebuild container? [y/N] ")
				if askYesNo() {
					opts.Rebuild = true
				} else {
					fmt.Println("Continuing with existing container")
				}
			}
		}
	}

	// Rebuild: remove existing container first
	if opts.Rebuild && state != docker.StateNotFound {
		fmt.Printf("Removing existing container %s...\n", containerName)
		if err := m.Docker.Remove(containerName, true); err != nil {
			return fmt.Errorf("removing container for rebuild: %w", err)
		}
		m.Session.Clean(containerName)
		state = docker.StateNotFound
	}

	switch state {
	case docker.StateRunning:
		// Re-ensure sibling services in case one was removed out-of-band.
		m.ensureServicesForExisting(containerName, merged)
		fmt.Printf("Container %s is already running\n", containerName)

	case docker.StateStopped, docker.StateCreated:
		// The shared network must exist before starting an agent attached to it.
		m.ensureServicesForExisting(containerName, merged)
		fmt.Printf("Starting existing container %s...\n", containerName)
		if err := m.Docker.Start(containerName); err != nil {
			return fmt.Errorf("starting container: %w", err)
		}

	case docker.StatePaused:
		fmt.Printf("Unpausing container %s...\n", containerName)
		if err := m.Docker.Unpause(containerName); err != nil {
			return fmt.Errorf("unpausing container: %w", err)
		}

	case docker.StateNotFound:
		if err := m.createContainer(containerName, devCfg, merged, opts.WorkspaceFolder, agentProfiles, currentHash); err != nil {
			// Surface any warnings collected before the failure.
			m.printWarningSummary()
			return err
		}
	}

	// Track session
	count, _ := m.Session.Attach(containerName)
	fmt.Printf("Container %s ready (%s)\n", containerName, session.FormatCount(count))

	m.printWarningSummary()

	if !opts.Detach {
		return m.Docker.Exec(containerName, []string{"/bin/bash"}, true)
	}

	return nil
}

// createContainer handles image pull, feature build, container creation, and lifecycle commands.
func (m *Manager) createContainer(
	containerName string,
	devCfg *types.DevContainerConfig,
	custom *types.DevcCustomization,
	workspaceFolder string,
	agentProfiles []*agent.Profile,
	configHash string,
) error {
	// Pull base image if needed
	if devCfg.Image != "" && !m.Docker.ImageExists(devCfg.Image) {
		fmt.Printf("Pulling image %s...\n", devCfg.Image)
		if err := m.Docker.Pull(devCfg.Image); err != nil {
			return fmt.Errorf("pulling image: %w", err)
		}
	}

	// Build image with features if any are configured
	effectiveImage := devCfg.Image
	if len(devCfg.Features) > 0 {
		built, buildErr := m.Docker.BuildImageWithFeatures(devCfg.Image, devCfg.Features, containerName)
		if buildErr != nil {
			return fmt.Errorf("building image with features: %w", buildErr)
		}
		effectiveImage = built
	}

	// Swap the image for container creation
	origImage := devCfg.Image
	devCfg.Image = effectiveImage

	// Resolve security profile early — needed for home dir and pre-create warnings.
	secProfile := security.GetProfile(custom.SecurityProfile)
	secProfile = security.ApplyCustomizations(secProfile, custom)

	fmt.Printf("Creating container %s...\n", containerName)

	// Warn if strict (no-network) profile is used with agents that require internet access.
	if secProfile.Network.Mode == "none" {
		for _, p := range agentProfiles {
			if len(p.NetworkAllow) > 0 {
				m.warn(
					"security profile %q disables all networking, but agent %q requires internet access (%s)",
					custom.SecurityProfile, p.Name,
					strings.Join(p.NetworkAllow, ", "),
				)
			}
		}
	}

	// Start sibling service containers (and their shared network) before the
	// agent so its DNS names and connection-string env vars resolve.
	networkName := ""
	var svcEnv []string
	if servicesEnabled(custom) {
		if !servicesNetworkOK(custom) {
			m.warn(
				"services are enabled but network mode %q has no private network for DNS aliases; services skipped (use a bridge/restricted profile, or reach services via published 127.0.0.1 ports)",
				effectiveNetworkMode(custom),
			)
		} else {
			networkName = serviceNetworkName(containerName)
			if err := m.setupServices(containerName, networkName, custom); err != nil {
				devCfg.Image = origImage
				return fmt.Errorf("setting up services: %w", err)
			}
			svcEnv = serviceEnv(custom)
		}
	}

	if err := m.Docker.CreateAndStart(containerName, devCfg, custom, workspaceFolder, agentProfiles, configHash, networkName, svcEnv); err != nil {
		devCfg.Image = origImage
		return fmt.Errorf("creating container: %w", err)
	}
	devCfg.Image = origImage

	containerHome := m.Docker.ResolveHomeDir(effectiveImage, secProfile.RunAsUser)
	wsInContainer := config.WorkspaceInContainer(devCfg, workspaceFolder)

	// A freshly-created ~/.local volume is owned by root, so the agent's install
	// command (run as the container user) could not write to ~/.local/bin. Make
	// the mount point writable before lifecycle commands run. Harmless to repeat
	// when the volume already carries an install from a previous build.
	m.ensureAgentBinDir(containerName, containerHome)

	// Start the secret-hiding FUSE filter before anything reads the workspace, so
	// the agent (and lifecycle commands) only ever see the filtered view. A
	// failure here is fatal: the user opted into hiding secrets from the agent, so
	// we must not silently fall back to exposing them.
	if err := m.setupSecretFS(containerName, wsInContainer, custom); err != nil {
		return fmt.Errorf("setting up secret hiding (workspaceSecretsPolicy mode=hide): %w", err)
	}

	// Resolve the credential policy once: it gates whether host agent config is
	// copied into the container.
	cred := credpolicy.Decide(custom.CredentialPolicy)

	// Set up each agent: copy config, path mappings
	for _, p := range agentProfiles {
		if cred.AllowHostAgentConfig {
			m.copyAgentConfig(containerName, p, containerHome)
		}
		if p.SetupFunc != nil {
			err := p.SetupFunc(containerName, workspaceFolder, wsInContainer, containerHome, func(cmd []string, user string) error {
				return m.Docker.ExecAs(containerName, cmd, docker.ExecOptions{User: user})
			})
			if err != nil {
				m.warn("agent %s setup failed: %v", p.Name, err)
			}
		}
		// Special case for Claude config patching until we move it to a better place.
		// Under restrictive credential policies, mark the workspace trusted without
		// copying the host's global Claude config into the container.
		if p.Name == "claude" {
			m.setupClaudePathMapping(containerName, workspaceFolder, wsInContainer, containerHome, cred.AllowHostAgentConfig)
			m.setupClaudePermissions(containerName, containerHome, custom.AgentPermissionMode, cred.AllowHostAgentConfig)
		}
	}

	// Install the git wrapper that blocks `git push` under gitPolicy=commitOnly.
	if custom.GitPolicy == types.GitPolicyCommitOnly {
		m.installGitWrapper(containerName)
	}

	// Run lifecycle commands in order.
	// These commands come directly from devcontainer.json and run inside the container.
	// They are logged before execution so the user can see what is being run.
	hasLifecycle := devCfg.OnCreateCommand != nil || devCfg.PostCreateCommand != nil || devCfg.PostStartCommand != nil
	if hasLifecycle {
		fmt.Println("[devc] Running devcontainer lifecycle commands (source: devcontainer.json)")
	}
	if devCfg.OnCreateCommand != nil {
		if lcErr := m.runLifecycleCommand(containerName, devCfg.OnCreateCommand, "onCreateCommand"); lcErr != nil {
			m.warn("onCreateCommand failed: %v", lcErr)
		}
	}
	if devCfg.PostCreateCommand != nil {
		if lcErr := m.runLifecycleCommand(containerName, devCfg.PostCreateCommand, "postCreateCommand"); lcErr != nil {
			m.warn("postCreateCommand failed: %v", lcErr)
		}
	}
	if devCfg.PostStartCommand != nil {
		if lcErr := m.runLifecycleCommand(containerName, devCfg.PostStartCommand, "postStartCommand"); lcErr != nil {
			m.warn("postStartCommand failed: %v", lcErr)
		}
	}

	// Ensure agent binaries are on PATH
	for _, p := range agentProfiles {
		m.linkAgentBinary(containerName, p, containerHome)
	}

	// Make the skills mount discoverable by each agent. The mount lands at a
	// generic target (default /skills); agents scan their own real dir (Claude:
	// ~/.claude/skills), so materialize that dir and link each skill into it.
	m.linkSkillsForAgents(containerName, containerHome, custom, agentProfiles)

	// Apply egress firewall last, after installs/lifecycle have run, so setup
	// traffic isn't blocked but the agent's own traffic is restricted. Use the
	// same effective-mode resolution as the docker layer that grants the caps.
	if custom.Network != nil && custom.Network.Enforce && servicesNetworkOK(custom) {
		m.applyEgressFirewall(containerName, agentProfiles, custom)
	}

	return nil
}

// applyEgressFirewall installs the allowlist-based OUTPUT firewall as root.
func (m *Manager) applyEgressFirewall(containerName string, agentProfiles []*agent.Profile, custom *types.DevcCustomization) {
	var profileDomains []string
	for _, p := range agentProfiles {
		profileDomains = append(profileDomains, p.NetworkAllow...)
	}
	var allowlist []string
	if custom.Network != nil {
		allowlist = custom.Network.Allowlist
	}
	script := buildFirewallScript(egressDomains(profileDomains, allowlist))

	fmt.Println("[devc] Applying egress firewall (network.enforce)")
	if err := m.Docker.ExecAs(containerName, []string{"sh", "-c", script}, docker.ExecOptions{User: "root"}); err != nil {
		m.warn("could not apply egress firewall: %v", err)
	}
}

// Exec runs a command in the container for the given workspace.
func (m *Manager) Exec(workspaceFolder string, command []string) error {
	containerName := config.ContainerName(workspaceFolder)

	// Enforce the workspace secrets policy before running anything: a protected
	// file added after `devc up` must not become reachable via `devc exec` /
	// `devc shell`. Without this, the gate only ran at create time. A config-load
	// failure is fatal here because we cannot verify the policy without it.
	_, merged, err := config.LoadMerged(workspaceFolder)
	if err != nil {
		return fmt.Errorf("loading config to enforce secrets policy: %w", err)
	}
	if err := enforceWorkspaceSecrets(workspaceFolder, merged); err != nil {
		return err
	}

	state := m.Docker.Inspect(containerName).State
	if state != docker.StateRunning {
		return fmt.Errorf("container %s is not running (state: %s); run 'devc up' to start it", containerName, state)
	}

	return m.Docker.ExecAs(containerName, command, docker.ExecOptions{
		Interactive: true,
	})
}

// Attach attaches an interactive session to the container. A stopped container
// is started automatically so `devc shell` / `devc attach` "just work" after a
// `devc stop`; only a missing container is an error.
func (m *Manager) Attach(workspaceFolder, shell string) error {
	containerName := config.ContainerName(workspaceFolder)

	// Load config and enforce the secrets policy before attaching to a running OR
	// stopped container, so a protected file added after `devc up` blocks the
	// shell. A config-load failure is fatal here because we cannot verify the
	// policy without it (the same merged config is reused to restore services).
	_, merged, err := config.LoadMerged(workspaceFolder)
	if err != nil {
		return fmt.Errorf("loading config to enforce secrets policy: %w", err)
	}
	if err := enforceWorkspaceSecrets(workspaceFolder, merged); err != nil {
		return err
	}

	switch state := m.Docker.Inspect(containerName).State; state {
	case docker.StateRunning:
		// Already running — attach directly.
	case docker.StateStopped, docker.StateCreated:
		// Best-effort: bring sibling services (and their network) back the same
		// way `up` does. Service restoration stays soft (it only warns); the
		// secrets gate above is the hard guarantee.
		m.ensureServicesForExisting(containerName, merged)
		fmt.Printf("Starting existing container %s...\n", containerName)
		if err := m.Docker.Start(containerName); err != nil {
			return fmt.Errorf("starting container: %w", err)
		}
		// Start returns once the request is accepted; confirm the container
		// actually stayed up before claiming a session and a shell.
		if st := m.Docker.Inspect(containerName).State; st != docker.StateRunning {
			return fmt.Errorf("container %s exited immediately after start (state: %s); see 'devc logs'", containerName, st)
		}
	case docker.StatePaused:
		fmt.Printf("Unpausing container %s...\n", containerName)
		if err := m.Docker.Unpause(containerName); err != nil {
			return fmt.Errorf("unpausing container: %w", err)
		}
	default:
		return fmt.Errorf("no container found for %s; run 'devc up' to create one", containerName)
	}

	count, _ := m.Session.Attach(containerName)
	fmt.Printf("Attached (%s)\n", session.FormatCount(count))

	err = m.Docker.Exec(containerName, []string{shell}, true)

	remaining, _ := m.Session.Detach(containerName)
	fmt.Printf("Detached (%s)\n", session.FormatCount(remaining))

	return err
}

// StatusInfo is a snapshot of a workspace's container for `devc status`.
type StatusInfo struct {
	Name            string   `json:"name"`
	Workspace       string   `json:"workspace"`
	State           string   `json:"state"`
	Image           string   `json:"image"`
	Agents          []string `json:"agents,omitempty"`
	SecurityProfile string   `json:"securityProfile"`
	NetworkMode     string   `json:"networkMode"`
	AllowlistSize   int      `json:"allowlistSize"`
	CPUs            string   `json:"cpus,omitempty"`
	Memory          string   `json:"memory,omitempty"`
	PidsLimit       int64    `json:"pidsLimit,omitempty"`
	Sessions        int      `json:"sessions"`
	ConfigDrift     bool     `json:"configDrift"`
	Services        []string `json:"services,omitempty"`
}

// Status returns a snapshot of the container and effective config for a
// workspace, including whether the live container has drifted from the config.
// If devcontainer.json is missing/unparseable it still reports a live
// container's basic state, so `devc status` is useful even when config is gone.
func (m *Manager) Status(workspaceFolder string) (*StatusInfo, error) {
	containerName := config.ContainerName(workspaceFolder)
	inspect := m.Docker.Inspect(containerName)

	devCfg, merged, err := config.LoadMerged(workspaceFolder)
	if err != nil {
		if inspect.State == docker.StateNotFound {
			// No container and no config — nothing to report.
			return nil, err
		}
		// Container exists but config is unreadable: report what we can.
		return &StatusInfo{
			Name:      containerName,
			Workspace: workspaceFolder,
			State:     string(inspect.State),
			Sessions:  m.Session.Count(containerName),
		}, nil
	}

	info := &StatusInfo{
		Name:            containerName,
		Workspace:       workspaceFolder,
		State:           string(inspect.State),
		Image:           devCfg.Image,
		Agents:          merged.ResolvedAgents(),
		SecurityProfile: merged.SecurityProfile,
		Sessions:        m.Session.Count(containerName),
		Services:        merged.EnabledServiceNames(),
	}
	if merged.Network != nil {
		info.NetworkMode = merged.Network.Mode
		info.AllowlistSize = len(merged.Network.Allowlist)
	}
	if merged.Resources != nil {
		info.CPUs = merged.Resources.CPUs
		info.Memory = merged.Resources.Memory
		info.PidsLimit = merged.Resources.PidsLimit
	}

	if inspect.State != docker.StateNotFound {
		stored := inspect.Labels["devc.config-hash"]
		info.ConfigDrift = stored != "" && stored != config.ConfigHash(devCfg, merged)
	}

	return info, nil
}

// Logs streams the container's logs to stdout for the workspace.
func (m *Manager) Logs(workspaceFolder string, follow bool) error {
	containerName := config.ContainerName(workspaceFolder)
	if m.Docker.Inspect(containerName).State == docker.StateNotFound {
		return fmt.Errorf("no container found for %s; run 'devc up' to create one", containerName)
	}
	return m.Docker.Logs(containerName, follow, os.Stdout)
}

// Stop stops the container, respecting session count.
func (m *Manager) Stop(workspaceFolder string, force bool) error {
	containerName := config.ContainerName(workspaceFolder)

	state := m.Docker.Inspect(containerName).State
	if state != docker.StateRunning {
		fmt.Printf("Container %s is not running\n", containerName)
		return nil
	}

	if !force {
		count := m.Session.Count(containerName)
		if count > 0 {
			return fmt.Errorf("container %s has %s; use --force to stop anyway", containerName, session.FormatCount(count))
		}
	}

	fmt.Printf("Stopping container %s...\n", containerName)
	if err := m.Docker.Stop(containerName); err != nil {
		return err
	}

	fmt.Printf("Container %s stopped\n", containerName)
	return nil
}

// Down stops and removes the container.
func (m *Manager) Down(workspaceFolder string, force bool) error {
	containerName := config.ContainerName(workspaceFolder)

	state := m.Docker.Inspect(containerName).State
	if state == docker.StateNotFound {
		fmt.Printf("No container found for %s\n", workspaceFolder)
		return nil
	}

	if state == docker.StateRunning {
		if !force {
			count := m.Session.Count(containerName)
			if count > 0 {
				return fmt.Errorf("container %s has %s; use --force to remove", containerName, session.FormatCount(count))
			}
		}
	}

	fmt.Printf("Removing container %s...\n", containerName)
	if err := m.Docker.Remove(containerName, true); err != nil {
		return err
	}

	// Drop the persisted agent-install volume. Unlike rebuild (which keeps it so
	// the agent need not reinstall), `down` is a full teardown.
	if err := m.Docker.RemoveVolume(docker.AgentVolumeName(containerName)); err != nil {
		m.warn("could not remove agent volume for %s: %v", containerName, err)
	}

	// Remove sibling services and the shared network.
	m.cleanupServices(containerName)

	m.Session.Clean(containerName)
	fmt.Printf("Container %s removed\n", containerName)
	return nil
}

// List returns all managed containers.
func (m *Manager) List() ([]types.ContainerInfo, error) {
	containers, err := m.Docker.ListManaged()
	if err != nil {
		return nil, err
	}

	for i := range containers {
		containers[i].Sessions = m.Session.Count(containers[i].Name)
	}

	return containers, nil
}

// Clean removes all stopped managed containers.
func (m *Manager) Clean(dryRun bool) ([]string, error) {
	containers, err := m.Docker.ListManaged()
	if err != nil {
		return nil, err
	}

	var removed []string
	for _, c := range containers {
		if c.State == "stopped" {
			if dryRun {
				removed = append(removed, c.Name)
				continue
			}
			if err := m.Docker.Remove(c.Name, false); err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "warning: failed to remove %s: %v\n", c.Name, err)
				continue
			}
			if err := m.Docker.RemoveVolume(docker.AgentVolumeName(c.Name)); err != nil {
				m.warn("could not remove agent volume for %s: %v", c.Name, err)
			}
			m.cleanupServices(c.Name)
			m.Session.Clean(c.Name)
			removed = append(removed, c.Name)
		}
	}

	return removed, nil
}

// ensureAgentBinDir makes the persisted ~/.local volume writable by the
// container user (1000:1000). A named volume is created root-owned, so without
// this the agent install command — which runs as the container user — cannot
// write into ~/.local/bin. Runs as root and only touches the two top-level
// dirs, so it is cheap and safe to repeat across rebuilds.
func (m *Manager) ensureAgentBinDir(containerName, containerHome string) {
	cmd := fmt.Sprintf(
		`mkdir -p %s/.local/bin && chown 1000:1000 %s/.local %s/.local/bin`,
		containerHome, containerHome, containerHome,
	)
	if err := m.Docker.ExecAs(containerName, []string{"sh", "-c", cmd}, docker.ExecOptions{User: "root"}); err != nil {
		m.warn("could not prepare agent install dir %s/.local/bin: %v", containerHome, err)
	}
}

// linkAgentBinary ensures the agent's binary is on the system PATH by symlinking
// from the user-local install location into /usr/local/bin.
// containerHome must be the container user's home directory (e.g. /home/vscode),
// not root's home. The symlink creation runs as root, so ~ must not be used here
// as it would resolve to /root instead of the container user's home.
func (m *Manager) linkAgentBinary(containerName string, profile *agent.Profile, containerHome string) {
	// Check common user-local install locations and symlink to /usr/local/bin.
	// Use explicit containerHome rather than ~ because this runs as root.
	cmd := fmt.Sprintf(
		`for dir in %s/.local/bin %s/bin %s/.claude/bin; do `+
			`if [ -x "$dir/%s" ]; then ln -sf "$dir/%s" /usr/local/bin/%s 2>/dev/null && break; fi; `+
			`done; true`,
		containerHome, containerHome, containerHome,
		profile.Binary, profile.Binary, profile.Binary,
	)
	if err := m.Docker.ExecAs(containerName, []string{"sh", "-c", cmd}, docker.ExecOptions{User: "root"}); err != nil {
		m.warn("could not link %s to PATH: %v", profile.Binary, err)
	}
}

// gitWrapperScript blocks `git push` while delegating every other invocation to
// the real git binary. It is installed at /usr/local/bin/git, which precedes
// /usr/bin on PATH.
//
// The wrapper resolves the real subcommand by skipping leading global options
// (and their arguments, e.g. `git -C dir push` / `git -c k=v push`) so the push
// block cannot be bypassed with a leading flag. The installer is idempotent: the
// real binary is preserved once at /usr/local/bin/git.real and reused on re-run.
//
// Some base images (e.g. agent-dev-base) ship the real git at /usr/local/bin/git
// — exactly where the wrapper installs itself. The installer tells the real
// binary apart from a previously-installed wrapper via the marker comment below,
// so it correctly backs up the real git instead of refusing to install.
const gitWrapperScript = `set -e
mkdir -p /usr/local/bin
if [ -x /usr/local/bin/git.real ]; then
  REAL_GIT=/usr/local/bin/git.real
else
  REAL_GIT="$(command -v git || true)"
  if [ -z "$REAL_GIT" ]; then
    echo "devc: git not found, skipping gitPolicy wrapper" >&2
    exit 0
  fi
  if [ "$REAL_GIT" = "/usr/local/bin/git" ] && grep -q "devc-git-wrapper" /usr/local/bin/git 2>/dev/null; then
    echo "devc: git wrapper already installed but real git binary missing, skipping gitPolicy wrapper" >&2
    exit 0
  fi
  cp "$REAL_GIT" /usr/local/bin/git.real
  REAL_GIT=/usr/local/bin/git.real
fi
cat > /usr/local/bin/git <<EOF
#!/bin/sh
# devc-git-wrapper: blocks 'git push' under gitPolicy=commitOnly.
# Find the subcommand, skipping leading global options and their arguments.
sub=
skip=0
for arg in "\$@"; do
  if [ "\$skip" = 1 ]; then skip=0; continue; fi
  case "\$arg" in
    -C|-c|--git-dir|--work-tree|--namespace|--super-prefix|--exec-path) skip=1 ;;
    -*) ;;
    *) sub="\$arg"; break ;;
  esac
done
if [ "\$sub" = "push" ]; then
  echo "git push is disabled by devc gitPolicy=commitOnly" >&2
  exit 1
fi
exec $REAL_GIT "\$@"
EOF
chmod 0755 /usr/local/bin/git`

// installGitWrapper installs the commitOnly git wrapper as root.
func (m *Manager) installGitWrapper(containerName string) {
	if err := m.Docker.ExecAs(containerName, []string{"sh", "-c", gitWrapperScript}, docker.ExecOptions{User: "root"}); err != nil {
		m.warn("could not install git wrapper (gitPolicy=commitOnly): %v", err)
	}
}

// linkSkillsForAgents makes the skills mounted at the generic target (default
// /skills) discoverable by each agent. Agents scan a real per-agent directory
// such as ~/.claude/skills; Claude Code does not reliably follow a symlinked
// skills *root*, so the root is materialized as a real directory and each
// individual skill is symlinked into it (~/.claude/skills/<name> -> /skills/<name>).
// Runs as the container user so the directory and links are user-owned. A stale
// symlinked root from older devc is migrated, and a real user-provided skill of
// the same name is left untouched.
func (m *Manager) linkSkillsForAgents(containerName, containerHome string, custom *types.DevcCustomization, profiles []*agent.Profile) {
	if custom.Skills == nil || !custom.Skills.Enabled {
		return
	}
	target := custom.Skills.Target
	if target == "" {
		target = "/skills"
	}
	for _, p := range profiles {
		if p.SkillsDir == "" {
			continue
		}
		dst := containerHome + "/" + p.SkillsDir
		qTarget := shellQuote(target)
		qDst := shellQuote(dst)
		// Replace a stale symlinked root with a real directory, prune symlinks
		// whose source skill disappeared, then (re)link each current skill.
		// Existing real entries (user-provided skills) are left untouched.
		// mkdir/ln failures exit non-zero so ExecAs surfaces them via m.warn.
		script := fmt.Sprintf(
			`if [ -d %[1]s ]; then `+
				`if [ -L %[2]s ]; then rm -f %[2]s; fi; `+
				`mkdir -p %[2]s || exit 1; `+
				`for l in %[2]s/*; do if [ -L "$l" ] && [ ! -e "$l" ]; then rm -f "$l"; fi; done; `+
				`for d in %[1]s/*/; do [ -d "$d" ] || continue; n=$(basename "$d"); `+
				`if [ ! -e %[2]s/"$n" ] || [ -L %[2]s/"$n" ]; then ln -sfn "$d" %[2]s/"$n" || exit 1; fi; `+
				`done; `+
				`fi`,
			qTarget, qDst,
		)
		if err := m.Docker.ExecAs(containerName, []string{"sh", "-c", script}, docker.ExecOptions{}); err != nil {
			m.warn("could not link skills for %s: %v", p.Name, err)
		}
	}
}

// shellQuote single-quotes s for safe interpolation into a /bin/sh command,
// escaping any embedded single quote as '\”. Unlike fmt %q (which produces a
// double-quoted Go literal), this neutralizes $, backticks, and backslashes as
// POSIX sh requires.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// enforceWorkspaceSecrets applies the workspace secrets policy before container
// startup. mode=fail aborts when protected files are present; off/readonly/mask
// are technical controls applied at mount time during container creation, so
// they are no-ops here (readonly/mask shadow the files via bind mounts).
func enforceWorkspaceSecrets(workspaceFolder string, custom *types.DevcCustomization) error {
	sp := custom.WorkspaceSecretsPolicy
	if !secrets.IsEnabled(sp) {
		return nil
	}

	mode := sp.Mode
	if mode == "" {
		mode = types.SecretsModeFail // conservative default when enabled
	}

	switch mode {
	case types.SecretsModeOff, types.SecretsModeReadonly, types.SecretsModeMask, types.SecretsModeHide:
		// readonly/mask are technical controls applied at mount time, and hide is
		// enforced live by the FUSE filter; nothing to gate before startup. (hide,
		// unlike fail, never blocks startup — that is its whole point.)
		return nil
	case types.SecretsModeFail:
		findings, err := secrets.Scan(workspaceFolder, sp.Patterns, sp.AllowPatterns)
		if err != nil {
			return fmt.Errorf("scanning workspace for protected files: %w", err)
		}
		if len(findings) > 0 {
			return fmt.Errorf("%s", secrets.FormatFailure(custom.Preset, findings))
		}
		return nil
	default:
		return fmt.Errorf("unknown workspaceSecretsPolicy mode %q", mode)
	}
}

// setupSecretFS copies the devc-secretfs helper into the container and starts it
// as a FUSE filter over the workspace, hiding files that match the secret
// patterns from the agent dynamically (any path, any time). The real workspace
// is bind-mounted at secrets.FSBackingPath (by the docker layer) and the
// filtered view is mounted at wsTarget. No-op unless mode=hide.
func (m *Manager) setupSecretFS(containerName, wsTarget string, custom *types.DevcCustomization) error {
	sp := custom.WorkspaceSecretsPolicy
	if sp == nil || !sp.Enabled || sp.Mode != types.SecretsModeHide {
		return nil
	}

	patterns := sp.Patterns
	if len(patterns) == 0 {
		patterns = secrets.DefaultPatterns()
	}
	allow := sp.AllowPatterns
	if len(allow) == 0 {
		allow = secrets.DefaultAllowPatterns()
	}

	// Pick the helper build matching the container architecture.
	archOut, err := m.Docker.ExecCapture(containerName, []string{"uname", "-m"}, "root")
	if err != nil {
		return fmt.Errorf("detecting container architecture: %w", err)
	}
	goarch := unameToGoArch(strings.TrimSpace(archOut))
	bin, err := secretfsbin.Binary(goarch)
	if err != nil {
		return err
	}

	tmpDir, err := os.MkdirTemp("", "devc-secretfs-")
	if err != nil {
		return fmt.Errorf("creating temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)
	tmpBin := filepath.Join(tmpDir, "devc-secretfs")
	if err := os.WriteFile(tmpBin, bin, 0o755); err != nil {
		return fmt.Errorf("writing helper: %w", err)
	}
	if err := m.Docker.CopyInto(containerName, tmpBin, "/usr/local/bin"); err != nil {
		return fmt.Errorf("copying helper into container: %w", err)
	}

	// chmod 0700 the backing's parent so the non-root agent cannot traverse into
	// the UNFILTERED backing mount and read secrets directly, bypassing the FUSE
	// view. The FUSE daemon runs as root, so it still reaches the backing.
	backingParent := filepath.Dir(secrets.FSBackingPath)
	prep := fmt.Sprintf(
		`chmod 0755 /usr/local/bin/devc-secretfs && mkdir -p %s %s && chmod 0700 %s`,
		shQuote(wsTarget), shQuote(secrets.FSBackingPath), shQuote(backingParent),
	)
	if err := m.Docker.ExecAs(containerName, []string{"sh", "-c", prep}, docker.ExecOptions{User: "root"}); err != nil {
		return fmt.Errorf("preparing mountpoint: %w", err)
	}

	// Start detached (new session) so it survives the exec connection closing.
	// Every interpolated value is single-quoted: patterns come from
	// devcontainer.json and run in a root shell, so an unquoted $/backtick would
	// be a command-injection vector.
	start := fmt.Sprintf(
		`setsid /usr/local/bin/devc-secretfs --backing %s --mount %s --deny %s --allow %s --uid 1000 --gid 1000 `+
			`</dev/null >/var/log/devc-secretfs.log 2>&1 &`,
		shQuote(secrets.FSBackingPath), shQuote(wsTarget),
		shQuote(strings.Join(patterns, ",")), shQuote(strings.Join(allow, ",")),
	)
	if err := m.Docker.ExecAs(containerName, []string{"sh", "-c", start}, docker.ExecOptions{User: "root"}); err != nil {
		return fmt.Errorf("starting helper: %w", err)
	}

	// Wait for the mount to appear. Match on the fixed FUSE device name rather
	// than interpolating wsTarget into a regex (which would break on paths with
	// spaces or glob/regex metacharacters).
	check := `for i in $(seq 1 50); do if grep -q "^devc-secretfs " /proc/mounts; then exit 0; fi; sleep 0.1; done; exit 1`
	if err := m.Docker.ExecAs(containerName, []string{"sh", "-c", check}, docker.ExecOptions{User: "root"}); err != nil {
		logOut, _ := m.Docker.ExecCapture(containerName, []string{"cat", "/var/log/devc-secretfs.log"}, "root")
		return fmt.Errorf("FUSE mount did not come up at %s: %w\n%s", wsTarget, err, strings.TrimSpace(logOut))
	}

	// The FUSE passthrough reports the backing file owner, which can differ from
	// the container user and trip git's "dubious ownership" guard. Mark the
	// workspace safe for git (as the container user) so git keeps working.
	safe := fmt.Sprintf(`git config --global --add safe.directory %s 2>/dev/null; true`, shQuote(wsTarget))
	_ = m.Docker.ExecAs(containerName, []string{"sh", "-c", safe}, docker.ExecOptions{})
	return nil
}

// shQuote wraps s in single quotes for safe interpolation into an `sh -c`
// command, escaping any embedded single quotes.
func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// unameToGoArch maps `uname -m` output to a Go GOARCH value.
func unameToGoArch(uname string) string {
	switch uname {
	case "aarch64", "arm64":
		return "arm64"
	case "x86_64", "amd64":
		return "amd64"
	default:
		return uname
	}
}

// copyAgentConfig copies host agent configuration into the container.
// Files are copied (not mounted) so the container has its own writable copy
// with no link back to the host filesystem.
func (m *Manager) copyAgentConfig(containerName string, profile *agent.Profile, containerHome string) {
	home, _ := os.UserHomeDir()
	if home == "" {
		return
	}

	for _, mt := range profile.ConfigMounts {
		if !mt.Copy {
			continue
		}

		src := home + "/" + mt.HostPath
		if _, err := os.Stat(src); err != nil {
			continue // host path doesn't exist, skip
		}

		// Docker CopyToContainer extracts a tar into the destination directory.
		// For "~/.claude/settings.json" the tar contains "settings.json",
		// so the destination must be the parent: containerHome + "/.claude"
		dst := mt.ContainerPath
		if dst == "" {
			dst = containerHome + "/" + filepath.Dir(mt.HostPath)
		} else {
			dst = filepath.Dir(dst)
		}

		// Ensure destination directory exists with correct ownership
		mkdirCmd := fmt.Sprintf(`mkdir -p %s && chown -R 1000:1000 %s`, dst, dst)
		_ = m.Docker.ExecAs(containerName, []string{"sh", "-c", mkdirCmd}, docker.ExecOptions{User: "root"})

		if err := m.Docker.CopyInto(containerName, src, dst); err != nil {
			m.warn("could not copy %s into container: %v", mt.HostPath, err)
			continue
		}

		// Fix ownership so the container user can read/write
		target := containerHome + "/" + mt.HostPath
		if mt.ContainerPath != "" {
			target = mt.ContainerPath
		}
		chownCmd := fmt.Sprintf(`chown -R 1000:1000 %s 2>/dev/null; true`, target)
		_ = m.Docker.ExecAs(containerName, []string{"sh", "-c", chownCmd}, docker.ExecOptions{User: "root"})
	}
}

func (m *Manager) runLifecycleCommand(containerName string, cmd any, name string) error {
	// Run lifecycle commands as the container user (not root), matching
	// devcontainer spec behavior. This ensures installs land in the
	// correct user home directory.
	opts := docker.ExecOptions{}

	switch v := cmd.(type) {
	case string:
		fmt.Printf("[devc]   %s: $ %s\n", name, v)
		return m.Docker.ExecAs(containerName, []string{"sh", "-c", v}, opts)
	case []any:
		args := make([]string, len(v))
		for i, a := range v {
			args[i] = fmt.Sprintf("%v", a)
		}
		fmt.Printf("[devc]   %s: %v\n", name, args)
		return m.Docker.ExecAs(containerName, args, opts)
	default:
		return fmt.Errorf("unsupported command format for %s", name)
	}
}

func (m *Manager) setupClaudePathMapping(containerName, hostWorkspace, containerWorkspace, containerHome string, includeHostConfig bool) {
	containerKey := claudeProjectKey(containerWorkspace)

	// Pre-create the session history directory so Claude can store transcripts.
	// Validate the key before interpolating it into a shell command: claudeProjectKey
	// replaces path separators with dashes but does not otherwise sanitize the input,
	// so workspace paths with spaces or shell metacharacters could allow injection.
	if !safeShellPathRe.MatchString(containerKey) {
		m.warn("skipping Claude session directory setup: workspace path %q produces unsafe key %q", containerWorkspace, containerKey)
	} else {
		cmd := fmt.Sprintf(
			`home=$(eval echo ~) && `+
				`mkdir -p "$home/.claude/projects/%s"`,
			containerKey,
		)
		if err := m.Docker.ExecAs(containerName, []string{"sh", "-c", cmd}, docker.ExecOptions{}); err != nil {
			m.warn("could not set up Claude session directory: %v", err)
		}
	}

	// Patch ~/.claude.json in the container to mark the workspace as trusted so
	// Claude doesn't prompt for authorization on every run. When the credential
	// policy withholds host config, start from an empty base instead of copying
	// the host's global Claude config (which may carry account/history data).
	hostConfigPath := ""
	if includeHostConfig {
		hostHome, _ := os.UserHomeDir()
		hostConfigPath = filepath.Join(hostHome, ".claude.json")
	}
	modified, err := patchClaudeGlobalConfig(hostConfigPath, containerWorkspace)
	if err != nil {
		m.warn("could not generate Claude global config: %v", err)
		return
	}

	// Write to a temp file named .claude.json, then docker cp it into the container.
	tmpDir, err := os.MkdirTemp("", "devc-claude-")
	if err != nil {
		m.warn("could not create temp dir for Claude config: %v", err)
		return
	}
	defer os.RemoveAll(tmpDir)

	tmpFile := filepath.Join(tmpDir, ".claude.json")
	if err := os.WriteFile(tmpFile, modified, 0o644); err != nil {
		m.warn("could not write temp Claude config: %v", err)
		return
	}
	if err := m.Docker.CopyInto(containerName, tmpFile, containerHome); err != nil {
		m.warn("could not copy Claude config into container: %v", err)
		return
	}

	chown := fmt.Sprintf("chown 1000:1000 %s/.claude.json 2>/dev/null; true", containerHome)
	_ = m.Docker.ExecAs(containerName, []string{"sh", "-c", chown}, docker.ExecOptions{User: "root"})
}

// patchClaudeGlobalConfig reads the host ~/.claude.json (or starts with an empty config),
// ensures the container workspace path is present under "projects" with hasTrustDialogAccepted
// set to true, and returns the modified JSON. All other fields are preserved.
func patchClaudeGlobalConfig(hostConfigPath, containerWorkspace string) ([]byte, error) {
	var cfg map[string]any

	if data, err := os.ReadFile(hostConfigPath); err == nil {
		if jsonErr := json.Unmarshal(data, &cfg); jsonErr != nil {
			cfg = make(map[string]any)
		}
	} else {
		cfg = make(map[string]any)
	}

	projects, _ := cfg["projects"].(map[string]any)
	if projects == nil {
		projects = make(map[string]any)
	}

	if _, exists := projects[containerWorkspace]; !exists {
		projects[containerWorkspace] = map[string]any{
			"allowedTools":           []any{},
			"mcpContextUris":         []any{},
			"mcpServers":             map[string]any{},
			"hasTrustDialogAccepted": true,
		}
	}

	cfg["projects"] = projects
	return json.MarshalIndent(cfg, "", "  ")
}

// setupClaudePermissions writes ~/.claude/settings.json inside the container so
// the agent runs with the configured default permission mode (e.g.
// bypassPermissions in a sandbox, so it edits files without confirmation). The
// host settings.json is merged in when the credential policy allows it, so other
// keys the user already set are preserved.
func (m *Manager) setupClaudePermissions(containerName, containerHome, mode string, includeHostConfig bool) {
	if mode == "" {
		return
	}

	hostSettingsPath := ""
	if includeHostConfig {
		if hostHome, err := os.UserHomeDir(); err == nil {
			hostSettingsPath = filepath.Join(hostHome, ".claude", "settings.json")
		}
	}
	data, err := patchClaudeSettings(hostSettingsPath, mode)
	if err != nil {
		m.warn("could not generate Claude settings: %v", err)
		return
	}

	tmpDir, err := os.MkdirTemp("", "devc-claude-settings-")
	if err != nil {
		m.warn("could not create temp dir for Claude settings: %v", err)
		return
	}
	defer os.RemoveAll(tmpDir)

	tmpFile := filepath.Join(tmpDir, "settings.json")
	if err := os.WriteFile(tmpFile, data, 0o644); err != nil {
		m.warn("could not write temp Claude settings: %v", err)
		return
	}

	dst := containerHome + "/.claude"
	mkdir := fmt.Sprintf(`mkdir -p %s && chown -R 1000:1000 %s`, dst, dst)
	_ = m.Docker.ExecAs(containerName, []string{"sh", "-c", mkdir}, docker.ExecOptions{User: "root"})
	if err := m.Docker.CopyInto(containerName, tmpFile, dst); err != nil {
		m.warn("could not copy Claude settings into container: %v", err)
		return
	}
	chown := fmt.Sprintf("chown 1000:1000 %s/settings.json 2>/dev/null; true", dst)
	_ = m.Docker.ExecAs(containerName, []string{"sh", "-c", chown}, docker.ExecOptions{User: "root"})
}

// patchClaudeSettings reads the host ~/.claude/settings.json (or starts empty),
// sets permissions.defaultMode to mode, and returns the merged JSON. All other
// fields are preserved.
func patchClaudeSettings(hostSettingsPath, mode string) ([]byte, error) {
	var cfg map[string]any
	if hostSettingsPath != "" {
		if data, err := os.ReadFile(hostSettingsPath); err == nil {
			if jsonErr := json.Unmarshal(data, &cfg); jsonErr != nil {
				cfg = nil
			}
		}
	}
	if cfg == nil {
		cfg = make(map[string]any)
	}

	perms, _ := cfg["permissions"].(map[string]any)
	if perms == nil {
		perms = make(map[string]any)
	}
	perms["defaultMode"] = mode
	cfg["permissions"] = perms

	return json.MarshalIndent(cfg, "", "  ")
}

// claudeProjectKey converts an absolute path to the key Claude uses for
// ~/.claude/projects/ directory names: replace path separators with dashes.
func claudeProjectKey(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	return strings.ReplaceAll(abs, string(filepath.Separator), "-")
}

// describeChanges compares stored container labels with current config to produce
// human-readable descriptions of what changed.
func describeChanges(
	labels map[string]string,
	devCfg *types.DevContainerConfig,
	custom *types.DevcCustomization,
	agentProfiles []*agent.Profile,
) []string {
	var changes []string

	currentAgents := strings.Join(custom.ResolvedAgents(), ",")
	if storedAgent := labels["devc.agent"]; storedAgent != "" {
		if currentAgents != "" && currentAgents != storedAgent {
			changes = append(changes, fmt.Sprintf("agents changed: %s → %s", storedAgent, currentAgents))
		}
	} else if currentAgents != "" {
		changes = append(changes, fmt.Sprintf("agents added: %s", currentAgents))
	}

	for _, p := range agentProfiles {
		changes = append(changes, fmt.Sprintf("agent %s profile may have updated install commands", p.Name))
	}

	// Generic fallback if we can't determine specifics
	if len(changes) == 0 {
		changes = append(changes, "devcontainer.json or devc configuration has changed")
	}

	return changes
}

// askYesNo reads a yes/no answer from stdin. Defaults to no.
func askYesNo() bool {
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil {
		return false
	}
	line = strings.TrimSpace(strings.ToLower(line))
	return line == "y" || line == "yes"
}
