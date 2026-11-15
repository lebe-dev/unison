<script lang="ts">
  import { onMount } from "svelte";
  import {
    TransferDirection,
    TransferState,
    type Transfer,
  } from "../api/gen/unison/local/v1/local_pb";
  import { daemon } from "../daemon.svelte";
  import { pickFiles } from "../files";
  import {
    formatBytes,
    formatDateTime,
    progressPercent,
    transferDirectionLabel,
    transferFilesText,
    transferStateLabel,
  } from "../format";
  import { isActiveTransfer, transfers } from "../transfers.svelte";

  let picked = $state<string[]>([]);
  let targets = $state<string[]>([]);
  let busy = $state(false);
  let picking = $state(false);

  const online = $derived(daemon.devices.filter((device) => device.online && !device.revoked));
  const active = $derived(transfers.active);
  const finished = $derived(transfers.finished);

  onMount(() => {
    void transfers.refresh();
    const timer = setInterval(() => void transfers.refresh(), 10_000);
    return () => clearInterval(timer);
  });

  async function choose(): Promise<void> {
    picking = true;
    try {
      picked = await pickFiles();
      targets = online.map((device) => device.deviceId);
    } catch (error) {
      transfers.notice = {
        kind: "error",
        text: error instanceof Error ? error.message : String(error),
      };
    } finally {
      picking = false;
    }
  }

  async function send(): Promise<void> {
    busy = true;
    try {
      await transfers.send(picked, targets);
      picked = [];
      targets = [];
    } finally {
      busy = false;
    }
  }

  function toggleTarget(id: string): void {
    targets = targets.includes(id)
      ? targets.filter((candidate) => candidate !== id)
      : [...targets, id];
  }

  function cancellable(transfer: Transfer): boolean {
    return isActiveTransfer(transfer) && transfer.state !== TransferState.PENDING;
  }

  function progressText(transfer: Transfer): string {
    const total = Number(transfer.totalBytes);
    if (total <= 0) {
      return formatBytes(transfer.bytes);
    }
    return `${formatBytes(transfer.bytes)} of ${formatBytes(total)}`;
  }

  function measured(transfer: Transfer): boolean {
    return transfer.state === TransferState.TRANSFERRING && Number(transfer.totalBytes) > 0;
  }
</script>

