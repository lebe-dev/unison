<script lang="ts">
  import { onMount } from "svelte";
  import { Kind, type HistoryItem } from "../api/gen/unison/local/v1/local_pb";
  import { daemon } from "../daemon.svelte";
  import {
    fileNames,
    formatBytes,
    formatDateTime,
    itemSize,
    kindLabel,
    shortDeviceID,
    textSnippet,
  } from "../format";
  import { history } from "../history.svelte";
  import { t } from "../i18n";
  import ImageCanvas from "../ImageCanvas.svelte";

  /** Height of one row of the feed, in pixels. */
  const ROW_HEIGHT = 88;

  /** Rows rendered outside the visible area. */
  const OVERSCAN = 4;

  /** Longer side of the pixels of a row thumbnail. */
  const THUMB_SIDE = 128;

  /** Longer side of the pixels of the full preview. */
  const PREVIEW_SIDE = 1024;

  const kinds = [
    { value: Kind.UNSPECIFIED, labelKey: "history.allTypes" },
    { value: Kind.TEXT, labelKey: "format.kind.text" },
    { value: Kind.IMAGE, labelKey: "format.kind.image" },
    { value: Kind.FILES, labelKey: "format.kind.files" },
  ] as const;

  let viewportHeight = $state(360);
  let scrollTop = $state(0);
  let feed: HTMLDivElement | null = $state(null);
  let deleting = $state<string | null>(null);
  let preview = $state<{ title: string; id: string } | null>(null);
  let startedAt = $state<Record<string, number>>({});
  let now = $state(Date.now());

  const origins = $derived([
    { value: "", label: t("history.allDevices") },
    { value: daemon.deviceID, label: t("common.thisDevice") },
    ...daemon.devices.map((device) => ({
      value: device.deviceId,
      label: device.name || shortDeviceID(device.deviceId),
    })),
  ]);

  const windowStart = $derived(
    Math.min(
      Math.max(0, Math.floor(scrollTop / ROW_HEIGHT) - OVERSCAN),
      Math.max(0, history.items.length - 1),
    ),
  );
  const windowEnd = $derived(
    Math.min(
      history.items.length,
      windowStart + Math.ceil(viewportHeight / ROW_HEIGHT) + OVERSCAN * 2,
    ),
  );
  const visible = $derived(history.items.slice(windowStart, windowEnd));
  const topPad = $derived(windowStart * ROW_HEIGHT);
  const bottomPad = $derived(Math.max(0, (history.items.length - windowEnd) * ROW_HEIGHT));

  onMount(() => {
    void history.load();
    void history.loadSettings();
    const timer = setInterval(() => void history.load(true), 10_000);
    return () => clearInterval(timer);
  });

  $effect(() => {
    // A new filter starts the feed from the newest item.
    void history.kind;
    void history.origin;
    scrollTop = 0;
    if (feed) {
      feed.scrollTop = 0;
    }
  });

  $effect(() => {
    if (history.downloading.length === 0) {
      return;
    }
    const timer = setInterval(() => (now = Date.now()), 1000);
    return () => clearInterval(timer);
  });

  function originName(origin: string): string {
    if (origin === daemon.deviceID) {
      return t("common.thisDevice");
    }
    const device = daemon.devices.find((candidate) => candidate.deviceId === origin);
    return device?.name || shortDeviceID(origin);
  }

  function description(item: HistoryItem): string {
    const info = item.item;
    if (!info) {
      return "";
    }
    if (info.kind === Kind.FILES) {
      return fileNames(info).join(", ");
    }
    if (info.kind === Kind.TEXT) {
      return textSnippet(info, 140);
    }
    return t("history.storedAs", {
      mime: info.representations[0]?.mimeType ?? t("history.anImage"),
    });
  }

  /** hasThumbnail reports an image whose payload is stored on this device, so
   * the daemon can decode it. */
  function hasThumbnail(item: HistoryItem): boolean {
    return item.item?.kind === Kind.IMAGE && item.complete;
  }

  function openPreview(item: HistoryItem): void {
    const info = item.item;
    if (!info || !hasThumbnail(item)) {
      return;
    }
    preview = { title: t("history.imageFrom", { name: originName(info.origin) }), id: info.id };
  }

  async function download(item: HistoryItem): Promise<void> {
    const id = item.item?.id;
    if (!id) {
      return;
    }
    startedAt = { ...startedAt, [id]: Date.now() };
    await history.download(item);
    const next = { ...startedAt };
    delete next[id];
    startedAt = next;
  }

  function elapsed(id: string): string {
    const start = startedAt[id];
    if (!start) {
      return "";
    }
    const seconds = Math.max(0, Math.round((now - start) / 1000));
    return t("history.elapsedSeconds", { seconds });
  }
