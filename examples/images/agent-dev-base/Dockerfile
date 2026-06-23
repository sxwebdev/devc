# Agent dev base image (example).
#
# A reusable base image for the secure-local-agent workflow with C/C++, Go,
# Node, Python, and common CLI tooling preinstalled. This is an EXAMPLE — it is
# not built or published by devc. Build it yourself and reference it from
# devcontainer.json:
#
#   docker build -t agent-dev-base:latest examples/images/agent-dev-base
#   # devcontainer.json: "image": "agent-dev-base:latest"
#
# Based on the Microsoft devcontainer base image, which already provides the
# non-root `vscode` user (uid/gid 1000) that devc expects, plus git, curl, etc.

FROM mcr.microsoft.com/devcontainers/base:ubuntu

ENV DEBIAN_FRONTEND=noninteractive

# --- System packages: C/C++, Python, and common tools -----------------------
RUN apt-get update && apt-get install -y --no-install-recommends \
    build-essential gcc g++ clang clang-format cmake make gdb lldb pkg-config \
    python3 python3-dev python3-venv python3-pip pipx \
    git git-lfs curl wget jq tree unzip zip shellcheck \
    openssl ca-certificates postgresql-client redis-tools sqlite3 \
    iptables dnsutils \
    && git lfs install --system \
    && rm -rf /var/lib/apt/lists/*

# --- yq and shfmt (not in apt; install static binaries) ---------------------
# TARGETARCH is auto-populated by BuildKit from the build platform (amd64/arm64).
# Do NOT give it a default — a default shadows BuildKit's value and breaks native
# arm64 builds (Go/gcc arch mismatch -> "unrecognized command-line option '-m64'").
ARG TARGETARCH
RUN curl -fsSL "https://github.com/mikefarah/yq/releases/latest/download/yq_linux_${TARGETARCH}" -o /usr/local/bin/yq \
    && chmod 0755 /usr/local/bin/yq \
    && curl -fsSL "https://github.com/mvdan/sh/releases/latest/download/shfmt_v3.13.1_linux_${TARGETARCH}" -o /usr/local/bin/shfmt \
    && chmod 0755 /usr/local/bin/shfmt

# --- fd (apt ships it as fd-find / fdfind; expose a `fd` alias) -------------
RUN apt-get update && apt-get install -y --no-install-recommends fd-find ripgrep \
    && ln -sf "$(command -v fdfind)" /usr/local/bin/fd \
    && rm -rf /var/lib/apt/lists/*

# --- Go toolchain + tools ---------------------------------------------------
ARG GO_VERSION=1.26.4
RUN curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-${TARGETARCH}.tar.gz" | tar -C /usr/local -xz
ENV PATH="/usr/local/go/bin:/home/vscode/go/bin:${PATH}"
ENV GOPATH="/home/vscode/go"
USER vscode
# Core language tooling, formatters, linters, security scanners.
RUN go install golang.org/x/tools/gopls@latest \
    && go install github.com/go-delve/delve/cmd/dlv@latest \
    && go install golang.org/x/tools/cmd/goimports@latest \
    && go install mvdan.cc/gofumpt@latest \
    && go install golang.org/x/vuln/cmd/govulncheck@latest \
    && go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest
# Protobuf / ConnectRPC / gRPC codegen: buf + the protoc-gen-* plugins it invokes.
RUN go install github.com/bufbuild/buf/cmd/buf@latest \
    && go install google.golang.org/protobuf/cmd/protoc-gen-go@latest \
    && go install connectrpc.com/connect/cmd/protoc-gen-connect-go@latest \
    && go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
# SQL / DB codegen + migrations (migrate needs the postgres driver build tag).
RUN go install github.com/tkcrm/pgxgen/cmd/pgxgen@latest \
    && go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest \
    && go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest
# Dev workflow: OpenAPI docs, mocks, test runner, live reload.
RUN go install github.com/swaggo/swag/cmd/swag@latest \
    && go install go.uber.org/mock/mockgen@latest \
    && go install gotest.tools/gotestsum@latest \
    && go install github.com/air-verse/air@latest
USER root

# --- Node.js (latest) + package managers ------------------------------------
# Node 25+ no longer bundles corepack, so install pnpm/yarn globally via npm.
RUN curl -fsSL https://deb.nodesource.com/setup_26.x | bash - \
    && apt-get install -y --no-install-recommends nodejs \
    && npm install -g npm@latest pnpm@latest yarn@latest \
    && rm -rf /var/lib/apt/lists/*

# buf TypeScript codegen plugins (Node-based), so buf.gen.yaml can run them as
# `local:` instead of `remote:` — protoc-gen-es (buf.build/bufbuild/es) and
# protoc-gen-connect-query (buf.build/connectrpc/query-es, TanStack Query hooks).
RUN npm install -g @bufbuild/protoc-gen-es @connectrpc/protoc-gen-connect-query

# --- Python tools via pipx (isolated, on PATH for vscode) -------------------
USER vscode
ENV PATH="/home/vscode/.local/bin:${PATH}"
RUN pipx install uv \
    && pipx install poetry \
    && pipx install ruff \
    && pipx install black \
    && pipx install mypy
USER root

ENV DEBIAN_FRONTEND=
USER vscode
