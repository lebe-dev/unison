# Unison

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
- [Security](docs/security.md) - the model in short
- [Troubleshooting](docs/troubleshooting.md) - the usual reasons a device does not connect

Building the code and running the tests is described in `DEV.md`.
