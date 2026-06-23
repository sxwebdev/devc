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

This expands (via the `preset` field) to roughly:

```json
{
  "customizations": {
    "devc": {
      "preset": "secure-local-agent",
      "agent": "claude",
      "securityProfile": "moderate",
      "credentialPolicy": "agentOnly",
      "gitPolicy": "commitOnly",
      "workspaceSecretsPolicy": { "enabled": true, "mode": "fail" },
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

When `patterns`/`allowPatterns` are omitted, the built-in default lists are used
(see [`workspaceSecretsPolicy`](#workspacesecretspolicy)). `devc init --preset`
additionally writes an explicit starter `patterns`/`allowPatterns` set (and
example `services`) into your `devcontainer.json` that you can edit.

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
- Outbound network beyond the allowlist — only when `network.enforce=true` and
  `iptables` is present (experimental)

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

| Mode       | Behavior                                                                                                 |
| ---------- | -------------------------------------------------------------------------------------------------------- |
| `off`      | Do nothing (existing behavior).                                                                          |
| `fail`     | Refuse to start and list the protected files. Recommended.                                               |
| `readonly` | Mount each protected file read-only (the agent can still read it — less safe).                           |
| `mask`     | Shadow each protected file with an empty file so the agent cannot read its contents (technical control). |

`mask` is a real technical control: each matched file is bind-mounted over with
an empty, read-only file, so the agent sees it as empty rather than seeing the
secret. The workspace stays writable; only the matched files are shadowed. Files
created after container start are not masked.

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

## Network egress enforcement (experimental)

By default `restricted` is a standard bridge network with no outbound filtering —
the domain allowlist is advisory. Set `network.enforce: true` to turn the
allowlist into a real egress firewall:

```json
{
  "network": {
    "mode": "restricted",
    "allowlist": ["api.anthropic.com", "internal.example.com"],
    "enforce": true
  }
}
```

When enabled, after setup/install commands have run, `devc` installs an iptables
OUTPUT firewall (as root) that defaults to **DROP** and allows only:

- loopback and established/related connections,
- DNS (port 53),
- private networks (`127/8`, `10/8`, `172.16/12`, `192.168/16`) — needed for the
  Docker resolver and sibling services,
- the resolved IPv4 addresses of each agent profile's required domains plus your
  `allowlist`.

The agent runs as a non-root user, so it cannot flush these rules.

> **Trade-off / caveats (read before enabling):**
>
> - The container is granted `NET_ADMIN` and `NET_RAW` capabilities so the root
>   init script can program iptables. This weakens the otherwise minimal
>   capability set; the protection relies on the agent being non-root.
> - It requires `iptables` in the image. If `iptables` is missing the firewall is
>   **skipped with a warning** (fail-open) — outbound traffic is not restricted.
> - Domains are resolved to IPs at setup time. CDN/anycast services whose IPs
>   rotate may become unreachable. Add domains conservatively and test.
> - Behavior varies across Docker hosts (rootless, Docker Desktop, unusual
>   networking). Verify on your setup.
> - **Not a hard exfiltration boundary.** To let the agent reach sibling
>   services, all private ranges (`10/8`, `172.16/12`, `192.168/16`) and DNS
>   (port 53 to any resolver) are allowed. A determined agent can still reach
>   LAN hosts / other containers on private IPs or tunnel data over DNS. The
>   filter blocks accidental/lazy egress to non-allowlisted public IPs, not a
>   motivated adversary.
>
> Because of these caveats, `enforce` is **opt-in** and is not turned on by the
> `secure-local-agent` preset.

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

## Services (databases, brokers, …)

Any container image can run as a sibling service alongside the agent, reachable
from the agent by DNS name and from the host on `127.0.0.1` ports. The agent
never receives the host Docker socket — `devc` manages the service containers
from the host.

Postgres and Redis are not special — they are just the keys with built-in
defaults. RabbitMQ, Kafka, NATS, MongoDB, MySQL, and others work the same way;
many also have built-in defaults (see the table below). For anything else, set
`image` + `containerPort` + `hostPort` and provide the connection string via
`agentEnv`.

```json
{
  "services": {
    "postgres": {
      "enabled": true,
      "image": "postgres:16",
      "containerPort": 5432,
      "hostPort": 54321,
      "hostIP": "127.0.0.1",
      "env": {
        "POSTGRES_USER": "app",
        "POSTGRES_PASSWORD": "app",
        "POSTGRES_DB": "app"
      },
      "volumes": [
        { "name": "postgres-data", "target": "/var/lib/postgresql/data" }
      ]
    },
    "redis": {
      "enabled": true,
      "image": "redis:7",
      "containerPort": 6379,
      "hostPort": 63791,
      "hostIP": "127.0.0.1"
    }
  }
}
```

Behavior:

- A per-project bridge network (`devc-net-<container>`) is created; the agent and
  service containers join it. Services get DNS aliases matching their keys.
- The agent connects via DNS: `postgres:5432`, `redis:6379`.
- Ports publish to `127.0.0.1` only by default (set `hostIP`/`hostPort`).
- For well-known services, connection-string env vars are injected into the
  agent: `DATABASE_URL=postgresql://app:app@postgres:5432/app` and
  `REDIS_URL=redis://redis:6379`. Override the injected variables per service
  with `agentEnv` (see below).
