<script lang="ts">
  import { Role, type Device } from "../api/gen/unison/local/v1/local_pb";
  import { client } from "../api/client";
  import { confirmation } from "../confirmation.svelte";
  import { daemon } from "../daemon.svelte";
  import { errorText, isUnimplemented } from "../errors";
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
        text: isUnimplemented(error)
          ? "The daemon cannot rename devices yet."
          : errorText(error, "The device could not be renamed."),
      };
    } finally {
      busy = false;
    }
  }

  async function setConfirmation(device: Device, require: boolean): Promise<void> {
    notice = null;
    const failure = await confirmation.set(device.deviceId, require);
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
                  <label class="toggle">
                    <input
                      type="checkbox"
                      checked={confirmation.require(device)}
                      disabled={confirmation.busy.includes(device.deviceId)}
                      onchange={(event) => void setConfirmation(device, event.currentTarget.checked)}
                    />
                    <span>
                      Ask before applying items from this device
                      <small>
                        Its items wait here until you apply them. The device is asked to do the
                        same with the items it receives.
                      </small>
                    </span>
                  </label>
                {/if}

                {#if device.peerRequireConfirmation}
                  <p class="meta muted">
                    {device.name || "The device"} asks for confirmation before applying what it
                    receives.
                  </p>
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
