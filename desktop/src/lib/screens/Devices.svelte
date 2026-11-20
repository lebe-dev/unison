<script lang="ts">
  import { Platform, Role, type Device } from "../api/gen/unison/local/v1/local_pb";
  import Icon, { type IconName } from "../Icon.svelte";
  import { client } from "../api/client";
  import { confirmation, type Scope, type Side } from "../confirmation.svelte";
  import { daemon } from "../daemon.svelte";
  import { errorText } from "../errors";
  import { t } from "../i18n";
  import NetworkIssue from "../NetworkIssue.svelte";
  import Spinner from "../Spinner.svelte";
  import { deviceNameKey } from "../platform";
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
      titleKey: "devices.personal" as const,
      devices: daemon.devices.filter((device) => device.role === Role.PERSONAL),
    },
    {
      role: Role.FRIENDS,
      titleKey: "devices.friends" as const,
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
      case Platform.LINUX:
        return "laptop";
      default:
        return "laptop";
    }
  }

  function statusText(device: Device): string {
    if (device.online) {
      return t("common.online");
    }
    return device.unreachable ? t("common.notReachable") : t("common.offline");
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
      notice = { kind: "error", text: t("devices.renameEmpty") };
      return;
    }
    busy = true;
    notice = null;
    try {
      await client.renameDevice({ deviceId: device.deviceId, name });
      editing = null;
      await daemon.refresh();
      notice = { kind: "info", text: t("devices.renamed", { name }) };
    } catch (error) {
      notice = {
        kind: "error",
        text: errorText(error, t("devices.renameFailed")),
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
    const name = device.name || t("common.theDevice");
    const fromName = device.name || t("common.theDeviceGenitive");
    if (side === "local") {
      return t(scope === "clipboard" ? "devices.flagLocalClipboard" : "devices.flagLocalFiles", {
        name: fromName,
      });
    }
    return t(scope === "clipboard" ? "devices.flagPeerClipboard" : "devices.flagPeerFiles", {
      name,
    });
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

  /** remoteLabel names the permission switch for a screen reader. */
  function remoteLabel(device: Device): string {
    return t("devices.allowRemote", { name: device.name || t("common.theDevice") });
  }

  async function setRemoteAllowed(device: Device, allow: boolean): Promise<void> {
    notice = null;
    const failure = await confirmation.setRemoteAllowed(device.deviceId, allow);
    if (!failure) {
      return;
    }
    notice = { kind: "error", text: failure };
  }

  /** reconnect asks the daemon to dial an offline device again. */
  async function reconnect(device: Device): Promise<void> {
    busy = true;
    notice = null;
    try {
      await client.retryDevice({ deviceId: device.deviceId });
      notice = {
        kind: "info",
        text: t("devices.reconnecting", { name: device.name || t("common.theDeviceGenitive") }),
      };
    } catch (error) {
      notice = { kind: "error", text: errorText(error, t("devices.reconnectFailed")) };
    } finally {
      busy = false;
    }
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
        text: t("devices.revoked", {
          name: device.name || t("common.theDeviceGenitive"),
        }),
      };
    } catch (error) {
      notice = { kind: "error", text: errorText(error, t("devices.revokeFailed")) };
    } finally {
      busy = false;
    }
  }
</script>

