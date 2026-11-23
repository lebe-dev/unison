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
  import { t } from "../i18n";
  import { isActiveTransfer, transfers } from "../transfers.svelte";
  import Spinner from "../Spinner.svelte";

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
      transfers.offer(await pickFiles());
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
      await transfers.sendDraft();
    } finally {
      busy = false;
    }
  }

  function cancellable(transfer: Transfer): boolean {
    return isActiveTransfer(transfer) && transfer.state !== TransferState.PENDING;
  }

  function progressText(transfer: Transfer): string {
    const total = Number(transfer.totalBytes);
    if (total <= 0) {
      return formatBytes(transfer.bytes);
    }
    return t("transfers.of", {
      done: formatBytes(transfer.bytes),
      total: formatBytes(total),
    });
  }

  function measured(transfer: Transfer): boolean {
    return transfer.state === TransferState.TRANSFERRING && Number(transfer.totalBytes) > 0;
  }
</script>

{#if !daemon.connected}
  <section class="state">
    {#if daemon.loading}
      <Spinner />
      <p class="muted">{t("devices.connecting")}</p>
    {:else}
      <p class="notice error">{t("transfers.daemonNotRunning")}</p>
      <button class="button" type="button" onclick={() => void daemon.retry()}>
        {t("common.tryAgain")}
      </button>
    {/if}
  </section>
{:else}
  <div class="transfers">
    <section class="panel send">
      <div class="panel-header">
        <h3>{t("transfers.sendFiles")}</h3>
        <button class="button" type="button" onclick={() => void choose()} disabled={picking}>
          {picking ? t("transfers.picking") : t("transfers.chooseFiles")}
        </button>
      </div>

      {#if transfers.picked.length === 0}
        <p class="muted">
          {online.length === 0 ? t("transfers.noDeviceOnline") : t("transfers.pickHint")}
        </p>
      {:else}
        <p class="muted">{transfers.picked.join(", ")}</p>
        {#if online.length === 0}
          <p class="notice error">{t("transfers.noDeviceOnline")}</p>
        {:else}
          <div class="targets">
            {#each online as device (device.deviceId)}
              <label class="check">
                <input
                  type="checkbox"
                  checked={transfers.targets.includes(device.deviceId)}
                  onchange={() => transfers.toggleTarget(device.deviceId)}
                />
                <span>{device.name || t("common.unnamedDevice")}</span>
              </label>
            {/each}
          </div>
          <div class="toolbar">
            <button
              class="button primary small"
              type="button"
              onclick={() => void send()}
              disabled={busy || transfers.targets.length === 0}
            >
              {t("transfers.send")}
            </button>
            <button
              class="button ghost small"
              type="button"
              onclick={() => transfers.clearDraft()}
              disabled={busy}
            >
              {t("common.clear")}
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

    <section class="section">
      <div class="section-title">
        <h3 class="grow">
          {t("transfers.inProgress")} <span class="count">{active.length}</span>
        </h3>
        <button
          class="button ghost small"
          type="button"
          onclick={() => void transfers.refresh()}
          disabled={transfers.loading}
        >
          {t("common.refresh")}
        </button>
      </div>
      {#if active.length === 0}
        <p class="empty">{t("transfers.noRunning")}</p>
      {:else}
        <ul class="list">
          {#each active as transfer (transfer.id)}
            <li class="transfer">
              <div class="head">
                <div class="identity">
                  <strong>{transferFilesText(transfer)}</strong>
                  <span
                    class="badge"
                    class:incoming={transfer.direction === TransferDirection.INCOMING}
                  >
                    {transferDirectionLabel(transfer.direction)}
                  </span>
                  <span class="badge">{transferStateLabel(transfer.state)}</span>
                </div>
                <span class="muted">{formatDateTime(transfer.createdAtUnixMs)}</span>
              </div>

              <p class="muted">
                {transfer.direction === TransferDirection.INCOMING
                  ? t("transfers.from")
                  : t("transfers.to")}
                {transfer.deviceName || t("common.aDeviceLower")} - {progressText(transfer)}
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
                    {t("common.accept")}
                  </button>
                  <button
                    class="button danger small"
                    type="button"
                    onclick={() => void transfers.reject(transfer.id)}
                  >
                    {t("common.decline")}
                  </button>
                {:else if cancellable(transfer)}
                  <button
                    class="button small"
                    type="button"
                    onclick={() => void transfers.cancel(transfer.id)}
                  >
                    {t("common.cancel")}
                  </button>
                {/if}
              </div>
            </li>
          {/each}
        </ul>
      {/if}
    </section>

    <section class="section">
      <h3 class="section-title">
        {t("transfers.finished")} <span class="count">{finished.length}</span>
      </h3>
      {#if finished.length === 0}
        <p class="empty">{t("transfers.noFinished")}</p>
      {:else}
        <ul class="list">
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
                {transfer.direction === TransferDirection.INCOMING
                  ? t("transfers.from")
                  : t("transfers.to")}
                {transfer.deviceName || t("common.aDeviceLower")} -
                {formatBytes(transfer.totalBytes)}
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
    gap: 22px;
  }

  .targets {
    display: flex;
    flex-wrap: wrap;
    gap: 6px 16px;
  }

  h3 {
    font-size: 13px;
    font-weight: 600;
  }

  .head {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
  }

  .identity {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 6px;
    min-width: 0;
  }

  .identity strong {
    font-weight: 500;
    overflow-wrap: anywhere;
  }

  .badge.incoming {
    color: var(--accent);
    background-color: var(--accent-soft);
  }

  .progress {
    height: 4px;
    border-radius: 2px;
    background-color: var(--fill);
    overflow: hidden;
  }

  .progress div {
    height: 100%;
    border-radius: 2px;
    background-color: var(--accent);
    transition: width 0.2s ease;
  }

  .progress:not(.measured) div {
    animation: slide 1.2s ease-in-out infinite alternate;
  }

  .files {
    display: flex;
    flex-wrap: wrap;
    gap: 2px 12px;
    word-break: break-all;
  }

  .toolbar:empty {
    display: none;
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
