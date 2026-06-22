# Secure Local Agent Workflow

`devc` can run an AI coding agent (Claude, Codex, Copilot, …) inside a dev
container where the agent works almost like it would on the host — editing code,
running tests and tooling, and making `git commit`s without constant
confirmation prompts — while host credentials and in-repo secrets stay out of
reach and `git push` is disabled.

This workflow is **additive and opt-in**. The default behavior of `devc` is
unchanged; you enable the secure workflow explicitly with a preset or individual
settings.

> **Warning:** Do not run bypass / no-confirmation AI agents directly on the
> host. Use this secure local container workflow instead.

## Quick start

```bash
devc init --preset secure-local-agent --agent claude
devc up
```

`devc init --list-presets` lists available presets.

This produces a `customizations.devc` block equivalent to:

```json
{
  "customizations": {
    "devc": {
      "preset": "secure-local-agent",
      "agent": "claude",
      "securityProfile": "moderate",
      "credentialPolicy": "agentOnly",
      "gitPolicy": "commitOnly",
      "workspaceSecretsPolicy": {
        "enabled": true,
        "mode": "fail",
        "patterns": [
          ".env",
          ".env.*",
          "*.env",
          "config.yaml",
          "secrets.yaml",
          "credentials.json",
          "service-account*.json",
          ".npmrc"
        ],
        "allowPatterns": [
          ".env.example",
          ".env.sample",
          "*.example.yaml",
          "*.sample.yaml"
        ]
      },
      "skills": {
        "enabled": true,
        "source": "~/.agent/skills",
        "target": "/skills",
        "readonly": true,
        "required": false
      }
    }
  }
}
```

The `preset` field expands these defaults at runtime (`global defaults < preset
< project`), so you can override any individual field explicitly while keeping
the rest of the preset.

## Threat model

### Protected

- Host SSH keys (`~/.ssh`)
- Host git config (`~/.gitconfig`)
- SSH agent (`SSH_AUTH_SOCK` forwarding)
- GitHub / GitLab tokens (env and macOS Keychain)
- Cloud credentials (`AWS_*`, `KUBECONFIG`, `GOOGLE_APPLICATION_CREDENTIALS`, …)
- Host home directory and host `~/.config`
- Host Docker socket (never mounted into the agent container)
- Protected in-repo secret files when `workspaceSecretsPolicy.mode=fail`
  prevents startup
- Ability to `git push` (blocked under `gitPolicy=commitOnly`)

### Not protected

- Files in the mounted workspace that are not blocked — the agent is supposed to
  edit the workspace
- The AI agent's own credentials available inside the container (needed for it
  to run)
- Anything you explicitly inject into the container
- Any service you expose to the container
- In-repo secret files when `workspaceSecretsPolicy` is disabled or set to
  `off`/`readonly`

## `credentialPolicy`

Controls which host credentials reach the container.

| Value              | Behavior                                                                                                                                              |
| ------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------- |
| `none`             | No host credentials mounted, forwarded, read, or injected. The agent must be authenticated some other way (e.g. log in inside the container).         |
| `agentOnly`        | Only the credentials needed to run the AI agent itself (e.g. the Claude OAuth token). No git/forge/cloud/SSH credentials, no host agent config files. |
| `developer`        | `agentOnly` plus opt-in developer conveniences: host git config and SSH agent forwarding.                                                             |
| `legacy` (default) | Existing behavior: host SSH keys, git config, SSH agent, agent tokens, and configured env passthroughs are all available.                             |

An empty/unset `credentialPolicy` is treated as `legacy` for backwards
compatibility.

### Why the secure preset uses `agentOnly`, not `none`

`none` is the strongest setting, but it withholds the agent's own auth token,
which means you'd re-authenticate the agent inside the container on every fresh
build. The `secure-local-agent` preset therefore defaults to `agentOnly`: the
agent's LLM token is forwarded (so it just works), while git, forge, cloud, and
SSH credentials are withheld. Set `"credentialPolicy": "none"` explicitly if you
want maximum isolation and are willing to log in inside the container.

## `workspaceSecretsPolicy`

Some repositories contain local secret files (`.env`, `secrets.yaml`,
service-account JSON, `.npmrc`, …). This policy controls what happens when such
files are present.

| Mode       | Behavior                                                                                                       |
| ---------- | -------------------------------------------------------------------------------------------------------------- |
| `off`      | Do nothing (existing behavior).                                                                                |
| `fail`     | Refuse to start and list the protected files. Recommended.                                                     |
| `readonly` | Mount each protected file read-only (the agent can still read it — less safe).                                 |
| `mask`     | Technically hide protected files from the agent. **Not implemented yet** — selecting it returns a clear error. |

- Matching is shell-glob by file base name.
- `allowPatterns` exempts example/sample files (`.env.example`, etc.).
- The `.git` directory is always ignored.
- Findings are reported as workspace-relative paths.

### What to do if `.env` or `config.yaml` exists in the repo

With `mode=fail`, `devc up` refuses to start and lists the offending files. To
resolve:

- Move the secret outside the repository and reference it from there, or
- Keep only a safe `*.example` / `*.sample` file in the repo, or
- As a last resort, set `mode` to `off` or `readonly` if you understand the
  risk.

## `gitPolicy`

| Value        | Behavior                                                     |
| ------------ | ------------------------------------------------------------ |
| `none`       | Do not modify git behavior.                                  |
| `commitOnly` | All git operations work except `git push`, which is blocked. |
| `full`       | No restriction.                                              |

`commitOnly` is a **technical control**: a wrapper script is installed at
`/usr/local/bin/git` (ahead of the real git on `PATH`) that rejects `push` and
delegates everything else to the real git binary. It is not merely a prompt
instruction.

> **Limitation:** tools that call git via an absolute path (`/usr/bin/git`)
> bypass the wrapper. This is a usability boundary, not a hardened sandbox.

## Skills (`/skills`)

A read-only skills directory can be mounted into the container:

```json
{
  "skills": {
    "enabled": true,
    "source": "~/.agent/skills",
    "target": "/skills",
    "readonly": true,
    "required": false
  }
}
```

- `~` expands to the host home directory.
- Defaults: source `~/.agent/skills`, target `/skills`, read-only.
- When enabled, `AGENT_SKILLS_DIR=/skills` is set inside the container.
- A missing source path is skipped with a warning unless `required: true`, which
  makes it a hard error.

The canonical source is `~/.agent/skills`. If your host symlinks
`~/.claude/skills -> ~/.agent/skills`, the container still mounts the canonical
path.

## Services (Postgres / Redis) and DBeaver

Sibling service containers (Postgres, Redis) reachable from the agent by DNS
name, and from the host on `127.0.0.1` ports, are **planned** and not yet part
of this milestone. The `services` config is parsed but not yet executed. Once
available, the intended model is:

- `devc` (on the host) manages the service containers — the agent container does
  **not** receive the Docker socket.
- The agent connects via container DNS names (`postgres:5432`, `redis:6379`).
- The host connects via published localhost ports (e.g. DBeaver →
  `127.0.0.1:54321`).

## Known limitations

- `workspaceSecretsPolicy.mode=mask` is not implemented yet.
- Service containers are not implemented yet.
- Network egress is not filtered to an allowlist yet; `restricted` is a standard
  bridge network. Treat outbound network access as open.
- The `git push` wrapper can be bypassed by absolute-path git invocations.
- `readonly` secrets and `mask` only cover files present at container creation
  time.