- Services and the network are removed on `devc down` / `devc clean`. Named
  volumes are **preserved** (delete them manually with `docker volume rm` if you
  want a clean slate).

### Built-in defaults

For these service keys, `containerPort` may be omitted and a connection-string
env var is injected automatically (override either with `containerPort` /
`agentEnv`):

| Service key                  | Default port | Injected env var |
| ---------------------------- | ------------ | ---------------- |
| `postgres` / `postgresql`    | 5432         | `DATABASE_URL`   |
| `mysql` / `mariadb`          | 3306         | `DATABASE_URL`   |
| `redis` / `valkey`           | 6379         | `REDIS_URL`      |
| `mongo` / `mongodb`          | 27017        | `MONGO_URL`      |
| `rabbitmq`                   | 5672         | `AMQP_URL`       |
| `nats`                       | 4222         | `NATS_URL`       |
| `kafka`                      | 9092         | `KAFKA_BROKERS`  |
| `clickhouse`                 | 9000         | —                |
| `elasticsearch`/`opensearch` | 9200         | —                |
| `memcached`                  | 11211        | —                |

Anything else also works — give it a `containerPort` and an `agentEnv`:

```json
{
  "services": {
    "rabbitmq": {
      "enabled": true,
      "image": "rabbitmq:3-management",
      "hostPort": 5672,
      "env": { "RABBITMQ_DEFAULT_USER": "app", "RABBITMQ_DEFAULT_PASS": "app" }
    },
    "nats": { "enabled": true, "image": "nats:2", "hostPort": 4222 },
    "kafka": {
      "enabled": true,
      "image": "bitnami/kafka:3.7",
      "hostPort": 9092
    },
    "qdrant": {
      "enabled": true,
      "image": "qdrant/qdrant",
      "containerPort": 6333,
      "hostPort": 6333,
      "agentEnv": { "QDRANT_URL": "http://qdrant:6333" }
    }
  }
}
```

The agent reaches each service by its key as DNS name (`rabbitmq:5672`,
`nats:4222`, `kafka:9092`, `qdrant:6333`). Two services that both default to
`DATABASE_URL` (e.g. postgres + mysql) collide — use `agentEnv` to give one a
distinct variable.

### Customizing the injected connection env

To change the variable name or value the agent receives, set `agentEnv` on the
service. It replaces the default derivation:

```json
{
  "services": {
    "postgres": {
      "enabled": true,
      "image": "postgres:16",
      "agentEnv": {
        "PG_DSN": "postgres://app:app@postgres:5432/app?sslmode=disable"
      }
    }
  }
}
```

### Connect from the host

Use any database client on the host (`psql`, DBeaver, TablePlus, …). With the
example above:

```text
Host: 127.0.0.1
Port: 54321
Database: app
User: app
Password: app
```

Redis:

```text
Host: 127.0.0.1
Port: 63791
```

## Accessing your app (frontend / backend) from the host

Dev servers the agent runs **inside** the container (a frontend on `:3000`, a
backend API on `:8080`, …) are reachable from your host browser/tools by
publishing their ports with the standard devcontainer `forwardPorts` field.
Ports publish to `127.0.0.1` only by default.

