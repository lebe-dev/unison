<script lang="ts">
  import { Role, type Device } from "../api/gen/unison/local/v1/local_pb";
  import { client } from "../api/client";
  import { confirmation, type Scope, type Side } from "../confirmation.svelte";
  import { daemon } from "../daemon.svelte";
  import { errorText } from "../errors";
  import { formatDateTime, formatTime, pathLabel, platformLabel, roleLabel, shortDeviceID } from "../format";

  let { onAdd }: { /** Opens the screen that adds a device. */ onAdd?: () => void } = $props();

  const groups = $derived([
    {
      role: Role.PERSONAL,
      title: "Personal",
      devices: daemon.devices.filter((device) => device.role === Role.PERSONAL),
    },
    {
      role: Role.FRIENDS,
      title: "Friends",
      devices: daemon.devices.filter((device) => device.role === Role.FRIENDS),
    },
  ]);

  let editing = $state<string | null>(null);
  let draftName = $state("");
  let revoking = $state<string | null>(null);
  let notice = $state<{ kind: "info" | "error"; text: string } | null>(null);
  let busy = $state(false);

  function beginRename(device: Device): void {
    editing = device.deviceId;
    draftName = device.name;
    revoking = null;
    notice = null;
  }

  function cancelRename(): void {
    editing = null;
    draftName = "";
  }

  async function rename(device: Device): Promise<void> {
    const name = draftName.trim();
    if (!name) {
      notice = { kind: "error", text: "The name cannot be empty." };
      return;
    }
    busy = true;
    notice = null;
    try {
      await client.renameDevice({ deviceId: device.deviceId, name });
      editing = null;
      await daemon.refresh();
      notice = { kind: "info", text: `The device is now called ${name}.` };
    } catch (error) {
      notice = {
        kind: "error",
        text: errorText(error, "The device could not be renamed."),
      };
    } finally {
      busy = false;
    }
  }

  /** Switches of the confirmation flags, in the order of the grid. */
  const flags: { side: Side; scope: Scope }[] = [
    { side: "local", scope: "clipboard" },
    { side: "local", scope: "files" },
    { side: "peer", scope: "clipboard" },
    { side: "peer", scope: "files" },
  ];

  /** flagLabel names one switch for a screen reader. */
  function flagLabel(device: Device, side: Side, scope: Scope): string {
    const name = device.name || "The device";
    if (side === "local") {
      return scope === "clipboard"
        ? `This device asks before applying the clipboard of ${name}`
        : `This device asks before accepting files from ${name}`;
    }
    return scope === "clipboard"
      ? `${name} asks before applying the clipboard of this device`
      : `${name} asks before accepting files from this device`;
  }

  async function setConfirmation(device: Device, scope: Scope, side: Side, require: boolean): Promise<void> {
    notice = null;
    const failure = await confirmation.set(device.deviceId, scope, side, require);
    if (!failure) {
      return;
    }
    notice = { kind: "error", text: failure };
  }

  async function revoke(device: Device): Promise<void> {
    busy = true;
    notice = null;
    try {
      await client.revokeDevice({ deviceId: device.deviceId });
      revoking = null;
      await daemon.refresh();
      notice = {
        kind: "info",
        text: `${device.name || "The device"} is revoked and cannot reconnect.`,
      };
    } catch (error) {
      notice = { kind: "error", text: errorText(error, "The device could not be revoked.") };
    } finally {
      busy = false;
    }
  }
</script>