{#if !daemon.connected}
  <section class="state">
    {#if daemon.loading}
      <p class="muted">Connecting to the daemon...</p>
    {:else}
      <p class="notice error">The daemon is not running, so the transfers are unavailable.</p>
      <button class="button" type="button" onclick={() => void daemon.refresh()}>Try again</button>
    {/if}
  </section>
{:else}
  <div class="transfers">
    <section class="panel send">
      <div class="panel-header">
        <h3>Send files</h3>
        <button class="button small" type="button" onclick={() => void choose()} disabled={picking}>
          {picking ? "Picking..." : "Choose files"}
        </button>
      </div>

      {#if picked.length === 0}
        <p class="muted">
          {online.length === 0
            ? "No device is online right now."
            : "Pick files, then choose the devices that receive them."}
        </p>
      {:else}
        <p class="muted">{picked.join(", ")}</p>
        {#if online.length === 0}
          <p class="notice error">No device is online right now.</p>
        {:else}
          <div class="targets">
            {#each online as device (device.deviceId)}
              <label class="toggle">
                <input
                  type="checkbox"
                  checked={targets.includes(device.deviceId)}
                  onchange={() => toggleTarget(device.deviceId)}
                />
                <span>{device.name || "Unnamed device"}</span>
              </label>
            {/each}
          </div>
          <div class="toolbar">
            <button
              class="button primary small"
              type="button"
              onclick={() => void send()}
              disabled={busy || targets.length === 0}
            >
              Send
            </button>
            <button class="button ghost small" type="button" onclick={() => (picked = [])} disabled={busy}>
              Clear
            </button>
          </div>
        {/if}
      {/if}
    </section>

    {#if transfers.notice}
      <p
        class="notice"
        class:error={transfers.notice.kind === "error"}
        class:info={transfers.notice.kind === "info"}
      >
        {transfers.notice.text}
      </p>
    {/if}

    {#if transfers.error}
      <p class="notice error">{transfers.error}</p>
    {/if}

    <section class="group">
      <div class="panel-header">
        <h3>Active <span class="count">{active.length}</span></h3>
        <button
          class="button ghost small"
          type="button"
          onclick={() => void transfers.refresh()}
          disabled={transfers.loading}
        >
          Refresh
        </button>
      </div>
      {#if active.length === 0}
        <p class="empty">No transfer is running.</p>
      {:else}
        <ul>
          {#each active as transfer (transfer.id)}
            <li class="transfer">
              <div class="head">
                <div class="identity">
                  <strong>{transferFilesText(transfer)}</strong>
                  <span class="badge" class:incoming={transfer.direction === TransferDirection.INCOMING}>
                    {transferDirectionLabel(transfer.direction)}
                  </span>
                  <span class="badge">{transferStateLabel(transfer.state)}</span>
                </div>
                <span class="muted">{formatDateTime(transfer.createdAtUnixMs)}</span>
              </div>

              <p class="muted">
                {transfer.direction === TransferDirection.INCOMING ? "From" : "To"}
                {transfer.deviceName || "a device"} - {progressText(transfer)}
              </p>

              <div class="progress" class:measured={measured(transfer)}>
                <div
                  style:width={measured(transfer) ? `${progressPercent(transfer)}%` : "40%"}
                ></div>
              </div>

              {#if transfer.files.length > 1}
                <p class="files muted">
                  {#each transfer.files as file (file.blake3)}
                    <span>{file.name} ({formatBytes(file.size)})</span>
                  {/each}
                </p>
              {/if}

              {#if transfer.error}
                <p class="notice error">{transfer.error}</p>
              {/if}

              <div class="toolbar">
                {#if transfer.state === TransferState.PENDING}
                  <button
                    class="button primary small"
                    type="button"
                    onclick={() => void transfers.accept(transfer.id)}
                  >
                    Accept
                  </button>
                  <button
                    class="button danger small"
                    type="button"
                    onclick={() => void transfers.reject(transfer.id)}
                  >
                    Decline
                  </button>
                {:else if cancellable(transfer)}
                  <button
                    class="button small"
                    type="button"
                    onclick={() => void transfers.cancel(transfer.id)}
                  >
                    Cancel
                  </button>
                {/if}
              </div>
            </li>
          {/each}
        </ul>
      {/if}
    </section>

    <section class="group">
      <h3>Finished <span class="count">{finished.length}</span></h3>
      {#if finished.length === 0}
        <p class="empty">No finished transfer yet.</p>
      {:else}
        <ul>
          {#each finished as transfer (transfer.id)}
            <li class="transfer">
              <div class="head">
                <div class="identity">
                  <strong>{transferFilesText(transfer)}</strong>
                  <span class="badge">{transferStateLabel(transfer.state)}</span>
                </div>
                <span class="muted">{formatDateTime(transfer.createdAtUnixMs)}</span>
              </div>
              <p class="muted">
                {transfer.direction === TransferDirection.INCOMING ? "From" : "To"}
                {transfer.deviceName || "a device"} - {formatBytes(transfer.totalBytes)}
              </p>
              {#if transfer.paths.length > 0}
                <p class="files muted">
                  {#each transfer.paths as path (path)}
                    <span class="mono">{path}</span>
                  {/each}
                </p>
              {/if}
              {#if transfer.error}
                <p class="notice error">{transfer.error}</p>
              {/if}
            </li>
          {/each}
        </ul>
      {/if}
    </section>
  </div>
{/if}

<style>
  .transfers {
    display: flex;
    flex-direction: column;
    gap: 1rem;
  }

  .state {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 0.75rem;
  }

  .send {
    gap: 0.5rem;
  }

  .targets {
    display: flex;
    flex-wrap: wrap;
    gap: 0.5rem 1rem;
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

  .transfer {
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
    align-items: center;
    gap: 0.5rem;
  }

  .badge.incoming {
    color: var(--accent);
    background-color: var(--accent-soft);
  }

  .progress {
    height: 6px;
    border-radius: 999px;
    background-color: var(--surface-soft);
    overflow: hidden;
  }

  .progress div {
    height: 100%;
    border-radius: 999px;
    background-color: var(--accent);
    transition: width 0.2s ease;
  }

  .progress:not(.measured) div {
    animation: slide 1.2s ease-in-out infinite alternate;
  }

  .files {
    display: flex;
    flex-wrap: wrap;
    gap: 0.25rem 0.75rem;
    word-break: break-all;
  }

  @keyframes slide {
    from {
      transform: translateX(-20%);
    }
    to {
      transform: translateX(160%);
    }
  }
</style>
