# Unison

[Русский](README.ru.md) | English

Unison moves the clipboard, images and files between your own devices over the local network, and
sends single items to friends. Trust is per device: two devices are paired once, and there is no
account and no service you have to trust.

macOS and Android are supported. Windows and Linux are not available yet.

## Documentation

- [Install](docs/install.md) - the macOS app and the Android app, and how to remove them
- [Devices and pairing](docs/devices.md) - roles, the pairing window, QR codes, revocation
- [Clipboard and history](docs/clipboard.md) - what is shared, images and files, pause, history
- [Network policies](docs/policy.md) - what arrives by itself on which network
- [Relay](docs/relay.md) - a self-hosted relay for devices that are not in one network
- [Keyboard bridge](docs/keyboard-bridge.md) - the Android interface for a keyboard
- [Command line](docs/cli.md) - the `unison` client of the daemon
- [Security](docs/security.md) - keys, transport, pairing, untrusted content, data at rest
- [FAQ](docs/faq.md) - the questions asked most often, mostly about security
- [Troubleshooting](docs/troubleshooting.md) - the usual reasons a device does not connect

## Architecture

- [Overview](docs/architecture/overview.md) - the processes, the layers and the data directory
- [Core](docs/architecture/core.md) - the domain packages and how the node composes them
- [Protocol](docs/architecture/protocol.md) - transport, session, discovery, pairing and the domain messages
- [Daemon and local API](docs/architecture/local-api.md) - the daemon, its endpoint and the CLI
- [Relay](docs/architecture/relay.md) - the relay client, the server and its deployment
- [Desktop app](docs/architecture/desktop.md) - the Tauri shell and the Svelte frontend
- [macOS](docs/architecture/macos.md) - the darwin ports and the bundle
- [Windows](docs/architecture/windows.md) - the windows ports and the installer
- [Android](docs/architecture/android.md) - the mobile binding, the service and the app
- [Application log](docs/architecture/application-log.md) - the log the user can turn on and share

Building the code and running the tests is described in `DEV.md`.