{#if !daemon.connected}
  <section class="state">
    {#if daemon.loading}
      <p class="muted">Connecting to the daemon...</p>
    {:else}
      <p class="notice error">The daemon is not running, so paired devices cannot be shown.</p>
      <button class="button" type="button" onclick={() => void daemon.refresh()}>Try again</button>
    {/if}
  </section>
{:else}
  <div class="devices">
    <section class="self">
      <span class="muted">This device</span>
      <strong>{daemon.deviceName}</strong>
      <span class="mono muted">{shortDeviceID(daemon.deviceID)}</span>
    </section>

    {#if notice}
      <p class="notice" class:error={notice.kind === "error"} class:info={notice.kind === "info"}>
        {notice.text}
      </p>
    {/if}

    {#each groups as group (group.role)}
      <section class="group">
        <div class="panel-header">
          <h3>
            {group.title}
            <span class="count">{group.devices.length}</span>
          </h3>
          {#if group.role === Role.PERSONAL && onAdd}
            <button class="button small" type="button" onclick={onAdd}>Add device</button>
          {/if}
        </div>

        {#if group.devices.length === 0}
          <p class="empty">
            {group.role === Role.PERSONAL
              ? "No Personal devices yet."
              : "No Friends yet."}
          </p>
        {:else}
          <ul>
            {#each group.devices as device (device.deviceId)}
              <li class="device">
                <div class="head">
                  <div class="identity">
                    <strong>{device.name || "Unnamed device"}</strong>
                    <span class="mono muted">{shortDeviceID(device.deviceId)}</span>
                  </div>
                  <div class="badges">
                    <span class="badge" class:online={device.online}>
                      {device.online ? "Online" : "Offline"}
                    </span>
                    {#if device.online && pathLabel(device.path)}
                      <span class="badge">{pathLabel(device.path)}</span>
                    {/if}
                  </div>
                </div>

                <p class="meta muted">
                  {platformLabel(device.platform)} - {roleLabel(device.role)}
                  {#if device.addedAtUnixMs}
                    - added {formatDateTime(device.addedAtUnixMs)}
                  {/if}
                  {#if !device.online && device.lastSeenAtUnixMs}
                    - last seen {formatTime(device.lastSeenAtUnixMs)}
                  {/if}
                </p>

                {#if device.role === Role.PERSONAL}
                  <div class="confirm" role="group" aria-label="Ask before taking">
                    <span class="confirm-title">Ask before taking</span>
                    <span class="confirm-head">Clipboard</span>
                    <span class="confirm-head">Files</span>
                    {#each flags as flag (`${flag.side}:${flag.scope}`)}
                      {#if flag.scope === "clipboard"}
                        <span class="confirm-row">
                          {flag.side === "local"
                            ? `Here, from ${device.name || "the device"}`
                            : `On ${device.name || "the device"}, from here`}
                        </span>
                      {/if}
                      <input
                        type="checkbox"
                        aria-label={flagLabel(device, flag.side, flag.scope)}
                        title={flagLabel(device, flag.side, flag.scope)}
                        checked={confirmation.require(device, flag.scope, flag.side)}
                        disabled={confirmation.isBusy(device.deviceId, flag.scope, flag.side) ||
                          (flag.side === "peer" && !device.online)}
                        onchange={(event) =>
                          void setConfirmation(device, flag.scope, flag.side, event.currentTarget.checked)}
                      />
                    {/each}
                    <small class="confirm-hint muted">
                      What waits for an answer shows up in a prompt.
                      {#if !device.online}
                        The second row changes while {device.name || "the device"} is online.
                      {/if}
                    </small>
                  </div>
                {/if}

                {#if editing === device.deviceId}
                  <div class="rename">
                    <input
                      type="text"
                      aria-label="New device name"
                      bind:value={draftName}
                      maxlength="128"
                      onkeydown={(event) => {
                        if (event.key === "Enter") void rename(device);
                        if (event.key === "Escape") cancelRename();
                      }}
                    />
                    <button
                      class="button primary small"
                      type="button"
                      onclick={() => void rename(device)}
                      disabled={busy}
                    >
                      Save
                    </button>
                    <button class="button ghost small" type="button" onclick={cancelRename} disabled={busy}>
                      Cancel
                    </button>
                  </div>
                {:else if revoking === device.deviceId}
                  <div class="revoke" role="alertdialog" aria-label="Revoke the device">
                    <p class="muted">
                      Revoke {device.name || "the device"}? Every Personal device of the group stops
                      trusting it, and it cannot reconnect until it is paired again.
                    </p>
                    <div class="toolbar">
                      <button
                        class="button danger small"
                        type="button"
                        onclick={() => void revoke(device)}
                        disabled={busy}
                      >
                        Revoke device
                      </button>
                      <button
                        class="button ghost small"
                        type="button"
                        onclick={() => (revoking = null)}
                        disabled={busy}
                      >
                        Cancel
                      </button>
                    </div>
                  </div>
                {:else}
                  <div class="toolbar">
                    <button class="button small" type="button" onclick={() => beginRename(device)}>
                      Rename
                    </button>
                    <button
                      class="button danger small"
                      type="button"
                      onclick={() => {
                        revoking = device.deviceId;
                        editing = null;
                        notice = null;
                      }}
                    >
                      Revoke
                    </button>
                  </div>
                {/if}
              </li>
            {/each}
          </ul>
        {/if}
      </section>
    {/each}
  </div>
{/if}

<style>
  .devices {
    display: flex;
    flex-direction: column;
    gap: 1.25rem;
  }

  .state {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 0.75rem;
  }

  .self {
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    gap: 0.5rem;
    padding-bottom: 0.75rem;
    border-bottom: 1px solid var(--border);
    font-size: 0.875rem;
  }

  .group {
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
  }

  h3 {
    display: flex;
    align-items: center;
    gap: 0.4rem;
    margin: 0;
    font-size: 0.9375rem;
    font-weight: 600;
  }

  .count {
    padding: 0.05rem 0.4rem;
    border-radius: 999px;
    color: var(--muted);
    background-color: var(--surface-soft);
    font-size: 0.75rem;
    font-weight: 500;
  }

  ul {
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
    margin: 0;
    padding: 0;
    list-style: none;
  }

  .device {
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
    padding: 0.75rem;
    border: 1px solid var(--border);
    border-radius: 10px;
  }

  .head {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: 0.5rem;
  }

  .identity {
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    gap: 0.5rem;
  }

  .badges {
    display: flex;
    flex-wrap: wrap;
    gap: 0.35rem;
  }

  .meta {
    margin: 0;
  }

  .confirm {
    display: grid;
    grid-template-columns: minmax(0, 1fr) 5rem 5rem;
    align-items: center;
    row-gap: 0.35rem;
    padding: 0.6rem 0.75rem;
    border-radius: 8px;
    background-color: var(--surface-soft);
    font-size: 0.8125rem;
  }

  .confirm-title {
    font-weight: 600;
  }

  .confirm-head {
    color: var(--muted);
    text-align: center;
  }

  .confirm input {
    justify-self: center;
    accent-color: var(--accent);
  }

  .confirm input:disabled {
    cursor: not-allowed;
  }

  .confirm-hint {
    grid-column: 1 / -1;
  }

  .rename {
    display: flex;
    flex-wrap: wrap;
    gap: 0.5rem;
  }

  .rename input {
    flex: 1 1 180px;
  }

  .revoke p {
    margin: 0 0 0.5rem;
  }
</style>
