# Agent dev base image (example)

A reusable base image for the [secure-local-agent workflow](../../../docs/secure-local-agent.md)
with C/C++, Go, Node, and Python toolchains plus common CLI tools preinstalled.

This is an **example**. devc does not build or publish it for you — build it
yourself and point `devcontainer.json` at the resulting tag.

## What's inside

- **C/C++**: build-essential, gcc, g++, clang, clang-format, cmake, make, gdb, lldb, pkg-config
- **Go**: go, gopls, dlv, goimports, gofumpt, govulncheck, golangci-lint
- **Go (proto/RPC)**: buf, protoc-gen-go, protoc-gen-connect-go, protoc-gen-go-grpc
- **Go (db)**: pgxgen, sqlc, golang-migrate
- **Go (dev)**: swag (swaggo), mockgen, gotestsum, air
- **Node**: Node.js (latest, 26.x), npm, pnpm, yarn
- **Node (proto/RPC)**: protoc-gen-es, protoc-gen-connect-query (buf TS codegen)
- **Python**: python3, python3-dev, python3-venv, pipx, uv, poetry, ruff, black, mypy
- **Common**: git, git-lfs, curl, wget, jq, yq, ripgrep, fd, tree, unzip, zip,
  shellcheck, shfmt, openssl, ca-certificates, postgresql-client, redis-tools, sqlite3
- **Network egress** (`network.enforce`): iptables, dnsutils

It is based on `mcr.microsoft.com/devcontainers/base:ubuntu`, so it keeps the
non-root `vscode` user (uid/gid `1000`) that devc expects.

## Build

```bash
docker build -t agent-dev-base:latest examples/images/agent-dev-base
```

## Use

Reference the image from your `devcontainer.json`:

```json
{
  "name": "my-app",
  "image": "agent-dev-base:latest",
  "forwardPorts": [3000, 8080],
  "customizations": {
    "devc": {
      "preset": "secure-local-agent",
      "agent": "claude"
    }
  }
}
```

Then `devc up`.

## Notes

- Pin `GO_VERSION` and the tool versions for reproducible builds.
- The image is intentionally not minimal; trim the toolchains you don't need to
  speed up builds.
- `TARGETARCH` is auto-detected from the build platform, so the image builds
  natively on amd64 and arm64 with no extra flags (use buildx for multi-arch).
