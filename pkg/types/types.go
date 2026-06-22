package types

// DevContainerConfig represents a parsed devcontainer.json.
type DevContainerConfig struct {
	Name                 string            `json:"name,omitempty"`
	Image                string            `json:"image,omitempty"`
	Build                *BuildConfig      `json:"build,omitempty"`
	DockerComposeFile    any               `json:"dockerComposeFile,omitempty"`
	Service              string            `json:"service,omitempty"`
	RunArgs              []string          `json:"runArgs,omitempty"`
	ContainerEnv         map[string]string `json:"containerEnv,omitempty"`
	RemoteEnv            map[string]string `json:"remoteEnv,omitempty"`
	RemoteUser           string            `json:"remoteUser,omitempty"`
	Mounts               []any             `json:"mounts,omitempty"`
	Features             map[string]any    `json:"features,omitempty"`
	ForwardPorts         []any             `json:"forwardPorts,omitempty"`
	PostCreateCommand    any               `json:"postCreateCommand,omitempty"`
	PostStartCommand     any               `json:"postStartCommand,omitempty"`
	PostAttachCommand    any               `json:"postAttachCommand,omitempty"`
	InitializeCommand    any               `json:"initializeCommand,omitempty"`
	OnCreateCommand      any               `json:"onCreateCommand,omitempty"`
	UpdateContentCommand any               `json:"updateContentCommand,omitempty"`
	Customizations       map[string]any    `json:"customizations,omitempty"`
	OverrideCommand      *bool             `json:"overrideCommand,omitempty"`
	ShutdownAction       string            `json:"shutdownAction,omitempty"`
	WorkspaceFolder      string            `json:"workspaceFolder,omitempty"`
	WorkspaceMount       string            `json:"workspaceMount,omitempty"`
}

// BuildConfig holds Dockerfile build settings.
type BuildConfig struct {
	Dockerfile string            `json:"dockerfile,omitempty"`
	Context    string            `json:"context,omitempty"`
	Args       map[string]string `json:"args,omitempty"`
	Target     string            `json:"target,omitempty"`
	CacheFrom  any               `json:"cacheFrom,omitempty"`
}

// DevcCustomization holds AI safety extensions under customizations.devc.
type DevcCustomization struct {
	Agent           string            `json:"agent,omitempty"`  // Single agent (backward compat)
	Agents          []string          `json:"agents,omitempty"` // Multiple agents
	SecurityProfile string            `json:"securityProfile,omitempty"`
	Network         *NetworkConfig    `json:"network,omitempty"`
	Resources       *ResourceConfig   `json:"resources,omitempty"`
	Filesystem      *FilesystemConfig `json:"filesystem,omitempty"`
	Session         *SessionConfig    `json:"session,omitempty"`
	AgentMounts     map[string]string `json:"agentMounts,omitempty"`
	EnvPassthrough  []string          `json:"envPassthrough,omitempty"` // Host env vars to forward (e.g., API keys)

	// Secure local agent workflow (additive; empty values preserve legacy behavior).
	Preset                 string                    `json:"preset,omitempty"`           // Named bundle of secure defaults (e.g. secure-local-agent)
	CredentialPolicy       string                    `json:"credentialPolicy,omitempty"` // none, agentOnly, developer, legacy (default: legacy)
	WorkspaceSecretsPolicy *WorkspaceSecretsPolicy   `json:"workspaceSecretsPolicy,omitempty"`
	GitPolicy              string                    `json:"gitPolicy,omitempty"` // none, commitOnly, full
	Skills                 *SkillsConfig             `json:"skills,omitempty"`
	Services               map[string]*ServiceConfig `json:"services,omitempty"` // sibling service containers (executed in a later milestone)
}

// Credential policy values. Empty string is treated as CredentialPolicyLegacy.
const (
	CredentialPolicyNone      = "none"      // No host credentials mounted, forwarded, read, or injected.
	CredentialPolicyAgentOnly = "agentOnly" // Only the credentials required to run the AI agent itself.
	CredentialPolicyDeveloper = "developer" // Agent creds plus opt-in developer conveniences (git config, ssh-agent).
	CredentialPolicyLegacy    = "legacy"    // Preserve existing behavior (all host creds/mounts as before).
)

// Git policy values. Empty string is treated as GitPolicyFull (no restriction).
const (
	GitPolicyNone       = "none"       // Do not modify git behavior.
	GitPolicyCommitOnly = "commitOnly" // Block `git push`; allow all other git operations.
	GitPolicyFull       = "full"       // Do not restrict git.
)

