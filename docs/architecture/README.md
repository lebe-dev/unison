# Architecture

[Русский](README.ru.md) | English

These pages describe how the repository is organised and how its parts talk to each other. Start
with the [overview](overview.md), which places the processes and the layers; every other page
covers one area in depth.

## Foundations

| Page | Content |
|------|---------|
| [Overview](overview.md) | the processes, the layers, the data directory and the trust boundaries |
| [Core](core.md) | the Go packages, the node composition, storage, identity, trust and the domains |
| [Protocol](protocol.md) | the wire format, transport, sessions, discovery, pairing and the domain messages |

## Processes

| Page | Content |
|------|---------|
| [Daemon and local API](local-api.md) | the daemon, its endpoint, the CLI and the local API |
| [Relay](relay.md) | the relay client, the server and its deployment |

## Clients

| Page | Content |
|------|---------|
| [Desktop app](desktop.md) | the Tauri shell and the Svelte frontend |
| [macOS](macos.md) | the darwin ports and the bundle |
| [Windows](windows.md) | the windows ports and the installer |
| [Android](android.md) | the mobile binding, the service and the app |

## Cross-cutting concerns

| Page | Content |
|------|---------|
| [Application log](application-log.md) | the log the user can turn on and share |
| [Settings file](settings-file.md) | the JSON document the user keeps and restores |
