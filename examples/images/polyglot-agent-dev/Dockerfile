# Polyglot agent dev image (example).
#
# A reusable base image for the secure-local-agent workflow with C/C++, Go,
# Node, Python, and common CLI tooling preinstalled. This is an EXAMPLE — it is
# not built or published by devc. Build it yourself and reference it from
# devcontainer.json:
#
#   docker build -t agent-dev-base:latest examples/images/polyglot-agent-dev
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
    && git lfs install --system \
    && rm -rf /var/lib/apt/lists/*

# --- yq and shfmt (not in apt; install static binaries) ---------------------
ARG TARGETARCH=amd64
RUN curl -fsSL "https://github.com/mikefarah/yq/releases/latest/download/yq_linux_${TARGETARCH}" -o /usr/local/bin/yq \
    && chmod 0755 /usr/local/bin/yq \
    && curl -fsSL "https://github.com/mvdan/sh/releases/latest/download/shfmt_v3.10.0_linux_${TARGETARCH}" -o /usr/local/bin/shfmt \
    && chmod 0755 /usr/local/bin/shfmt

# --- fd (apt ships it as fd-find / fdfind; expose a `fd` alias) -------------
RUN apt-get update && apt-get install -y --no-install-recommends fd-find ripgrep \
    && ln -sf "$(command -v fdfind)" /usr/local/bin/fd \
    && rm -rf /var/lib/apt/lists/*

# --- Go toolchain + tools ---------------------------------------------------
ARG GO_VERSION=1.23.4
RUN curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-${TARGETARCH}.tar.gz" | tar -C /usr/local -xz
ENV PATH="/usr/local/go/bin:/home/vscode/go/bin:${PATH}"
ENV GOPATH="/home/vscode/go"
USER vscode
RUN go install golang.org/x/tools/gopls@latest \
    && go install github.com/go-delve/delve/cmd/dlv@latest \
    && go install golang.org/x/tools/cmd/goimports@latest
USER root

# --- Node.js LTS + package managers ----------------------------------------
RUN curl -fsSL https://deb.nodesource.com/setup_lts.x | bash - \
    && apt-get install -y --no-install-recommends nodejs \
    && corepack enable \
    && corepack prepare pnpm@latest --activate \
    && corepack prepare yarn@stable --activate \
    && rm -rf /var/lib/apt/lists/*

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