// Workspace secrets policy modes.
const (
	SecretsModeOff      = "off"      // Do nothing (existing behavior).
	SecretsModeFail     = "fail"     // Refuse to start if protected files are present in the workspace.
	SecretsModeMask     = "mask"     // Technically hide protected files from the agent (implemented in a later milestone).
	SecretsModeReadonly = "readonly" // Mount protected files read-only (less safe; not used by the secure preset).
)

// WorkspaceSecretsPolicy controls how local secret files inside the workspace
// are handled before the agent gains access to it.
type WorkspaceSecretsPolicy struct {
	Enabled       bool     `json:"enabled,omitempty"`
	Mode          string   `json:"mode,omitempty"` // off, fail, mask, readonly
	Patterns      []string `json:"patterns,omitempty"`
	AllowPatterns []string `json:"allowPatterns,omitempty"`
}

// SkillsConfig configures a read-only skills mount inside the container.
type SkillsConfig struct {
	Enabled  bool   `json:"enabled,omitempty"`
	Source   string `json:"source,omitempty"`   // Host path; ~ is expanded. Default: ~/.agent/skills
	Target   string `json:"target,omitempty"`   // Container path. Default: /skills
	ReadOnly *bool  `json:"readonly,omitempty"` // Default: true
	Required bool   `json:"required,omitempty"` // If true, a missing source path is a hard error.
}

// ServiceConfig describes a sibling service container (e.g. Postgres, Redis).
// Parsed now; executed in a later milestone.
type ServiceConfig struct {
	Enabled       bool              `json:"enabled,omitempty"`
	Image         string            `json:"image,omitempty"`
	ContainerPort int               `json:"containerPort,omitempty"`
	HostPort      int               `json:"hostPort,omitempty"`
	HostIP        string            `json:"hostIP,omitempty"`
	Env           map[string]string `json:"env,omitempty"`
	Volumes       []ServiceVolume   `json:"volumes,omitempty"`
}

// ServiceVolume is a named volume attached to a service container.
type ServiceVolume struct {
	Name   string `json:"name,omitempty"`
	Target string `json:"target,omitempty"`
}

// ResolvedAgents returns the deduplicated list of agent names from both Agent and Agents fields.
func (d *DevcCustomization) ResolvedAgents() []string {
	seen := make(map[string]bool)
	var result []string
	// Agents field first, then Agent for backward compat
	for _, a := range d.Agents {
		if a != "" && !seen[a] {
			seen[a] = true
			result = append(result, a)
		}
	}
	if d.Agent != "" && !seen[d.Agent] {
		result = append(result, d.Agent)
	}
	return result
}

type NetworkConfig struct {
	Mode      string   `json:"mode,omitempty"` // none, restricted, host
	Allowlist []string `json:"allowlist,omitempty"`
	Denylist  []string `json:"denylist,omitempty"`
	// Enforce turns the allowlist into a real egress firewall (default-DROP
	// OUTPUT with only allowlisted domains permitted). Requires iptables in the
	// image and adds NET_ADMIN/NET_RAW capabilities. Experimental, opt-in.
	Enforce bool `json:"enforce,omitempty"`
}

type ResourceConfig struct {
	CPUs      string `json:"cpus,omitempty"`
	Memory    string `json:"memory,omitempty"`
	PidsLimit int64  `json:"pidsLimit,omitempty"`
}

type FilesystemConfig struct {
	ReadOnlyPaths    []string `json:"readOnlyPaths,omitempty"`
	NoExecPaths      []string `json:"noExecPaths,omitempty"`
	ProjectMountMode string   `json:"projectMountMode,omitempty"` // rw, ro, overlay
}

type SessionConfig struct {
	StopOnLastDetach   bool `json:"stopOnLastDetach,omitempty"`
	IdleTimeoutMinutes int  `json:"idleTimeoutMinutes,omitempty"`
}

// GlobalConfig represents ~/.devc/config.json.
type GlobalConfig struct {
	Defaults DevcCustomization      `json:"defaults"`
	Agents   map[string]AgentConfig `json:"agents,omitempty"`
}

type AgentConfig struct {
	ConfigPaths []string `json:"configPaths,omitempty"`
	MountMode   string   `json:"mountMode,omitempty"`
}

// ContainerInfo holds runtime info about a managed container.
type ContainerInfo struct {
	Name            string `json:"name"`
	ContainerID     string `json:"containerId"`
	WorkspaceFolder string `json:"workspaceFolder"`
	State           string `json:"state"`
	Image           string `json:"image"`
	Agent           string `json:"agent,omitempty"`
	Sessions        int    `json:"sessions"`
}

// SecurityProfile defines container security constraints.
type SecurityProfile struct {
	Name           string
	Network        NetworkConfig
	Resources      ResourceConfig
	DropAllCaps    bool
	AddCaps        []string
	SeccompProfile string
	RunAsUser      string
}
