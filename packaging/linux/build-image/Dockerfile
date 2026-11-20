# Build image of the Linux package: Ubuntu 22.04 with the WebKitGTK, Rust and
# Node toolchains of the desktop app. The 22.04 userland keeps the package
# usable on the glibc of both Ubuntu 22.04 and 24.04.
FROM node:26.13.1-bookworm-slim AS node
FROM rust:1.100.0-bookworm AS rust

FROM ubuntu:22.04

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        build-essential \
        ca-certificates \
        curl \
        file \
        libayatana-appindicator3-dev \
        libgtk-3-dev \
        librsvg2-dev \
        libssl-dev \
        libwebkit2gtk-4.1-dev \
        libxdo-dev \
        patchelf \
        pkg-config \
    && rm -rf /var/lib/apt/lists/*

# The frontend needs the Node major version of the project and yarn 1.22.
COPY --from=node /usr/local/bin/node /usr/local/bin/node
COPY --from=node /usr/local/lib/node_modules/npm /usr/local/lib/node_modules/npm
RUN ln -s ../lib/node_modules/npm/bin/npm-cli.js /usr/local/bin/npm \
    && ln -s ../lib/node_modules/npm/bin/npx-cli.js /usr/local/bin/npx \
    && npm install --global yarn@1.22.22 \
    && npm cache clean --force

# The Rust toolchain of the project. The default host target is
# x86_64-unknown-linux-gnu, so no extra target has to be installed.
COPY --from=rust /usr/local/rustup /usr/local/rustup
COPY --from=rust /usr/local/cargo /usr/local/cargo
ENV RUSTUP_HOME=/usr/local/rustup \
    CARGO_HOME=/usr/local/cargo \
    PATH=/usr/local/cargo/bin:$PATH

RUN rustc --version && cargo --version && node --version && yarn --version