```json
{
  "name": "my-app",
  "image": "mcr.microsoft.com/devcontainers/base:ubuntu",
  "forwardPorts": [3000, "8080:8080"],
  "customizations": {
    "devc": {
      "preset": "secure-local-agent",
      "agent": "claude"
    }
  }
}
```

`forwardPorts` entry forms:

| Entry                   | Result                                          |
| ----------------------- | ----------------------------------------------- |
| `3000`                  | `127.0.0.1:3000` on the host → container `3000` |
| `"8080:3000"`           | `127.0.0.1:8080` on the host → container `3000` |
| `"127.0.0.1:8080:3000"` | explicit host IP → container `3000`             |
| `"5353/udp"`            | UDP port                                        |

Inside the container, start your servers bound to `0.0.0.0` (not `127.0.0.1`) so
the published ports are reachable from the host:

```bash
# inside the container (devc exec or the attached shell)
npm run dev -- --host 0.0.0.0 --port 3000      # frontend
go run ./cmd/api                                # backend listening on 0.0.0.0:8080
```

Then from the host:

```text
Frontend:  http://localhost:3000
Backend:   http://localhost:8080
```

The backend talks to the database over the container network using the injected
`DATABASE_URL` (`postgresql://app:app@postgres:5432/app`) — no host ports needed
for service-to-service traffic.

> **Tips**
>
> - Bind dev servers to `0.0.0.0`. A server bound to `127.0.0.1` inside the
>   container is not reachable from the host even when the port is published.
> - Changing `forwardPorts` changes the container's config hash, so `devc up`
>   offers to rebuild the container to apply new port mappings.
> - No host browser needed? You can also reach a server without publishing:
>   `devc exec -- curl -s http://localhost:3000`.

## Custom base image

The secure workflow works with any image that provides the non-root `vscode`
user (uid/gid `1000`). To give the agent a richer toolchain, point `image` at
your own build:

```json
{
  "image": "agent-dev-base:latest",
  "customizations": {
    "devc": { "preset": "secure-local-agent", "agent": "claude" }
  }
}
```

A ready-to-build polyglot example (C/C++, Go, Node, Python + common CLI tools)
lives in [`examples/images/polyglot-agent-dev`](../examples/images/polyglot-agent-dev):

```bash
docker build -t agent-dev-base:latest examples/images/polyglot-agent-dev
```

If you enable `network.enforce`, make sure your image includes `iptables` (and
`getent`/`dnsutils`), or egress filtering will be skipped with a warning.

## Known limitations

- `readonly` and `mask` secrets only cover files present at container creation
  time; files created later are not protected.
- Service containers need a bridge-style network. They are skipped (with a
  warning) under `strict` (`none`) and `permissive` (`host`) network modes,
  since the agent can't resolve their DNS aliases there — reach services via
  their published `127.0.0.1` ports instead.
- Services are re-ensured on each `devc up`, but a service removed while the
  agent is gone entirely (no `devc down`) can be left as an orphan that the
  restart policy keeps alive; remove it with `docker rm`/`docker volume rm`.
- Egress filtering (`network.enforce`) is experimental, opt-in, fail-open when
  `iptables` is missing, and resolves domains to IPs at setup time. Without it,
  outbound network access is open.
- The `git push` wrapper can be bypassed by absolute-path git invocations.

## Verifying the protections

After `devc up`, confirm the controls from inside the container:

```bash
# Host credentials are withheld (none/agentOnly)
devc exec -- env | grep -E 'GH_TOKEN|GITHUB_TOKEN|GITLAB_TOKEN|SSH_AUTH_SOCK|AWS_|KUBECONFIG' || echo "no host creds"
devc exec -- ls -la ~/.ssh 2>&1 || echo "no ssh mount"

# Skills mount is present and read-only
devc exec -- ls -la /skills
devc exec -- touch /skills/x 2>&1 || echo "read-only as expected"

# git commit works, git push is blocked (commitOnly)
devc exec -- git commit --allow-empty -m test
devc exec -- git push 2>&1 || echo "push blocked as expected"

# Services reachable from the agent (DNS) and the host (localhost)
devc exec -- psql "$DATABASE_URL" -c 'select 1'
psql -h 127.0.0.1 -p 54321 -U app -d app -c 'select 1'
```

Workspace secrets policy (`mode=fail`):

```bash
touch .env && devc up          # refuses to start, lists .env
rm .env && touch .env.example && devc up   # starts (allow-listed)
```
