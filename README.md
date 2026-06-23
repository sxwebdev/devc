# devc

AI-safe development containers with [devcontainer.json](https://containers.dev/) support.

`devc` creates isolated, sandboxed Docker containers for AI coding agents (Claude Code, Codex, Gemini CLI, Opencode)
while providing a consistent development experience for both local and remote workflows.

## Features

- **Devcontainer.json compatible** — uses the standard [Dev Container spec](https://containers.dev/) with AI safety
  extensions via `customizations.devc`
- **Security profiles** — three presets (strict, moderate, permissive) controlling network access, capabilities, and
  resource limits
- **AI agent integration** — built-in profiles for Claude Code, Codex, GitHub Copilot CLI, Gemini CLI, Aider,
  Opencode, and Hermes Agent with config mounting and network allowlists
- **Secure local agent preset** — withhold host credentials (`credentialPolicy`), block or mask in-repo secrets
  (`workspaceSecretsPolicy`), disable `git push` (`gitPolicy`), read-only skills mount, opt-in egress firewall
- **Service containers** — sibling Postgres/Redis (and more) on a per-project network: reach them by DNS inside the
  container and on `127.0.0.1` ports from the host, with no Docker socket in the agent
- **Port forwarding** — publish container app ports (`forwardPorts`) to `127.0.0.1` for host access
- **Session tracking** — per-container session counting prevents accidental stops while sessions are active
- **Persistent containers** — containers survive between sessions, resuming where you left off

## Install

### Homebrew (macOS & Linux)

```sh
brew tap sxwebdev/devc https://github.com/sxwebdev/devc
brew trust sxwebdev/devc
brew install devc
```

### Download binary

Pre-built binaries for macOS and Linux (amd64 and arm64) are available on the
[GitHub Releases](https://github.com/sxwebdev/devc/releases) page.

### Go install

```sh
go install github.com/sxwebdev/devc@latest
```

### Build from source

```sh
git clone https://github.com/sxwebdev/devc.git
cd devc
make build
# binary at ./bin/devc
```

### Prerequisites

- A container runtime (see [Supported runtimes](#supported-container-runtimes) below)
- Go 1.26+ (for building from source)

## Quick start

```sh
# Initialize a project with AI safety defaults for Claude
devc init --agent claude

# Start the container and attach a shell
devc up

# Run a command inside the container
devc exec -- npm test

# Attach another session
devc attach

# Stop the container
devc stop
```

## Commands

| Command              | Description                                                                                       |
| -------------------- | ------------------------------------------------------------------------------------------------- |
| `devc up [path]`     | Create and start a development container                                                          |
| `devc attach [path]` | Open a shell in the container (alias: `devc shell`; starts it if stopped)                         |
| `devc exec -- <cmd>` | Execute a command in a running container                                                          |
| `devc stop [path]`   | Stop a container (respects active sessions)                                                       |
| `devc down [path]`   | Stop and remove a container                                                                       |
| `devc up --rebuild`  | Recreate the container (e.g. after config changes)                                                |
| `devc list`          | List all managed containers                                                                       |
| `devc status [path]` | Show container state and effective config                                                         |
| `devc logs [path]`   | Show container logs (`-f` to follow)                                                              |
| `devc config [path]` | Show effective config (subcommands: `set`, `validate`, `global`, `add-feature`, `remove-feature`) |
| `devc clean`         | Remove all stopped containers                                                                     |
| `devc init [path]`   | Generate a devcontainer.json with AI safety defaults                                              |

For config changes, `devc up` detects drift and prompts to rebuild; pass `--yes`
or `--no` to answer non-interactively (e.g. in CI).

### Output format

Commands that print machine-readable data — `list`, `status`, and `config show` —
accept `--output-format json` (default is human-readable `text`):

```sh
devc list --output-format json
devc status --output-format json
devc config show --output-format json
```

## Configuration

### Project: `.devcontainer/devcontainer.json`

Standard devcontainer.json fields work as expected. AI safety settings go in `customizations.devc`:

```jsonc
{
  "name": "my-project",
  "image": "mcr.microsoft.com/devcontainers/base:ubuntu",
  "features": {
    "ghcr.io/devcontainers/features/node:1": {},
  },
  "postCreateCommand": "npm install",
  "customizations": {
    "devc": {
      "agent": "claude",
      "securityProfile": "moderate",
      "network": {
        "mode": "restricted",
        "allowlist": ["api.anthropic.com", "registry.npmjs.org"],
      },
      "resources": {
        "cpus": "4",
        "memory": "8g",
        "pidsLimit": 256,
      },
      "session": {
        "stopOnLastDetach": true,
      },
    },
  },
}
```

### Global: `~/.devc/config.json`

User-level defaults that apply to all projects unless overridden at the project level.

### Security profiles

| Control      | Strict      | Moderate (default)  | Permissive      |
| ------------ | ----------- | ------------------- | --------------- |
| Network      | None        | Bridge (allowlist¹) | Host network    |
| Capabilities | Drop ALL    | Drop ALL + minimal  | Docker defaults |
| Resources    | 2 CPU, 4 GB | 4 CPU, 8 GB         | Unlimited       |
| User         | Non-root    | Non-root            | Non-root        |

¹ The network allowlist is advisory by default; set `network.enforce` to apply it
as a real egress firewall (see the [secure workflow](docs/secure-local-agent.md)).

### Secure local agent workflow

An opt-in workflow that lets an agent edit code and commit without host
credentials, blocks in-repo secrets from reaching the agent, and disables
`git push`:

```bash
devc init --preset secure-local-agent --agent claude
```

For maximum isolation use `--preset secure-local-strict`: it withholds **all**
host credentials (the agent authenticates inside the container) and turns on an
enforced egress firewall.

See [docs/secure-local-agent.md](docs/secure-local-agent.md) for the threat
model, the `credentialPolicy` / `workspaceSecretsPolicy` / `gitPolicy` settings,
the read-only skills mount, service containers (Postgres/Redis), `forwardPorts`,
and opt-in egress filtering.

## Supported container runtimes

`devc` communicates with container runtimes via the Docker Engine API — it does not shell out to a CLI binary. Any
runtime that exposes a Docker-compatible API socket will work.

| Runtime                                                           | Status          | Notes                                                        |
| ----------------------------------------------------------------- | --------------- | ------------------------------------------------------------ |
| [Docker Desktop](https://www.docker.com/products/docker-desktop/) | Fully supported | Default socket at `/var/run/docker.sock`                     |
| [Colima](https://github.com/abiosoft/colima)                      | Fully supported | Runs real dockerd; socket at `~/.colima/default/docker.sock` |
| [Rancher Desktop](https://rancherdesktop.io/) (moby mode)         | Fully supported | Runs real dockerd; socket at `~/.rd/docker.sock`             |
| [OrbStack](https://orbstack.dev/)                                 | Fully supported | Own engine with near-100% Docker API compat                  |
| [Podman](https://podman.io/)                                      | Supported       | Compat API layer; Podman 5.x+ recommended                    |
| [Finch](https://github.com/runfinch/finch)                        | Experimental    | Partial Docker API v1.43 via finch-daemon                    |

### Configuring non-default runtimes

`devc` reads the standard `DOCKER_HOST` environment variable to locate the container runtime socket:

```sh
# Colima
export DOCKER_HOST="unix://$HOME/.colima/default/docker.sock"

# Rancher Desktop (without admin access)
export DOCKER_HOST="unix://$HOME/.rd/docker.sock"

# Podman
export DOCKER_HOST="unix://$(podman machine inspect --format '{{.ConnectionInfo.PodmanSocket.Path}}')"

# OrbStack
export DOCKER_HOST="unix://$HOME/.orbstack/run/docker.sock"
```

Alternatively, configure a [Docker context](https://docs.docker.com/engine/manage-resources/contexts/) and `devc` will
use it automatically.

## License

[MIT](LICENSE)
