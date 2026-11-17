<script lang="ts">
  import { Platform, Role, type Device } from "../api/gen/unison/local/v1/local_pb";
  import Icon, { type IconName } from "../Icon.svelte";
  import { client } from "../api/client";
  import { confirmation, type Scope, type Side } from "../confirmation.svelte";
  import { daemon } from "../daemon.svelte";
  import { errorText } from "../errors";
  import NetworkIssue from "../NetworkIssue.svelte";
  import { unreachableDeviceIssue } from "../network";
  import {
    formatDateTime,
    formatTime,
    pathLabel,
    platformLabel,
    roleLabel,
    shortDeviceID,
  } from "../format";

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

  let chosen = $state<string | null>(null);
  // The first device is shown until the user picks another one, and a device
  // that disappears hands the detail over to the first one again.
  const selected = $derived(
    daemon.devices.find((device) => device.deviceId === chosen) ??
      groups.flatMap((group) => group.devices)[0],
  );

  /** platformIcon picks the symbol of the kind of computer or phone. */
  function platformIcon(device: Device): IconName {
    if (device.role === Role.FRIENDS) {
      return "person";
    }
    switch (device.platform) {
      case Platform.ANDROID:
        return "phone";
      case Platform.WINDOWS:
        return "desktop";
      default:
        return "laptop";
    }
  }

  function statusText(device: Device): string {
    if (device.online) {
      return "Online";
    }
    return device.unreachable ? "Not reachable" : "Offline";
  }

  function select(device: Device): void {
    chosen = device.deviceId;
    editing = null;
    revoking = null;
    notice = null;
  }

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

  async function setConfirmation(
    device: Device,
    scope: Scope,
    side: Side,
    require: boolean,
  ): Promise<void> {
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
    <section class="column" aria-label="Device list">
      <div class="column-head">
        <span class="column-title">Paired devices</span>
        {#if onAdd}
          <button
            class="icon-button"
            type="button"
            aria-label="Add device"
            title="Add device"
            onclick={onAdd}
          >
            <Icon name="plus" stroke={1.6} />
          </button>
        {/if}
      </div>

      <div class="scroll">
        <h3 class="group-title">This Mac</h3>
        <div class="entry self">
          <span class="avatar"><Icon name="laptop" size={20} stroke={1.2} /></span>
          <span class="text">
            <strong>{daemon.deviceName}</strong>
            <span class="mono sub">{shortDeviceID(daemon.deviceID)}</span>
          </span>
        </div>

        {#each groups as group (group.role)}
          <h3 class="group-title">
            {group.title} <span class="count">{group.devices.length}</span>
          </h3>
          {#if group.devices.length === 0}
            <p class="placeholder">
              {group.role === Role.PERSONAL ? "No Personal devices yet." : "No Friends yet."}
            </p>
          {:else}
            <ul>
              {#each group.devices as device (device.deviceId)}
                <li>
                  <button
                    type="button"
                    class="entry"
                    class:selected={selected?.deviceId === device.deviceId}
                    aria-current={selected?.deviceId === device.deviceId ? "true" : undefined}
                    onclick={() => select(device)}
                  >
                    <span class="avatar" class:dim={!device.online}>
                      <Icon name={platformIcon(device)} size={20} stroke={1.2} />
                    </span>
                    <span class="text">
                      <strong>{device.name || "Unnamed device"}</strong>
                      <span class="sub">
                        {platformLabel(
                          device.platform,
                        )}{#if device.online && pathLabel(device.path)}
                          · {pathLabel(
                            device.path,
                          )}{:else if !device.online && device.lastSeenAtUnixMs}
                          · seen {formatTime(device.lastSeenAtUnixMs)}{/if}
                      </span>
                    </span>
                    <span
                      class="status"
                      class:online={device.online}
                      class:unreachable={device.unreachable && !device.online}
                    >
                      {statusText(device)}
                    </span>
                  </button>
                </li>
              {/each}
            </ul>
          {/if}
        {/each}
      </div>
    </section>

    <section class="detail" aria-label="Device details">
      {#if notice}
        <p class="notice" class:error={notice.kind === "error"} class:info={notice.kind === "info"}>
          {notice.text}
        </p>
      {/if}

      {#if !selected}
        <div class="state">
          <span class="hero-icon"><Icon name="devices" size={34} stroke={1} /></span>
          <p>No paired devices yet.</p>
          <p class="muted">Pair a phone or another computer to share the clipboard and files.</p>
          {#if onAdd}
            <button class="button primary large" type="button" onclick={onAdd}>Add device</button>
          {/if}
        </div>
      {:else}
        {@const device = selected}
        <header class="hero">
          <span class="hero-icon"><Icon name={platformIcon(device)} size={34} stroke={0.9} /></span>
          <div class="hero-text">
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
                  class="button primary"
                  type="button"
                  onclick={() => void rename(device)}
                  disabled={busy}
                >
                  Save
                </button>
                <button class="button" type="button" onclick={cancelRename} disabled={busy}>
                  Cancel
                </button>
              </div>
            {:else}
              <h2>{device.name || "Unnamed device"}</h2>
            {/if}
            <span class="hero-status">
              <span
                class="dot"
                class:online={device.online}
                class:error={device.unreachable && !device.online}
                class:hollow={!device.online && !device.unreachable}
              ></span>
              {statusText(device)}{#if device.online && pathLabel(device.path)}
                · {pathLabel(device.path)}{/if}
            </span>
          </div>
        </header>

        {#if device.unreachable && !device.online}
          {@const issue = unreachableDeviceIssue(device)}
          {#if issue}
            <NetworkIssue summary={issue.summary} hint={issue.hint} details={issue.details} />
          {/if}
        {/if}

        {#if device.role === Role.PERSONAL}
          <div class="section" role="group" aria-label="Ask before taking">
            <h3 class="section-title">Ask before taking</h3>
            <div class="list">
              {#each ["local", "peer"] as const as side (side)}
                <div class="form-row">
                  <span class="label">
                    {side === "local"
                      ? `On this Mac, from ${device.name || "the device"}`
                      : `On ${device.name || "the device"}, from this Mac`}
                    {#if side === "peer" && !device.online}
                      <small>Changes while {device.name || "the device"} is online.</small>
                    {/if}
                  </span>
                  {#each flags.filter((flag) => flag.side === side) as flag (`${flag.side}:${flag.scope}`)}
                    <label class="switch-cell">
                      <span>{flag.scope === "clipboard" ? "Clipboard" : "Files"}</span>
                      <input
                        class="switch"
                        type="checkbox"
                        aria-label={flagLabel(device, flag.side, flag.scope)}
                        title={flagLabel(device, flag.side, flag.scope)}
                        checked={confirmation.require(device, flag.scope, flag.side)}
                        disabled={confirmation.isBusy(device.deviceId, flag.scope, flag.side) ||
                          (flag.side === "peer" && !device.online)}
                        onchange={(event) =>
                          void setConfirmation(
                            device,
                            flag.scope,
                            flag.side,
                            event.currentTarget.checked,
                          )}
                      />
                    </label>
                  {/each}
                </div>
              {/each}
            </div>
            <p class="section-footer">What waits for an answer shows up in a prompt.</p>
          </div>
        {/if}

        <div class="section">
          <h3 class="section-title">Details</h3>
          <dl class="list">
            <div class="form-row">
              <dt class="label">Role</dt>
              <dd>{roleLabel(device.role)}</dd>
            </div>
            <div class="form-row">
              <dt class="label">System</dt>
              <dd>{platformLabel(device.platform)}</dd>
            </div>
            <div class="form-row">
              <dt class="label">Device ID</dt>
              <dd class="mono">{shortDeviceID(device.deviceId)}</dd>
            </div>
            {#if device.addedAtUnixMs}
              <div class="form-row">
                <dt class="label">Added</dt>
                <dd>{formatDateTime(device.addedAtUnixMs)}</dd>
              </div>
            {/if}
            {#if !device.online && device.lastSeenAtUnixMs}
              <div class="form-row">
                <dt class="label">Last seen</dt>
                <dd>{formatTime(device.lastSeenAtUnixMs)}</dd>
              </div>
            {/if}
          </dl>
        </div>

        {#if revoking === device.deviceId}
          <div class="panel revoke" role="alertdialog" aria-label="Revoke the device">
            <p>
              Revoke {device.name || "the device"}? Every Personal device of the group stops
              trusting it, and it cannot reconnect until it is paired again.
            </p>
            <div class="toolbar end">
              <button
                class="button"
                type="button"
                onclick={() => (revoking = null)}
                disabled={busy}
              >
                Cancel
              </button>
              <button
                class="button danger"
                type="button"
                onclick={() => void revoke(device)}
                disabled={busy}
              >
                Revoke device
              </button>
            </div>
          </div>
        {:else if editing !== device.deviceId}
          <div class="toolbar">
            <button class="button" type="button" onclick={() => beginRename(device)}>Rename</button>
            <button
              class="button danger"
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
      {/if}
    </section>
  </div>
{/if}

<style>
  .devices {
    display: flex;
    flex: 1;
    min-height: 0;
  }

  .column {
    display: flex;
    flex: 0 0 300px;
    flex-direction: column;
    min-height: 0;
    border-right: 1px solid var(--separator);
    background-color: var(--surface);
  }

  .column-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    height: 40px;
    padding: 0 8px 0 16px;
    border-bottom: 1px solid var(--separator);
  }

  .column-title {
    font-size: 12px;
    font-weight: 600;
    color: var(--muted);
  }

  .scroll {
    flex: 1;
    padding: 4px 8px 12px;
    overflow-y: auto;
  }

  .group-title {
    margin: 12px 0 4px;
    padding: 0 8px;
    font-size: 11px;
    font-weight: 600;
    color: var(--muted);
  }

  .group-title .count {
    font-size: 11px;
  }

  ul {
    display: flex;
    flex-direction: column;
    gap: 2px;
    margin: 0;
    padding: 0;
    list-style: none;
  }

  .placeholder {
    padding: 6px 8px;
    font-size: 12px;
    color: var(--muted);
  }

  .entry {
    display: flex;
    align-items: center;
    gap: 10px;
    width: 100%;
    padding: 7px 8px;
    border: 0;
    border-radius: 8px;
    font: inherit;
    color: inherit;
    text-align: left;
    background: transparent;
    cursor: default;
  }

  button.entry:hover {
    background-color: var(--hover);
  }

  .entry.selected,
  .entry.selected:hover {
    background-color: var(--row-selected);
  }

  .entry:focus-visible {
    outline: 3px solid color-mix(in srgb, var(--accent) 45%, transparent);
    outline-offset: -2px;
  }

  .avatar {
    display: flex;
    flex: none;
    align-items: center;
    justify-content: center;
    width: 34px;
    height: 34px;
    border-radius: 9px;
    color: var(--secondary);
    background-color: var(--surface-soft);
  }

  .avatar.dim {
    color: var(--muted);
  }

  .text {
    display: flex;
    flex: 1;
    flex-direction: column;
    min-width: 0;
  }

  .text strong {
    overflow: hidden;
    font-weight: 600;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .sub {
    overflow: hidden;
    font-size: 11px;
    color: var(--muted);
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .status {
    flex: none;
    font-size: 11px;
    color: var(--muted);
  }

  .status.online {
    color: var(--online-text);
    font-weight: 500;
  }

  .status.unreachable {
    color: var(--danger);
    font-weight: 500;
  }

  .detail {
    display: flex;
    flex: 1;
    flex-direction: column;
    gap: 20px;
    min-width: 0;
    padding: 24px 28px 28px;
    overflow-y: auto;
  }

  .hero {
    display: flex;
    align-items: center;
    gap: 16px;
  }

  .hero-icon {
    display: flex;
    flex: none;
    align-items: center;
    justify-content: center;
    width: 60px;
    height: 60px;
    border-radius: 15px;
    color: var(--secondary);
    background-color: var(--surface);
    box-shadow:
      var(--box-shadow),
      0 1px 3px rgba(0, 0, 0, 0.08);
  }

  .hero-text {
    display: flex;
    flex: 1;
    flex-direction: column;
    gap: 3px;
    min-width: 0;
  }

  .hero-text h2 {
    font-size: 22px;
    font-weight: 700;
    letter-spacing: -0.01em;
  }

  .hero-status {
    display: flex;
    align-items: center;
    gap: 6px;
    color: var(--secondary);
  }

  .rename {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
  }

  .rename input {
    flex: 1 1 180px;
  }

  .switch-cell {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    font-size: 12px;
    color: var(--secondary);
  }

  .switch-cell + .switch-cell {
    margin-left: 10px;
  }

  dl {
    margin: 0;
  }

  dd {
    margin: 0;
    color: var(--secondary);
  }

  .toolbar.end {
    justify-content: flex-end;
  }

  .revoke p {
    margin: 0;
  }
</style>
