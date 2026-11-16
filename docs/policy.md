# Network policies

What arrives from the other devices is decided on the device that receives, from three things: the
network it is on, the type of the content and the role of the device that sends it.

## Conditions

| Condition | Meaning |
|-----------|---------|
| unmetered | an online network without restrictions, wired or Wi-Fi |
| metered | a network billed by volume, for example mobile data |
| roaming | a roaming network |
| data saver | the system data saver is on |

A restriction wins over a weaker one, so the data saver row applies on a metered roaming network.

## Actions

| Action | What happens |
|--------|--------------|
| reject | the item is dropped and not recorded |
| metadata | the item is recorded, the payload is downloaded when you ask for it |
| confirm | the item is recorded and waits for you to apply it |
| auto | the item is applied at once |

`auto` carries a size limit. An item above the limit is kept as metadata instead, so a large image
on mobile data does not download by itself.

## Defaults

| Condition | Text | Image | Files | Relay |
|-----------|------|-------|-------|-------|
| unmetered | auto up to 10 MiB | auto up to 10 MiB | metadata | yes |
| metered | auto up to 1 MiB | auto up to 1 MiB | metadata | yes |
| roaming | auto up to 1 MiB | metadata | metadata | no |
| data saver | auto up to 1 MiB | metadata | metadata | no |

The `files` cell covers files that are part of a clipboard copy. A file offered with
`unison send` is an explicit offer of the sender, so a Personal device receives it at once, within
the size limit of the cell.

A Friends device can only offer files, and every offer waits for your decision. Its clipboard items
are refused whatever the network.

The last column allows the relay on that network; `relay limit` caps the items fetched eagerly over
the relay, whatever the condition. A larger item waits in the history like any other metadata item.
Files over the relay wait for you by default, even when their cell allows them; see
[Relay](relay.md).

## Changing the table

The desktop app has the table under `Settings > Network policy`, the Android app under
`Settings`, and both offer `Reset to defaults` and the relay switches. The command line prints it:

```sh
unison policy
```

A change applies to the next item, without restarting the daemon.

The confirmation flags and the role of a device are separate from this table, see
[Devices and pairing](devices.md#confirmation-flags).