{#if !daemon.connected}
  <section class="state">
    {#if daemon.loading}
      <Spinner />
      <p class="muted">{t("devices.connecting")}</p>
    {:else}
      <p class="notice error">{t("devices.daemonNotRunning")}</p>
      <button class="button" type="button" onclick={() => void daemon.retry()}>
        {t("common.tryAgain")}
      </button>
    {/if}
  </section>
{:else}
  <div class="devices">
    <section class="column" aria-label={t("devices.deviceListAria")}>
      <div class="column-head">
        <span class="column-title">{t("devices.pairedDevices")}</span>
        {#if onAdd}
          <button
            class="icon-button"
            type="button"
            aria-label={t("devices.addDeviceAria")}
            title={t("devices.addDeviceAria")}
            onclick={onAdd}
          >
            <Icon name="plus" stroke={1.6} />
          </button>
        {/if}
      </div>

      <div class="scroll">
        <h3 class="group-title">{t(deviceNameKey())}</h3>
        <div class="entry self">
          <span class="avatar"><Icon name="laptop" size={20} stroke={1.2} /></span>
          <span class="text">
            <strong>{daemon.deviceName}</strong>
            <span class="mono sub">{shortDeviceID(daemon.deviceID)}</span>
          </span>
        </div>

        {#each groups as group (group.role)}
          <h3 class="group-title">
            {t(group.titleKey)} <span class="count">{group.devices.length}</span>
          </h3>
          {#if group.devices.length === 0}
            <p class="placeholder">
              {group.role === Role.PERSONAL ? t("devices.noPersonal") : t("devices.noFriends")}
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
                      <strong>{device.name || t("common.unnamedDevice")}</strong>
                      <span class="sub">
                        {platformLabel(
                          device.platform,
                        )}{#if device.online && pathLabel(device.path)}
                          · {pathLabel(
                            device.path,
                          )}{:else if !device.online && device.lastSeenAtUnixMs}
                          · {t("devices.seen", {
                            time: formatTime(device.lastSeenAtUnixMs),
                          })}{/if}
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

    <section class="detail" aria-label={t("devices.deviceDetailsAria")}>
      {#if notice}
        <p class="notice" class:error={notice.kind === "error"} class:info={notice.kind === "info"}>
          {notice.text}
        </p>
      {/if}

      {#if !selected}
        <div class="state">
          <span class="hero-icon"><Icon name="devices" size={34} stroke={1} /></span>
          <p>{t("devices.noPaired")}</p>
          <p class="muted">{t("devices.pairHint")}</p>
          {#if onAdd}
            <button class="button primary large" type="button" onclick={onAdd}>
              {t("common.addDevice")}
            </button>
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
                  aria-label={t("devices.newDeviceNameAria")}
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
                  {t("common.save")}
                </button>
                <button class="button" type="button" onclick={cancelRename} disabled={busy}>
                  {t("common.cancel")}
                </button>
              </div>
            {:else}
              <h2>{device.name || t("common.unnamedDevice")}</h2>
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
          <div class="section" role="group" aria-label={t("devices.askBeforeTaking")}>
            <h3 class="section-title">{t("devices.askBeforeTaking")}</h3>
            <div class="list">
              {#each ["local", "peer"] as const as side (side)}
                {@const name = device.name || t("common.theDevice")}
                {@const fromName = device.name || t("common.theDeviceGenitive")}
                {@const onName = device.name || t("common.theDevicePrepositional")}
                <div class="form-row">
                  <span class="label">
                    {side === "local"
                      ? t("devices.onThisDeviceFrom", { name: fromName })
                      : t("devices.onDeviceFromThisDevice", { name: onName })}
                    {#if side === "peer" && !device.online}
                      <small>{t("devices.changesWhileOnline", { name })}</small>
                    {/if}
                  </span>
                  {#each flags.filter((flag) => flag.side === side) as flag (`${flag.side}:${flag.scope}`)}
                    <label class="switch-cell">
                      <span>
                        {flag.scope === "clipboard"
                          ? t("common.clipboard")
                          : t("format.kind.files")}
                      </span>
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
              <div class="form-row">
                <span class="label">
                  {remoteLabel(device)}
                  <small>{t("devices.allowRemoteHint")}</small>
                </span>
                <input
                  class="switch"
                  type="checkbox"
                  aria-label={remoteLabel(device)}
                  checked={confirmation.remoteAllowed(device)}
                  disabled={confirmation.isRemoteBusy(device.deviceId)}
                  onchange={(event) => void setRemoteAllowed(device, event.currentTarget.checked)}
                />
              </div>
            </div>
            <p class="section-footer">{t("devices.promptFooter")}</p>
          </div>
        {/if}

        <div class="section">
          <h3 class="section-title">{t("common.details")}</h3>
          <dl class="list">
            <div class="form-row">
              <dt class="label">{t("devices.role")}</dt>
              <dd>{roleLabel(device.role)}</dd>
            </div>
            <div class="form-row">
              <dt class="label">{t("devices.system")}</dt>
              <dd>{platformLabel(device.platform)}</dd>
            </div>
            <div class="form-row">
              <dt class="label">{t("devices.deviceID")}</dt>
              <dd class="mono">{shortDeviceID(device.deviceId)}</dd>
            </div>
            {#if device.addedAtUnixMs}
              <div class="form-row">
                <dt class="label">{t("devices.added")}</dt>
                <dd>{formatDateTime(device.addedAtUnixMs)}</dd>
              </div>
            {/if}
            {#if !device.online && device.lastSeenAtUnixMs}
              <div class="form-row">
                <dt class="label">{t("devices.lastSeen")}</dt>
                <dd>{formatTime(device.lastSeenAtUnixMs)}</dd>
              </div>
            {/if}
          </dl>
        </div>

        {#if revoking === device.deviceId}
          <div class="panel revoke" role="alertdialog" aria-label={t("devices.revokeDialogAria")}>
            <p>
              {t("devices.revokeQuestion", {
                name: device.name || t("common.theDeviceLower"),
              })}
            </p>
            <div class="toolbar end">
              <button
                class="button"
                type="button"
                onclick={() => (revoking = null)}
                disabled={busy}
              >
                {t("common.cancel")}
              </button>
              <button
                class="button danger"
                type="button"
                onclick={() => void revoke(device)}
                disabled={busy}
              >
                {t("devices.revokeDevice")}
              </button>
            </div>
          </div>
        {:else if editing !== device.deviceId}
          <div class="toolbar">
            {#if !device.online}
              <button
                class="button"
                type="button"
                onclick={() => void reconnect(device)}
                disabled={busy}
              >
                {t("devices.reconnect")}
              </button>
            {/if}
            <button class="button" type="button" onclick={() => beginRename(device)}>
              {t("devices.rename")}
            </button>
            <button
              class="button danger"
              type="button"
              onclick={() => {
                revoking = device.deviceId;
                editing = null;
                notice = null;
              }}
            >
              {t("devices.revoke")}
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