</script>

{#if !daemon.connected}
  <section class="state">
    {#if daemon.loading}
      <p class="muted">{t("devices.connecting")}</p>
    {:else}
      <p class="notice error">{t("history.daemonNotRunning")}</p>
      <button class="button" type="button" onclick={() => void daemon.refresh()}>
        {t("common.tryAgain")}
      </button>
    {/if}
  </section>
{:else}
  <div class="history">
    <div class="toolbar">
      <label class="field">
        <span>{t("history.type")}</span>
        <select
          aria-label={t("history.type")}
          value={history.kind}
          onchange={(event) => void history.setKind(Number(event.currentTarget.value))}
        >
          {#each kinds as option (option.value)}
            <option value={option.value}>{t(option.labelKey)}</option>
          {/each}
        </select>
      </label>
      <label class="field">
        <span>{t("history.device")}</span>
        <select
          aria-label={t("history.device")}
          value={history.origin}
          onchange={(event) => void history.setOrigin(event.currentTarget.value)}
        >
          {#each origins as option (option.value)}
            <option value={option.value}>{option.label}</option>
          {/each}
        </select>
      </label>
      <span class="count muted">
        {t("history.itemsCount", { count: history.items.length })}
      </span>
      <button
        class="button small ghost"
        type="button"
        onclick={() => void history.load()}
        disabled={history.loading}
      >
        {t("common.refresh")}
      </button>
    </div>

    {#if history.notice}
      <p
        class="notice"
        class:error={history.notice.kind === "error"}
        class:info={history.notice.kind === "info"}
      >
        {history.notice.text}
      </p>
    {/if}

    {#if history.error}
      <p class="notice error">{history.error}</p>
    {/if}

    {#if history.items.length === 0}
      <p class="empty">
        {history.loading ? t("history.loading") : t("history.emptyFilter")}
      </p>
    {:else}
      <div
        class="feed"
        bind:this={feed}
        bind:clientHeight={viewportHeight}
        onscroll={(event) => (scrollTop = event.currentTarget.scrollTop)}
      >
        <div style:height={`${topPad}px`}></div>
        {#each visible as item (item.item?.id)}
          {@const info = item.item}
          <article class="row" class:pending={!item.complete}>
            <div class="preview">
              {#snippet kindBadge()}
                <span class="kind" title={kindLabel(info?.kind ?? Kind.UNSPECIFIED)}>
                  {kindLabel(info?.kind ?? Kind.UNSPECIFIED).slice(0, 1)}
                </span>
              {/snippet}
              {#if info && hasThumbnail(item)}
                <button
                  type="button"
                  class="thumb"
                  aria-label={t("history.showImageAria")}
                  onclick={() => openPreview(item)}
                >
                  <ImageCanvas id={info.id} maxSide={THUMB_SIDE} fallback={kindBadge} />
                </button>
              {:else}
                {@render kindBadge()}
              {/if}
            </div>

            <div class="body">
              <div class="line">
                <span class="title">{description(item) || t("common.item")}</span>
                {#if info && history.held.includes(info.id)}
                  <span class="badge confirm">{t("history.waitsForYou")}</span>
                {:else if info && !item.complete}
                  <span class="badge confirm">{t("history.notDownloaded")}</span>
                {/if}
              </div>
              <div class="line meta">
                <span>{originName(info?.origin ?? "")}</span>
                <span>{formatDateTime(info?.createdAtUnixMs ?? 0)}</span>
                {#if itemSize(info) > 0}
                  <span>{formatBytes(itemSize(info))}</span>
                {/if}
                {#if history.downloading.includes(info?.id ?? "")}
                  <span class="downloading">
                    {t("history.downloading", { time: elapsed(info?.id ?? "") })}
                  </span>
                {/if}
              </div>
            </div>

            <div class="actions">
              {#if history.held.includes(info?.id ?? "")}
                <button
                  class="button primary small"
                  type="button"
                  onclick={() => void history.apply(info?.id ?? "")}
                  disabled={!info}
                >
                  {t("common.apply")}
                </button>
              {:else}
                <button
                  class="button small"
                  type="button"
                  onclick={() => void history.copy(info?.id ?? "")}
                  disabled={!info}
                >
                  {t("common.copy")}
                </button>
              {/if}
              <button
                class="button small"
                type="button"
                onclick={() => info && void download(item)}
                disabled={!info ||
                  history.downloading.includes(info.id) ||
                  (item.complete && info.kind !== Kind.FILES)}
              >
                {item.complete && info?.kind !== Kind.FILES
                  ? t("history.downloaded")
                  : t("common.download")}
              </button>
              {#if deleting === info?.id}
                <button
                  class="button danger small"
                  type="button"
                  onclick={() => void history.remove(info?.id ?? "")}
                >
                  {t("history.delete")}
                </button>
                <button class="button ghost small" type="button" onclick={() => (deleting = null)}>
                  {t("common.cancel")}
                </button>
              {:else}
                <button
                  class="button danger small"
                  type="button"
                  onclick={() => (deleting = info?.id ?? null)}
                  disabled={!info}
                >
                  {t("history.delete")}
                </button>
              {/if}
            </div>
          </article>
        {/each}
        <div style:height={`${bottomPad}px`}></div>
      </div>
    {/if}
  </div>
{/if}

{#if preview}
  <div class="overlay" role="dialog" aria-modal="true" aria-label={t("history.previewAria")}>
    <button
      class="backdrop"
      type="button"
      aria-label={t("history.closePreviewAria")}
      onclick={() => (preview = null)}
    ></button>
    <figure>
      <ImageCanvas id={preview.id} maxSide={PREVIEW_SIDE} label={preview.title} />
      <figcaption>
        <span>{preview.title}</span>
        <button class="button small" type="button" onclick={() => (preview = null)}>
          {t("common.close")}
        </button>
      </figcaption>
    </figure>
  </div>
{/if}

<style>
  .history {
    display: flex;
    flex: 1;
    flex-direction: column;
    gap: 12px;
    min-height: 0;
  }

  .toolbar .field {
    flex-direction: row;
    align-items: center;
    gap: 6px;
  }

  .toolbar .field > span {
    font-weight: 400;
    color: var(--secondary);
  }

  .count {
    margin-left: auto;
  }

  .feed {
    display: flex;
    flex-direction: column;
    height: min(62vh, 620px);
    border-radius: 10px;
    background-color: var(--surface);
    box-shadow: var(--box-shadow);
    overflow-y: auto;
  }

  .row {
    display: flex;
    flex: 0 0 auto;
    align-items: center;
    gap: 12px;
    height: 88px;
    padding: 0 12px;
    border-bottom: 1px solid var(--separator);
    box-sizing: border-box;
  }

  .row.pending .title {
    color: var(--muted);
  }

  .preview {
    flex: 0 0 auto;
  }

  .thumb {
    display: block;
    width: 56px;
    height: 56px;
    padding: 0;
    border: 0;
    border-radius: 8px;
    box-shadow: var(--box-shadow);
    background-color: var(--surface-soft);
    overflow: hidden;
    cursor: zoom-in;
  }

  .thumb :global(canvas) {
    width: 100%;
    height: 100%;
    object-fit: cover;
  }

  .kind {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 56px;
    height: 56px;
    border-radius: 8px;
    color: var(--muted);
    background-color: var(--surface-soft);
    font-weight: 600;
  }

  .body {
    display: flex;
    flex: 1 1 auto;
    flex-direction: column;
    gap: 0.25rem;
    min-width: 0;
    overflow: hidden;
  }

  .line {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 0.5rem;
    min-width: 0;
  }

  .title {
    max-width: 100%;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-weight: 500;
  }

  .meta {
    flex-wrap: nowrap;
    overflow: hidden;
    white-space: nowrap;
    color: var(--muted);
    font-size: 11px;
  }

  .downloading {
    color: var(--accent);
  }

  .actions {
    display: flex;
    flex: 0 0 auto;
    flex-wrap: nowrap;
    justify-content: flex-end;
    gap: 0.35rem;
  }

  .overlay {
    position: fixed;
    inset: 0;
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 2rem;
    background-color: rgba(0, 0, 0, 0.45);
    z-index: 10;
  }

  .backdrop {
    position: absolute;
    inset: 0;
    border: none;
    background: transparent;
    cursor: zoom-out;
  }

  .overlay figure {
    position: relative;
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
    max-width: 90vw;
    max-height: 90vh;
    margin: 0;
    padding: 0.75rem;
    border-radius: 12px;
    background-color: var(--surface);
  }

  .overlay :global(canvas) {
    max-width: 100%;
    max-height: 75vh;
    object-fit: contain;
  }

  .overlay figcaption {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 1rem;
    color: var(--muted);
    font-size: 0.8125rem;
  }
</style>
