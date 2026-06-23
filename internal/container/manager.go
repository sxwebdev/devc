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
	"github.com/sxwebdev/devc/internal/secrets"
	"github.com/sxwebdev/devc/internal/security"
	"github.com/sxwebdev/devc/internal/session"
	"github.com/sxwebdev/devc/pkg/types"
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
}

// Up creates or starts a container for the workspace.
func (m *Manager) Up(opts UpOptions) error {
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
			_, _ = fmt.Fprintf(os.Stderr, "warning: unknown agent %q, skipping\n", name)
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
	if !opts.Rebuild && (state == docker.StateRunning || state == docker.StateStopped || state == docker.StateCreated) {
		storedHash := inspectResult.Labels["devc.config-hash"]
		if storedHash != "" && storedHash != currentHash {
			changes := describeChanges(inspectResult.Labels, devCfg, merged, agentProfiles)
			fmt.Printf("Configuration has changed since this container was created:\n")
			for _, change := range changes {
				fmt.Printf("  - %s\n", change)
			}
			fmt.Printf("\nRebuild container? [y/N] ")
			if askYesNo() {
				opts.Rebuild = true
			} else {
				fmt.Println("Continuing with existing container")
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

	case docker.StateNotFound:
		if err := m.createContainer(containerName, devCfg, merged, opts.WorkspaceFolder, agentProfiles, currentHash); err != nil {
			return err
		}
	}

	// Track session
	count, _ := m.Session.Attach(containerName)
	fmt.Printf("Container %s ready (%s)\n", containerName, session.FormatCount(count))

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
				_, _ = fmt.Fprintf(os.Stderr,
					"warning: security profile %q disables all networking, but agent %q requires internet access (%s)\n",
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
			_, _ = fmt.Fprintf(os.Stderr,
				"warning: services are enabled but network mode %q has no private network for DNS aliases; services skipped (use a bridge/restricted profile, or reach services via published 127.0.0.1 ports)\n",
				effectiveNetworkMode(custom))
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
				_, _ = fmt.Fprintf(os.Stderr, "warning: agent %s setup failed: %v\n", p.Name, err)
			}
		}
		// Special case for Claude config patching until we move it to a better place.
		// Under restrictive credential policies, mark the workspace trusted without
		// copying the host's global Claude config into the container.
		if p.Name == "claude" {
			m.setupClaudePathMapping(containerName, workspaceFolder, wsInContainer, containerHome, cred.AllowHostAgentConfig)
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
			_, _ = fmt.Fprintf(os.Stderr, "warning: onCreateCommand failed: %v\n", lcErr)
		}
	}
	if devCfg.PostCreateCommand != nil {
		if lcErr := m.runLifecycleCommand(containerName, devCfg.PostCreateCommand, "postCreateCommand"); lcErr != nil {
			_, _ = fmt.Fprintf(os.Stderr, "warning: postCreateCommand failed: %v\n", lcErr)
		}
	}
	if devCfg.PostStartCommand != nil {
		if lcErr := m.runLifecycleCommand(containerName, devCfg.PostStartCommand, "postStartCommand"); lcErr != nil {
			_, _ = fmt.Fprintf(os.Stderr, "warning: postStartCommand failed: %v\n", lcErr)
		}
	}

	// Ensure agent binaries are on PATH
	for _, p := range agentProfiles {
		m.linkAgentBinary(containerName, p, containerHome)
	}

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
		_, _ = fmt.Fprintf(os.Stderr, "warning: could not apply egress firewall: %v\n", err)
	}
}

// Exec runs a command in the container for the given workspace.
func (m *Manager) Exec(workspaceFolder string, command []string) error {
	containerName := config.ContainerName(workspaceFolder)

	state := m.Docker.Inspect(containerName).State
	if state != docker.StateRunning {
		return fmt.Errorf("container %s is not running (state: %s)", containerName, state)
	}

	return m.Docker.ExecAs(containerName, command, docker.ExecOptions{
		Interactive: true,
	})
}

// Attach attaches an interactive session to the container.
func (m *Manager) Attach(workspaceFolder, shell string) error {
	containerName := config.ContainerName(workspaceFolder)

	state := m.Docker.Inspect(containerName).State
	if state != docker.StateRunning {
		return fmt.Errorf("container %s is not running", containerName)
	}

	count, _ := m.Session.Attach(containerName)
	fmt.Printf("Attached (%s)\n", session.FormatCount(count))

	err := m.Docker.Exec(containerName, []string{shell}, true)

	remaining, _ := m.Session.Detach(containerName)
	fmt.Printf("Detached (%s)\n", session.FormatCount(remaining))

	return err
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

	// Remove sibling services and the shared network. Named volumes are kept.
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
			m.cleanupServices(c.Name)
			m.Session.Clean(c.Name)
			removed = append(removed, c.Name)
		}
	}

	return removed, nil
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
		_, _ = fmt.Fprintf(os.Stderr, "warning: could not link %s to PATH: %v\n", profile.Binary, err)
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
  if [ "$REAL_GIT" = "/usr/local/bin/git" ]; then
    echo "devc: cannot locate real git binary, skipping gitPolicy wrapper" >&2
    exit 0
  fi
  cp "$REAL_GIT" /usr/local/bin/git.real
  REAL_GIT=/usr/local/bin/git.real
fi
cat > /usr/local/bin/git <<EOF
#!/bin/sh
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
		_, _ = fmt.Fprintf(os.Stderr, "warning: could not install git wrapper (gitPolicy=commitOnly): %v\n", err)
	}
}

// enforceWorkspaceSecrets applies the workspace secrets policy before container
// startup. mode=fail aborts when protected files are present; mode=mask is not
// yet implemented; off/readonly are handled elsewhere (readonly at mount time).
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
	case types.SecretsModeOff, types.SecretsModeReadonly, types.SecretsModeMask:
		// readonly and mask are technical controls applied at mount time during
		// container creation; nothing to enforce before startup.
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
			_, _ = fmt.Fprintf(os.Stderr, "warning: could not copy %s into container: %v\n", mt.HostPath, err)
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
		_, _ = fmt.Fprintf(os.Stderr, "warning: skipping Claude session directory setup: workspace path %q produces unsafe key %q\n", containerWorkspace, containerKey)
	} else {
		cmd := fmt.Sprintf(
			`home=$(eval echo ~) && `+
				`mkdir -p "$home/.claude/projects/%s"`,
			containerKey,
		)
		if err := m.Docker.ExecAs(containerName, []string{"sh", "-c", cmd}, docker.ExecOptions{}); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "warning: could not set up Claude session directory: %v\n", err)
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
		_, _ = fmt.Fprintf(os.Stderr, "warning: could not generate Claude global config: %v\n", err)
		return
	}

	// Write to a temp file named .claude.json, then docker cp it into the container.
	tmpDir, err := os.MkdirTemp("", "devc-claude-")
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "warning: could not create temp dir for Claude config: %v\n", err)
		return
	}
	defer os.RemoveAll(tmpDir)

	tmpFile := filepath.Join(tmpDir, ".claude.json")
	if err := os.WriteFile(tmpFile, modified, 0o644); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "warning: could not write temp Claude config: %v\n", err)
		return
	}
	if err := m.Docker.CopyInto(containerName, tmpFile, containerHome); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "warning: could not copy Claude config into container: %v\n", err)
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
